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
	"reflect"
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
	// ErrBundledTempParentInsecure is returned when the temp parent directory fails safety checks.
	ErrBundledTempParentInsecure     = errors.New("bundled temp parent directory is insecure")
	errBundledTempParentStateInvalid = errors.New("bundled temp parent directory state is invalid")
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

	if ctx.Err() != nil {
		return nil
	}

	// Keep lifecycle ownership in waitForServerExit for graceful interrupt handling.
	cmd := exec.CommandContext(context.Background(), binaryPath, "stdio")
	cmd.Stdin = streams.in
	cmd.Stdout = streams.out
	cmd.Stderr = streams.err
	cmd.Env = buildChildProcessEnv(env)

	slog.InfoContext(ctx, "🚀 Starting bundled github-mcp-server", "version", mcpServerVersion)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start bundled github-mcp-server: %w", err)
	}

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

	tmpDir, cleanup, err := createTempDirWithFallback(bundledServerTempParentDirs())
	if err != nil {
		return "", func() {}, err
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

type tempParentDirState struct {
	info   os.FileInfo
	handle *os.File
}

func (s *tempParentDirState) close() {
	if s == nil || s.handle == nil {
		return
	}

	_ = s.handle.Close()
}

func createTempDirWithFallback(parentDirs []string) (string, func(), error) {
	if len(parentDirs) == 0 {
		parentDirs = []string{""}
	}

	attemptErrs := make([]error, 0, len(parentDirs))

	for _, parentDir := range parentDirs {
		tmpDir, cleanup, err := createTempDir(parentDir)
		if err == nil {
			return tmpDir, cleanup, nil
		}
		attemptErrs = append(attemptErrs, err)
	}

	return "", func() {}, fmt.Errorf(
		"failed to create temporary directory: %w",
		errors.Join(attemptErrs...),
	)
}

func createTempDir(parentDir string) (string, func(), error) {
	if parentDir == "" {
		tmpDir, err := os.MkdirTemp("", "gh-mcp-server-*")
		if err != nil {
			return "", func() {}, fmt.Errorf(
				"failed to create temporary directory in system temp: %w",
				err,
			)
		}

		cleanup := func() {
			_ = os.RemoveAll(tmpDir)
		}
		return tmpDir, cleanup, nil
	}

	parentState, err := ensureSecureTempParentDir(parentDir)
	if err != nil {
		return "", func() {}, err
	}

	tmpDir, err := createTempDirInVerifiedParent(parentDir, parentState)
	if err != nil {
		parentState.close()
		return "", func() {}, err
	}

	cleanup := func() {
		defer parentState.close()

		// Avoid deleting attacker-controlled paths if the parent changed.
		if err := verifyTempParentDirUnchanged(parentDir, parentState); err != nil {
			return
		}
		_ = os.RemoveAll(tmpDir)
	}

	return tmpDir, cleanup, nil
}

func ensureSecureTempParentDir(parentDir string) (*tempParentDirState, error) {
	if err := os.MkdirAll(parentDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create parent directory %q: %w", parentDir, err)
	}

	info, err := os.Lstat(parentDir)
	if err != nil {
		return nil, fmt.Errorf("failed to stat parent directory %q: %w", parentDir, err)
	}
	if err := validateTempParentDirInfo(parentDir, info); err != nil {
		return nil, err
	}

	state := &tempParentDirState{info: info}
	if runtime.GOOS == "windows" {
		return state, nil
	}

	handle, err := os.Open(parentDir)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to open parent directory %q for verification: %w",
			parentDir,
			err,
		)
	}

	handleInfo, err := handle.Stat()
	if err != nil {
		_ = handle.Close()
		return nil, fmt.Errorf(
			"failed to stat opened parent directory %q: %w",
			parentDir,
			err,
		)
	}
	if err := validateTempParentDirInfo(parentDir, handleInfo); err != nil {
		_ = handle.Close()
		return nil, err
	}
	if !os.SameFile(info, handleInfo) {
		_ = handle.Close()
		return nil, fmt.Errorf(
			"%w: parent directory %q changed while preparing temporary directory",
			ErrBundledTempParentInsecure,
			parentDir,
		)
	}

	tightenedInfo, err := ensureSecureUnixTempParentDir(parentDir, handle, handleInfo)
	if err != nil {
		_ = handle.Close()
		return nil, err
	}

	latestPathInfo, err := os.Lstat(parentDir)
	if err != nil {
		_ = handle.Close()
		return nil, fmt.Errorf(
			"failed to stat parent directory %q after permissions check: %w",
			parentDir,
			err,
		)
	}
	if err := validateTempParentDirInfo(parentDir, latestPathInfo); err != nil {
		_ = handle.Close()
		return nil, err
	}
	if !os.SameFile(tightenedInfo, latestPathInfo) {
		_ = handle.Close()
		return nil, fmt.Errorf(
			"%w: parent directory %q changed while preparing temporary directory",
			ErrBundledTempParentInsecure,
			parentDir,
		)
	}

	state.info = tightenedInfo
	state.handle = handle

	return state, nil
}

