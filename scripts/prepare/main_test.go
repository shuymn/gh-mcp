package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseMCPServerVersion(t *testing.T) {
	t.Run("parses version", func(t *testing.T) {
		content := []byte(`package main

const mcpServerVersion = "v0.30.3"
`)
		version, err := parseMCPServerVersion(content)
		if err != nil {
			t.Fatalf("parseMCPServerVersion returned error: %v", err)
		}
		if version != "v0.30.3" {
			t.Fatalf("unexpected version: got %q", version)
		}
	})

	t.Run("missing version", func(t *testing.T) {
		_, err := parseMCPServerVersion([]byte("package main"))
		if err == nil {
			t.Fatal("expected parseMCPServerVersion to fail when constant is missing")
		}
	})
}

func TestPositiveIntFromEnv(t *testing.T) {
	t.Run("default value", func(t *testing.T) {
		t.Setenv("DOWNLOAD_RETRY_COUNT", "")

		value, err := positiveIntFromEnv("DOWNLOAD_RETRY_COUNT", 3, errInvalidRetryCount)
		if err != nil {
			t.Fatalf("positiveIntFromEnv returned error: %v", err)
		}
		if value != 3 {
			t.Fatalf("unexpected value: got %d", value)
		}
	})

	t.Run("configured value", func(t *testing.T) {
		t.Setenv("DOWNLOAD_RETRY_COUNT", "5")

		value, err := positiveIntFromEnv("DOWNLOAD_RETRY_COUNT", 3, errInvalidRetryCount)
		if err != nil {
			t.Fatalf("positiveIntFromEnv returned error: %v", err)
		}
		if value != 5 {
			t.Fatalf("unexpected value: got %d", value)
		}
	})

	t.Run("invalid count", func(t *testing.T) {
		t.Setenv("DOWNLOAD_RETRY_COUNT", "0")

		_, err := positiveIntFromEnv("DOWNLOAD_RETRY_COUNT", 3, errInvalidRetryCount)
		if !errors.Is(err, errInvalidRetryCount) {
			t.Fatalf("expected errInvalidRetryCount, got: %v", err)
		}
	})

	t.Run("invalid delay", func(t *testing.T) {
		t.Setenv("DOWNLOAD_RETRY_DELAY_SECONDS", "x")

		_, err := positiveIntFromEnv("DOWNLOAD_RETRY_DELAY_SECONDS", 2, errInvalidRetryDelay)
		if !errors.Is(err, errInvalidRetryDelay) {
			t.Fatalf("expected errInvalidRetryDelay, got: %v", err)
		}
	})
}

func TestLoadChecksums(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checksums.txt")
	if err := os.WriteFile(
		path,
		[]byte("aaa file-a.tar.gz\nbbb file-b.zip\n"),
		0o600,
	); err != nil {
		t.Fatalf("failed to create checksums file: %v", err)
	}

	checksums, err := loadChecksums(path)
	if err != nil {
		t.Fatalf("loadChecksums returned error: %v", err)
	}

	if got := checksums["file-a.tar.gz"]; got != "aaa" {
		t.Fatalf("unexpected checksum for file-a.tar.gz: got %q", got)
	}
	if got := checksums["file-b.zip"]; got != "bbb" {
		t.Fatalf("unexpected checksum for file-b.zip: got %q", got)
	}
}
