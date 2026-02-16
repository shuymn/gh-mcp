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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestVerifyBundledArchiveChecksum(t *testing.T) {
	archive := []byte("test-archive")
	sum := sha256.Sum256(archive)
	validChecksum := hex.EncodeToString(sum[:])

	if err := verifyBundledArchiveChecksum(archive, validChecksum); err != nil {
		t.Fatalf("expected checksum verification to pass, got error: %v", err)
	}

	err := verifyBundledArchiveChecksum(archive, strings.Repeat("0", 64))
	if err == nil {
		t.Fatal("expected checksum verification to fail, got nil")
	}
}

func TestExtractTarGzExecutable(t *testing.T) {
	archive := buildTarGzArchive(t, map[string]string{
		"README.md":          "readme",
		"github-mcp-server":  "binary-content",
		"nested/another.txt": "other",
	})

	outputPath := filepath.Join(t.TempDir(), "github-mcp-server")
	if runtime.GOOS == "windows" {
		outputPath += ".exe"
	}

	if err := extractTarGzExecutable(archive, filepath.Base(outputPath), outputPath); err != nil {
		t.Fatalf("extractTarGzExecutable returned error: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read extracted file: %v", err)
	}

	if string(data) != "binary-content" {
		t.Fatalf("unexpected extracted content: got %q", string(data))
	}
}

func TestExtractTarGzExecutableNotFound(t *testing.T) {
	archive := buildTarGzArchive(t, map[string]string{
		"README.md": "readme",
	})

	err := extractTarGzExecutable(archive, "github-mcp-server", filepath.Join(t.TempDir(), "out"))
	if err == nil {
		t.Fatal("expected extractTarGzExecutable to fail when executable is missing")
	}
}

func TestExtractZipExecutable(t *testing.T) {
	archive := buildZipArchive(t, map[string]string{
		"README.md":             "readme",
		"github-mcp-server.exe": "binary-content",
	})

	outputPath := filepath.Join(t.TempDir(), "github-mcp-server.exe")
	if err := extractZipExecutable(archive, "github-mcp-server.exe", outputPath); err != nil {
		t.Fatalf("extractZipExecutable returned error: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read extracted file: %v", err)
	}

	if string(data) != "binary-content" {
		t.Fatalf("unexpected extracted content: got %q", string(data))
	}
}

func TestExtractZipExecutableNotFound(t *testing.T) {
	archive := buildZipArchive(t, map[string]string{
		"README.md": "readme",
	})

	err := extractZipExecutable(
		archive,
		"github-mcp-server.exe",
		filepath.Join(t.TempDir(), "out.exe"),
	)
	if err == nil {
		t.Fatal("expected extractZipExecutable to fail when executable is missing")
	}
}

func TestBundledExecutableSizeValidation(t *testing.T) {
	t.Run("negative tar size", func(t *testing.T) {
		err := validateBundledExecutableSize(-1, "github-mcp-server")
		if err == nil {
			t.Fatal("expected negative size to fail validation")
		}
	})

	t.Run("zero tar size", func(t *testing.T) {
		err := validateBundledExecutableSize(0, "github-mcp-server")
		if err != nil {
			t.Fatalf("expected zero size to pass validation, got: %v", err)
		}
	})

	t.Run("max tar size", func(t *testing.T) {
		err := validateBundledExecutableSize(maxBundledExecutableBytes, "github-mcp-server")
		if err != nil {
			t.Fatalf("expected max size to pass validation, got: %v", err)
		}
	})

	t.Run("tar size over max", func(t *testing.T) {
		err := validateBundledExecutableSize(maxBundledExecutableBytes+1, "github-mcp-server")
		if err == nil {
			t.Fatal("expected over-max size to fail validation")
		}
	})

	t.Run("zip size over max", func(t *testing.T) {
		err := validateZipExecutableSize(
			uint64(maxBundledExecutableBytes+1),
			"github-mcp-server.exe",
		)
		if err == nil {
			t.Fatal("expected zip over-max size to fail validation")
		}
	})
}