func ensureSecureUnixTempParentDir(
	parentDir string,
	handle *os.File,
	info os.FileInfo,
) (os.FileInfo, error) {
	if uid, ok := fileInfoUID(info); ok && uid != os.Geteuid() {
		return nil, fmt.Errorf(
			"%w: parent directory %q must be owned by the current user",
			ErrBundledTempParentInsecure,
			parentDir,
		)
	}

	if perms := info.Mode().Perm(); perms&0o077 == 0 {
		return info, nil
	}

	if err := handle.Chmod(0o700); err != nil {
		return nil, fmt.Errorf(
			"failed to tighten parent directory %q permissions: %w",
			parentDir,
			err,
		)
	}

	tightenedInfo, err := handle.Stat()
	if err != nil {
		return nil, fmt.Errorf(
			"failed to stat opened parent directory %q after chmod: %w",
			parentDir,
			err,
		)
	}
	if err := validateTempParentDirInfo(parentDir, tightenedInfo); err != nil {
		return nil, err
	}
	if perms := tightenedInfo.Mode().Perm(); perms&0o077 != 0 {
		return nil, fmt.Errorf(
			"%w: parent directory %q permissions are too broad (%#o)",
			ErrBundledTempParentInsecure,
			parentDir,
			perms,
		)
	}

	return tightenedInfo, nil
}

func validateTempParentDirInfo(parentDir string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf(
			"%w: parent directory %q must not be a symbolic link",
			ErrBundledTempParentInsecure,
			parentDir,
		)
	}
	if !info.IsDir() {
		return fmt.Errorf(
			"%w: parent path %q is not a directory",
			ErrBundledTempParentInsecure,
			parentDir,
		)
	}

	return nil
}

func fileInfoUID(info os.FileInfo) (int, bool) {
	value := reflect.ValueOf(info.Sys())
	if !value.IsValid() {
		return 0, false
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 0, false
		}
		value = value.Elem()
	}

	uidField := value.FieldByName("Uid")
	if !uidField.IsValid() {
		return 0, false
	}
	if uidField.CanInt() {
		uid := uidField.Int()
		if uid < 0 || uid > int64(math.MaxInt) {
			return 0, false
		}
		return int(uid), true
	}
	if uidField.CanUint() {
		uid := uidField.Uint()
		if uid > uint64(math.MaxInt) {
			return 0, false
		}
		return int(uid), true
	}

	return 0, false
}

func verifyTempParentDirUnchanged(parentDir string, expected *tempParentDirState) error {
	if expected == nil || expected.info == nil {
		return fmt.Errorf(
			"%w: missing expected parent directory state for %q",
			errBundledTempParentStateInvalid,
			parentDir,
		)
	}

	current, err := os.Lstat(parentDir)
	if err != nil {
		return fmt.Errorf("failed to re-check parent directory %q: %w", parentDir, err)
	}
	if err := validateTempParentDirInfo(parentDir, current); err != nil {
		return err
	}

	baseline := expected.info
	if expected.handle != nil {
		baseline, err = expected.handle.Stat()
		if err != nil {
			return fmt.Errorf(
				"failed to stat opened parent directory %q during verification: %w",
				parentDir,
				err,
			)
		}
	}

	if !os.SameFile(baseline, current) {
		return fmt.Errorf(
			"%w: parent directory %q changed while creating temporary directory",
			ErrBundledTempParentInsecure,
			parentDir,
		)
	}

	return nil
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
