package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var (
	// ErrServerNonZeroExit is returned when github-mcp-server exits with non-zero status.
	ErrServerNonZeroExit = errors.New("server exited with non-zero status")
	// ErrNoBundledServerForPlatform is returned when no bundled archive exists for the current platform.
	ErrNoBundledServerForPlatform = errors.New("no bundled github-mcp-server for platform")
	// ErrUnsupportedBundledArchiveFormat is returned when the bundled archive format is unknown.
	ErrUnsupportedBundledArchiveFormat = errors.New("unsupported bundled archive format")
	// ErrBundledChecksumMismatch is returned when bundled archive checksum validation fails.
	ErrBundledChecksumMismatch = errors.New("bundled github-mcp-server checksum mismatch")
	// ErrBundledExecutableNotFound is returned when the bundled executable is missing from the archive.
	ErrBundledExecutableNotFound = errors.New("bundled executable was not found in archive")
	// ErrBundledExecutableTooLarge is returned when extracted bytes exceed the configured limit.
	ErrBundledExecutableTooLarge = errors.New("bundled executable exceeds extraction size limit")
	// ErrBundledExecutableInvalidSize is returned when archive metadata reports an invalid executable size.
	ErrBundledExecutableInvalidSize = errors.New("bundled executable has invalid size")
)

const (
	maxBundledExecutableBytes int64 = 64 << 20 // 64 MiB
	// Wait this long after SIGINT before force-killing the bundled server process.
	serverGracefulShutdownTimeout = 3 * time.Second
	// Reserve capacity for cache-dir candidate and system-temp fallback candidate.
	bundledTempParentDirCapacity = 2
)

var allowedParentEnvKeys = []string{
	// Basic runtime environment.
	"PATH",
	"HOME",
	"USERPROFILE",
	"TMPDIR",
	"TMP",
	"TEMP",
	"SHELL",
	"COMSPEC",
	"SYSTEMROOT",
	"WINDIR",

	// Proxy/certificate environment for enterprise networks.
	"HTTP_PROXY",
	"HTTPS_PROXY",
	"NO_PROXY",
	"ALL_PROXY",
	"http_proxy",
	"https_proxy",
	"no_proxy",
	"all_proxy",
	"SSL_CERT_FILE",
	"SSL_CERT_DIR",
}

func runBundledServer(ctx context.Context, env []string, streams *ioStreams) error {
	binaryPath, cleanup, err := materializeBundledServerBinary()
	if err != nil {
		return err
	}
	defer cleanup()

	// Keep lifecycle control in waitForServerExit to allow graceful interrupt handling.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), binaryPath, "stdio")
	cmd.Stdin = streams.in
	cmd.Stdout = streams.out
	cmd.Stderr = streams.err
	cmd.Env = buildChildProcessEnv(env)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start bundled github-mcp-server: %w", err)
	}

	slog.InfoContext(ctx, "🚀 Starting bundled github-mcp-server", "version", mcpServerVersion)

	if err := waitForServerExit(ctx, cmd); err != nil {
		return err
	}

	return nil
}

func waitForServerExit(ctx context.Context, cmd *exec.Cmd) error {
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case waitErr := <-waitCh:
		// Prefer clean shutdown semantics if the caller has already canceled.
		select {
		case <-ctx.Done():
			return nil
		default:
			return normalizeServerExit(waitErr)
		}
	case <-ctx.Done():
		stopServerProcess(cmd, waitCh)
		return nil
	}
}

func stopServerProcess(cmd *exec.Cmd, waitCh <-chan error) {
	proc := cmd.Process
	if proc == nil {
		return
	}

	// Prefer graceful interrupt on Unix, then force-kill after timeout.
	if runtime.GOOS != "windows" {
		_ = proc.Signal(os.Interrupt)
		select {
		case <-waitCh:
			return
		case <-time.After(serverGracefulShutdownTimeout):
		}
	}

	_ = proc.Kill()
	<-waitCh
}

func normalizeServerExit(err error) error {
	if err == nil {
		return nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Errorf("%w: %d", ErrServerNonZeroExit, exitErr.ExitCode())
	}

	return fmt.Errorf("failed waiting for github-mcp-server process: %w", err)
}

