package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// Wait this long after SIGINT before force-killing the bundled server process.
	serverGracefulShutdownTimeout = 3 * time.Second
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
		return fmt.Errorf("%w: %d", errServerNonZeroExit, exitErr.ExitCode())
	}

	return fmt.Errorf("failed waiting for github-mcp-server process: %w", err)
}

func materializeBundledServerBinary() (string, func(), error) {
	if bundledMCPArchiveName == "" || bundledMCPExecutableName == "" ||
		len(bundledMCPArchive) == 0 {
		return "", func() {}, fmt.Errorf(
			"%w: os=%s arch=%s",
			errNoBundledServerForPlatform,
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
			errUnsupportedBundledArchiveFormat,
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
	var parentDirs []string

	if cacheDir, err := os.UserCacheDir(); err == nil && cacheDir != "" {
		parentDirs = append(parentDirs, filepath.Join(cacheDir, "gh-mcp"))
	}

	// Keep system temp as a fallback when cache dir is unavailable.
	parentDirs = append(parentDirs, "")

	return parentDirs
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