func TestCopyBundledExecutableWithLimit(t *testing.T) {
	t.Run("within limit", func(t *testing.T) {
		if err := copyBundledExecutableWithLimit(
			io.Discard,
			strings.NewReader("binary-content"),
			"github-mcp-server",
		); err != nil {
			t.Fatalf("expected copy to succeed, got: %v", err)
		}
	})

	t.Run("over limit", func(t *testing.T) {
		reader := io.LimitReader(repeatByteReader{}, maxBundledExecutableBytes+2)

		err := copyBundledExecutableWithLimit(
			io.Discard,
			reader,
			"github-mcp-server",
		)
		if err == nil {
			t.Fatal("expected copy to fail when payload exceeds max size")
		}
		if !strings.Contains(err.Error(), "exceeds extraction size limit") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestBuildChildProcessEnv(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("HTTPS_PROXY", "https://proxy.example.test")
	t.Setenv("SECRET_SHOULD_NOT_PASS", "top-secret")

	env := buildChildProcessEnv([]string{
		"GITHUB_PERSONAL_ACCESS_TOKEN=token-123",
		"GITHUB_HOST=https://github.com",
		"PATH=/custom/bin", // required values should take precedence
		"MALFORMED",
	})

	m := envSliceToMap(t, env)

	if got := m["GITHUB_PERSONAL_ACCESS_TOKEN"]; got != "token-123" {
		t.Fatalf("unexpected GITHUB_PERSONAL_ACCESS_TOKEN: got %q", got)
	}
	if got := m["GITHUB_HOST"]; got != "https://github.com" {
		t.Fatalf("unexpected GITHUB_HOST: got %q", got)
	}
	if got := m["PATH"]; got != "/custom/bin" {
		t.Fatalf("expected required PATH to override parent PATH, got %q", got)
	}
	if got := m["HTTPS_PROXY"]; got != "https://proxy.example.test" {
		t.Fatalf("unexpected HTTPS_PROXY: got %q", got)
	}
	if _, ok := m["SECRET_SHOULD_NOT_PASS"]; ok {
		t.Fatal("unexpected secret env propagated to child process")
	}
	if _, ok := m["MALFORMED"]; ok {
		t.Fatal("malformed env entry should not be propagated")
	}
}

func TestBundledVersionMatchesChecksumsFile(t *testing.T) {
	if bundledMCPArchiveName == "" {
		t.Skip("no bundled archive for this platform")
	}

	version := strings.TrimPrefix(mcpServerVersion, "v")
	checksumsPath := filepath.Join(
		"bundled",
		fmt.Sprintf("github-mcp-server_%s_checksums.txt", version),
	)

	if _, err := os.Stat(checksumsPath); err != nil {
		t.Fatalf(
			"expected checksums file %q for mcpServerVersion=%q: %v",
			checksumsPath,
			mcpServerVersion,
			err,
		)
	}
}

func TestBundledChecksumConstantMatchesChecksumsFile(t *testing.T) {
	if bundledMCPArchiveName == "" {
		t.Skip("no bundled archive for this platform")
	}

	version := strings.TrimPrefix(mcpServerVersion, "v")
	checksumsPath := filepath.Join(
		"bundled",
		fmt.Sprintf("github-mcp-server_%s_checksums.txt", version),
	)

	content, err := os.ReadFile(checksumsPath)
	if err != nil {
		t.Fatalf("failed to read checksums file %q: %v", checksumsPath, err)
	}

	var fromFile string
	for line := range strings.SplitSeq(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[1] == bundledMCPArchiveName {
			fromFile = fields[0]
			break
		}
	}

	if fromFile == "" {
		t.Fatalf("archive %q not found in checksums file %q", bundledMCPArchiveName, checksumsPath)
	}

	if fromFile != bundledMCPArchiveSHA256 {
		t.Fatalf(
			"checksum constant mismatch for %s: got %s, want %s",
			bundledMCPArchiveName,
			bundledMCPArchiveSHA256,
			fromFile,
		)
	}
}

func TestCreateTempDirWithFallback(t *testing.T) {
	root := t.TempDir()

	invalidParent := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(invalidParent, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to create invalid parent sentinel: %v", err)
	}

	validParent := filepath.Join(root, "cache")

	tmpDir, err := createTempDirWithFallback([]string{invalidParent, validParent})
	if err != nil {
		t.Fatalf("createTempDirWithFallback returned error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if !strings.HasPrefix(tmpDir, validParent+string(filepath.Separator)) {
		t.Fatalf("expected temp dir under %q, got %q", validParent, tmpDir)
	}
}

func TestBundledServerTempParentDirs(t *testing.T) {
	parentDirs := bundledServerTempParentDirs()
	if len(parentDirs) == 0 {
		t.Fatal("expected at least one temp parent directory")
	}

	if got := parentDirs[len(parentDirs)-1]; got != "" {
		t.Fatalf("expected system temp fallback as last entry, got %q", got)
	}
}

func TestWaitForServerExit(t *testing.T) {
	t.Run("normal exit", func(t *testing.T) {
		cmd := newServerTestHelperCommand(t, "exit-0")
		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start helper process: %v", err)
		}

		if err := waitForServerExit(context.Background(), cmd); err != nil {
			t.Fatalf("waitForServerExit returned error: %v", err)
		}
	})

	t.Run("non-zero exit", func(t *testing.T) {
		cmd := newServerTestHelperCommand(t, "exit-9")
		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start helper process: %v", err)
		}

		err := waitForServerExit(context.Background(), cmd)
		if err == nil {
			t.Fatal("expected waitForServerExit to return non-zero exit error")
		}
		if !errors.Is(err, ErrServerNonZeroExit) {
			t.Fatalf("expected ErrServerNonZeroExit, got: %v", err)
		}
		if !strings.Contains(err.Error(), ": 9") {
			t.Fatalf("expected exit code 9 in error, got: %v", err)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cmd := newServerTestHelperCommand(t, "sleep")
		if err := cmd.Start(); err != nil {
			cancel()
			t.Fatalf("failed to start helper process: %v", err)
		}

		cancel()
		if err := waitForServerExit(ctx, cmd); err != nil {
			t.Fatalf("expected nil error on canceled context, got: %v", err)
		}
	})
}

func TestWaitForServerExitCanceledContextSuppressesExitError(t *testing.T) {
	for i := range 24 {
		ctx, cancel := context.WithCancel(context.Background())
		cmd := newServerTestHelperCommand(t, "sleep-then-exit-5")
		if err := cmd.Start(); err != nil {
			cancel()
			t.Fatalf("failed to start helper process at iteration %d: %v", i, err)
		}

		cancel()
		time.Sleep(20 * time.Millisecond)

		if err := waitForServerExit(ctx, cmd); err != nil {
			t.Fatalf("expected nil error at iteration %d, got: %v", i, err)
		}
	}
}

func TestStopServerProcess(t *testing.T) {
	t.Run("nil process", func(_ *testing.T) {
		stopServerProcess(exec.Command("definitely-not-started"), make(chan error))
	})

	t.Run("running process", func(t *testing.T) {
		cmd := newServerTestHelperCommand(t, "sleep")
		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start helper process: %v", err)
		}

		waitCh := make(chan error, 1)
		go func() {
			waitCh <- cmd.Wait()
		}()

		stopServerProcess(cmd, waitCh)

		if cmd.ProcessState == nil {
			t.Fatal("expected process state after stopServerProcess")
		}
	})
}

func TestNormalizeServerExit(t *testing.T) {
	if err := normalizeServerExit(nil); err != nil {
		t.Fatalf("expected nil error for nil input, got: %v", err)
	}

	cmd := newServerTestHelperCommand(t, "exit-7")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected helper process to exit non-zero")
	}

	normalized := normalizeServerExit(err)
	if !errors.Is(normalized, ErrServerNonZeroExit) {
		t.Fatalf("expected ErrServerNonZeroExit, got: %v", normalized)
	}
	if !strings.Contains(normalized.Error(), ": 7") {
		t.Fatalf("expected exit code 7 in normalized error, got: %v", normalized)
	}

	waitErr := errors.New("wait failed")
	normalized = normalizeServerExit(waitErr)
	if normalized == nil {
		t.Fatal("expected wrapped wait error, got nil")
	}
	if !strings.Contains(normalized.Error(), "failed waiting for github-mcp-server process") {
		t.Fatalf("unexpected wrapped error: %v", normalized)
	}
}

func buildTarGzArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var raw bytes.Buffer
	gzipWriter := gzip.NewWriter(&raw)
	tarWriter := tar.NewWriter(gzipWriter)

	for name, body := range files {
		header := &tar.Header{
			Name: name,
			Mode: 0o600,
			Size: int64(len(body)),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("failed to write tar header: %v", err)
		}
		if _, err := tarWriter.Write([]byte(body)); err != nil {
			t.Fatalf("failed to write tar content: %v", err)
		}
	}

	if err := tarWriter.Close(); err != nil {
		t.Fatalf("failed to close tar writer: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("failed to close gzip writer: %v", err)
	}

	return raw.Bytes()
}

func buildZipArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var raw bytes.Buffer
	zipWriter := zip.NewWriter(&raw)

	for name, body := range files {
		fileWriter, err := zipWriter.Create(name)
		if err != nil {
			t.Fatalf("failed to create zip entry: %v", err)
		}
		if _, err := fileWriter.Write([]byte(body)); err != nil {
			t.Fatalf("failed to write zip entry: %v", err)
		}
	}

	if err := zipWriter.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}

	return raw.Bytes()
}

func envSliceToMap(t *testing.T, env []string) map[string]string {
	t.Helper()

	m := make(map[string]string, len(env))
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if !ok || key == "" {
			t.Fatalf("invalid env entry in test: %q", item)
		}
		m[key] = value
	}

	return m
}

type repeatByteReader struct{}

func (repeatByteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func newServerTestHelperCommand(t *testing.T, mode string) *exec.Cmd {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=TestServerProcessHelper", "--", mode)
	cmd.Env = append(os.Environ(), "GO_WANT_SERVER_PROCESS_HELPER=1")

	return cmd
}

func TestServerProcessHelper(*testing.T) {
	if os.Getenv("GO_WANT_SERVER_PROCESS_HELPER") != "1" {
		return
	}

	mode := ""
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
			break
		}
	}

	switch mode {
	case "exit-0":
		os.Exit(0)
	case "exit-7":
		os.Exit(7)
	case "exit-9":
		os.Exit(9)
	case "sleep":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "sleep-then-exit-5":
		time.Sleep(10 * time.Millisecond)
		os.Exit(5)
	default:
		os.Exit(2)
	}
}