func materializeBundledServerBinary() (string, func(), error) {
	if bundledMCPArchiveName == "" || bundledMCPExecutableName == "" ||
		len(bundledMCPArchive) == 0 {
		return "", func() {}, fmt.Errorf(
			"%w: os=%s arch=%s",
			ErrNoBundledServerForPlatform,
			runtime.GOOS,
			runtime.GOARCH,
		)
	}

	if err := verifyBundledArchiveChecksum(bundledMCPArchive, bundledMCPArchiveSHA256); err != nil {
		return "", func() {}, err
	}

	tmpDir, err := createTempDirWithFallback(bundledServerTempParentDirs())
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	binaryPath := filepath.Join(tmpDir, bundledMCPExecutableName)

	switch {
	case strings.HasSuffix(bundledMCPArchiveName, ".tar.gz"):
		err = extractTarGzExecutable(bundledMCPArchive, bundledMCPExecutableName, binaryPath)
	case strings.HasSuffix(bundledMCPArchiveName, ".zip"):
		err = extractZipExecutable(bundledMCPArchive, bundledMCPExecutableName, binaryPath)
	default:
		err = fmt.Errorf(
			"%w: archive=%s",
			ErrUnsupportedBundledArchiveFormat,
			bundledMCPArchiveName,
		)
	}
	if err != nil {
		cleanup()
		return "", func() {}, err
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(binaryPath, 0o755); err != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("failed to mark bundled binary executable: %w", err)
		}
	}

	return binaryPath, cleanup, nil
}

func bundledServerTempParentDirs() []string {
	parentDirs := make([]string, 0, bundledTempParentDirCapacity)

	if cacheDir, err := os.UserCacheDir(); err == nil && cacheDir != "" {
		parentDirs = append(parentDirs, filepath.Join(cacheDir, "gh-mcp"))
	}

	// Keep system temp as a fallback when cache dir is unavailable.
	parentDirs = append(parentDirs, "")

	return parentDirs
}

func createTempDirWithFallback(parentDirs []string) (string, error) {
	if len(parentDirs) == 0 {
		parentDirs = []string{""}
	}

	attemptErrs := make([]error, 0, len(parentDirs))

	for _, parentDir := range parentDirs {
		tmpDir, err := createTempDir(parentDir)
		if err == nil {
			return tmpDir, nil
		}
		attemptErrs = append(attemptErrs, err)
	}

	return "", fmt.Errorf("failed to create temporary directory: %w", errors.Join(attemptErrs...))
}

func createTempDir(parentDir string) (string, error) {
	if parentDir != "" {
		if err := os.MkdirAll(parentDir, 0o700); err != nil {
			return "", fmt.Errorf("failed to create parent directory %q: %w", parentDir, err)
		}
	}

	tmpDir, err := os.MkdirTemp(parentDir, "gh-mcp-server-*")
	if err != nil {
		if parentDir == "" {
			return "", fmt.Errorf("failed to create temporary directory in system temp: %w", err)
		}
		return "", fmt.Errorf("failed to create temporary directory in %q: %w", parentDir, err)
	}

	return tmpDir, nil
}

func verifyBundledArchiveChecksum(archive []byte, expectedSHA256 string) error {
	sum := sha256.Sum256(archive)
	actual := hex.EncodeToString(sum[:])

	if !strings.EqualFold(actual, expectedSHA256) {
		return fmt.Errorf(
			"%w: archive=%s expected=%s actual=%s",
			ErrBundledChecksumMismatch,
			bundledMCPArchiveName,
			expectedSHA256,
			actual,
		)
	}

	return nil
}

func extractTarGzExecutable(archive []byte, executableName, outputPath string) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("failed to read bundled tar.gz: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read bundled tar entry: %w", err)
		}

		if !header.FileInfo().Mode().IsRegular() {
			continue
		}
		// Archive entry names use "/" separators regardless of host OS.
		if path.Base(header.Name) != executableName {
			continue
		}
		if err := validateBundledExecutableSize(header.Size, executableName); err != nil {
			return err
		}

		file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
		if err != nil {
			return fmt.Errorf("failed to create extracted binary: %w", err)
		}

		if err := copyBundledExecutableWithLimit(file, tarReader, executableName); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("failed to close extracted binary: %w", err)
		}

		return nil
	}

	return fmt.Errorf(
		"%w: executable=%q archive=%s",
		ErrBundledExecutableNotFound,
		executableName,
		bundledMCPArchiveName,
	)
}

