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
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ErrServerNonZeroExit is returned when github-mcp-server exits with non-zero status.
var ErrServerNonZeroExit = errors.New("server exited with non-zero status")

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

	cmd := exec.Command(binaryPath, "stdio")
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
	case err := <-waitCh:
		return normalizeServerExit(err)
	case <-ctx.Done():
		stopServerProcess(cmd, waitCh)
		return nil
	}
}

func stopServerProcess(cmd *exec.Cmd, waitCh <-chan error) {
	if cmd.Process == nil {
		return
	}

	// Prefer graceful interrupt on Unix, then force-kill after timeout.
	if runtime.GOOS != "windows" {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-waitCh:
			return
		case <-time.After(3 * time.Second):
		}
	}

	_ = cmd.Process.Kill()
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
	if bundledMCPArchiveName == "" || bundledMCPExecutableName == "" || len(bundledMCPArchive) == 0 {
		return "", func() {}, fmt.Errorf(
			"no bundled github-mcp-server for platform %s/%s",
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
		err = fmt.Errorf("unsupported bundled archive format: %s", bundledMCPArchiveName)
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
	parentDirs := make([]string, 0, 2)

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
			"bundled github-mcp-server checksum mismatch for %s: expected %s, got %s",
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

		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		if path.Base(header.Name) != executableName {
			continue
		}

		file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
		if err != nil {
			return fmt.Errorf("failed to create extracted binary: %w", err)
		}
		defer file.Close()

		if _, err := io.Copy(file, tarReader); err != nil {
			return fmt.Errorf("failed to extract bundled binary: %w", err)
		}

		return nil
	}

	return fmt.Errorf(
		"bundled executable %q was not found in %s",
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
		if path.Base(fileInArchive.Name) != executableName {
			continue
		}

		readCloser, err := fileInArchive.Open()
		if err != nil {
			return fmt.Errorf("failed to open bundled executable: %w", err)
		}

		file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
		if err != nil {
			readCloser.Close()
			return fmt.Errorf("failed to create extracted binary: %w", err)
		}

		if _, err := io.Copy(file, readCloser); err != nil {
			file.Close()
			readCloser.Close()
			return fmt.Errorf("failed to extract bundled binary: %w", err)
		}

		if err := file.Close(); err != nil {
			readCloser.Close()
			return fmt.Errorf("failed to close extracted binary: %w", err)
		}
		if err := readCloser.Close(); err != nil {
			return fmt.Errorf("failed to close bundled executable stream: %w", err)
		}

		return nil
	}

	return fmt.Errorf(
		"bundled executable %q was not found in %s",
		executableName,
		bundledMCPArchiveName,
	)
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
