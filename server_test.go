package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

	err := extractZipExecutable(archive, "github-mcp-server.exe", filepath.Join(t.TempDir(), "out.exe"))
	if err == nil {
		t.Fatal("expected extractZipExecutable to fail when executable is missing")
	}
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
	version := strings.TrimPrefix(mcpServerVersion, "v")
	checksumsPath := filepath.Join("bundled", fmt.Sprintf("github-mcp-server_%s_checksums.txt", version))

	if _, err := os.Stat(checksumsPath); err != nil {
		t.Fatalf("expected checksums file %q for mcpServerVersion=%q: %v", checksumsPath, mcpServerVersion, err)
	}
}

func TestBundledChecksumConstantMatchesChecksumsFile(t *testing.T) {
	if bundledMCPArchiveName == "" {
		t.Skip("no bundled archive for this platform")
	}

	version := strings.TrimPrefix(mcpServerVersion, "v")
	checksumsPath := filepath.Join("bundled", fmt.Sprintf("github-mcp-server_%s_checksums.txt", version))

	content, err := os.ReadFile(checksumsPath)
	if err != nil {
		t.Fatalf("failed to read checksums file %q: %v", checksumsPath, err)
	}

	var fromFile string
	for _, line := range strings.Split(string(content), "\n") {
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