func extractZipExecutable(archive []byte, executableName, outputPath string) error {
	zipReader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return fmt.Errorf("failed to read bundled zip: %w", err)
	}

	for _, fileInArchive := range zipReader.File {
		// Archive entry names use "/" separators regardless of host OS.
		if path.Base(fileInArchive.Name) != executableName {
			continue
		}
		if err := validateZipExecutableSize(
			fileInArchive.UncompressedSize64,
			executableName,
		); err != nil {
			return err
		}

		readCloser, err := fileInArchive.Open()
		if err != nil {
			return fmt.Errorf("failed to open bundled executable: %w", err)
		}

		file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
		if err != nil {
			_ = readCloser.Close()
			return fmt.Errorf("failed to create extracted binary: %w", err)
		}

		if err := copyBundledExecutableWithLimit(file, readCloser, executableName); err != nil {
			_ = file.Close()
			_ = readCloser.Close()
			return err
		}

		if err := file.Close(); err != nil {
			_ = readCloser.Close()
			return fmt.Errorf("failed to close extracted binary: %w", err)
		}
		if err := readCloser.Close(); err != nil {
			return fmt.Errorf("failed to close bundled executable stream: %w", err)
		}

		return nil
	}

	return fmt.Errorf(
		"%w: executable=%q archive=%s",
		ErrBundledExecutableNotFound,
		executableName,
		bundledMCPArchiveName,
	)
}

func copyBundledExecutableWithLimit(dst io.Writer, src io.Reader, executableName string) error {
	copied, err := io.Copy(dst, io.LimitReader(src, maxBundledExecutableBytes+1))
	if err != nil {
		return fmt.Errorf("failed to extract bundled binary: %w", err)
	}
	if copied > maxBundledExecutableBytes {
		return fmt.Errorf(
			"%w: executable=%q limit=%d archive=%s",
			ErrBundledExecutableTooLarge,
			executableName,
			maxBundledExecutableBytes,
			bundledMCPArchiveName,
		)
	}

	return nil
}

func validateBundledExecutableSize(size int64, executableName string) error {
	if size < 0 || size > maxBundledExecutableBytes {
		return fmt.Errorf(
			"%w: executable=%q size=%d archive=%s allowed=0-%d",
			ErrBundledExecutableInvalidSize,
			executableName,
			size,
			bundledMCPArchiveName,
			maxBundledExecutableBytes,
		)
	}

	return nil
}

func validateZipExecutableSize(size uint64, executableName string) error {
	if size > math.MaxInt64 {
		return fmt.Errorf(
			"%w: executable=%q size=%d archive=%s allowed=0-%d",
			ErrBundledExecutableInvalidSize,
			executableName,
			size,
			bundledMCPArchiveName,
			maxBundledExecutableBytes,
		)
	}

	return validateBundledExecutableSize(int64(size), executableName)
}

func buildChildProcessEnv(required []string) []string {
	merged := make(map[string]string, len(required)+len(allowedParentEnvKeys))
	order := make([]string, 0, len(required)+len(allowedParentEnvKeys))

	add := func(key, value string) {
		if _, exists := merged[key]; !exists {
			order = append(order, key)
		}
		merged[key] = value
	}

	for _, item := range required {
		key, value, ok := strings.Cut(item, "=")
		if !ok || key == "" {
			continue
		}
		add(key, value)
	}

	for _, key := range allowedParentEnvKeys {
		if _, exists := merged[key]; exists {
			continue
		}
		if value, ok := os.LookupEnv(key); ok {
			add(key, value)
		}
	}

	result := make([]string, 0, len(order))
	for _, key := range order {
		result = append(result, key+"="+merged[key])
	}

	return result
}
