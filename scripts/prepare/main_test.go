package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	t.Run("parses version from grouped const", func(t *testing.T) {
		content := []byte(`package main

const (
	otherConst = "value"
	mcpServerVersion = "v1.2.3"
)
`)
		version, err := parseMCPServerVersion(content)
		if err != nil {
			t.Fatalf("parseMCPServerVersion returned error: %v", err)
		}
		if version != "v1.2.3" {
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

func TestParseBundleMetadata(t *testing.T) {
	t.Run("parses metadata from const block", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bundle_fixture.go")
		content := `package main

const (
	bundledMCPArchiveName    = "github-mcp-server_Darwin_arm64.tar.gz"
	bundledMCPArchiveSHA256  = "abc123"
	bundledMCPExecutableName = "github-mcp-server"
)
`
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("failed to write fixture: %v", err)
		}

		metadata, err := parseBundleMetadata(path)
		if err != nil {
			t.Fatalf("parseBundleMetadata returned error: %v", err)
		}
		if metadata.archiveName != "github-mcp-server_Darwin_arm64.tar.gz" {
			t.Fatalf("unexpected archiveName: got %q", metadata.archiveName)
		}
		if metadata.sha256 != "abc123" {
			t.Fatalf("unexpected sha256: got %q", metadata.sha256)
		}
	})

	t.Run("missing const", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bundle_missing.go")
		content := `package main

const bundledMCPArchiveSHA256 = "abc123"
`
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("failed to write fixture: %v", err)
		}

		_, err := parseBundleMetadata(path)
		if err == nil {
			t.Fatal("expected parseBundleMetadata to fail when const is missing")
		}
		if !errors.Is(err, errBundleMetadataNotFound) {
			t.Fatalf("expected errBundleMetadataNotFound, got: %v", err)
		}
	})

	t.Run("invalid const type", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bundle_invalid.go")
		content := `package main

const (
	bundledMCPArchiveName   = 123
	bundledMCPArchiveSHA256 = "abc123"
)
`
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("failed to write fixture: %v", err)
		}

		_, err := parseBundleMetadata(path)
		if err == nil {
			t.Fatal("expected parseBundleMetadata to fail for non-string const")
		}
		if !errors.Is(err, errBundleMetadataInvalid) {
			t.Fatalf("expected errBundleMetadataInvalid, got: %v", err)
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

func TestParseArgs(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		options, err := parseArgs(nil)
		if err != nil {
			t.Fatalf("parseArgs returned error: %v", err)
		}
		if options.checkOnly {
			t.Fatal("expected checkOnly to be false")
		}
		if options.version != "" {
			t.Fatalf("expected empty version, got %q", options.version)
		}
	})

	t.Run("check only with single hyphen", func(t *testing.T) {
		options, err := parseArgs([]string{"-check"})
		if err != nil {
			t.Fatalf("parseArgs returned error: %v", err)
		}
		if !options.checkOnly {
			t.Fatal("expected checkOnly to be true")
		}
		if options.version != "" {
			t.Fatalf("expected empty version, got %q", options.version)
		}
	})

	t.Run("check only with double hyphen", func(t *testing.T) {
		options, err := parseArgs([]string{"--check"})
		if err != nil {
			t.Fatalf("parseArgs returned error: %v", err)
		}
		if !options.checkOnly {
			t.Fatal("expected checkOnly to be true")
		}
		if options.version != "" {
			t.Fatalf("expected empty version, got %q", options.version)
		}
	})

	t.Run("check with version", func(t *testing.T) {
		options, err := parseArgs([]string{"-check", "v0.30.3"})
		if err != nil {
			t.Fatalf("parseArgs returned error: %v", err)
		}
		if !options.checkOnly {
			t.Fatal("expected checkOnly to be true")
		}
		if options.version != "v0.30.3" {
			t.Fatalf("unexpected version: got %q", options.version)
		}
	})

	t.Run("unknown flag", func(t *testing.T) {
		_, err := parseArgs([]string{"--unknown"})
		if !errors.Is(err, errUsage) {
			t.Fatalf("expected errUsage, got: %v", err)
		}
	})

	t.Run("multiple versions", func(t *testing.T) {
		_, err := parseArgs([]string{"v0.30.3", "v0.30.4"})
		if !errors.Is(err, errUsage) {
			t.Fatalf("expected errUsage, got: %v", err)
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

func TestCheckBundledAssets(t *testing.T) {
	version := "v0.30.3"

	t.Run("success", func(t *testing.T) {
		t.Chdir(t.TempDir())
		createBundledFixture(t, version)

		if err := checkBundledAssets(version); err != nil {
			t.Fatalf("checkBundledAssets returned error: %v", err)
		}
	})

	t.Run("missing asset", func(t *testing.T) {
		t.Chdir(t.TempDir())
		createBundledFixture(t, version)

		missingPath := filepath.Join(bundledDirName, assets[0])
		if err := os.Remove(missingPath); err != nil {
			t.Fatalf("failed to remove fixture asset: %v", err)
		}

		err := checkBundledAssets(version)
		if err == nil {
			t.Fatal("expected checkBundledAssets to fail when asset is missing")
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected os.ErrNotExist, got: %v", err)
		}
	})

	t.Run("checksum mismatch", func(t *testing.T) {
		t.Chdir(t.TempDir())
		createBundledFixture(t, version)

		targetPath := filepath.Join(bundledDirName, assets[0])
		if err := os.WriteFile(targetPath, []byte("tampered"), 0o600); err != nil {
			t.Fatalf("failed to tamper fixture asset: %v", err)
		}

		err := checkBundledAssets(version)
		if err == nil {
			t.Fatal("expected checkBundledAssets to fail on checksum mismatch")
		}
		if !errors.Is(err, errChecksumMismatch) {
			t.Fatalf("expected errChecksumMismatch, got: %v", err)
		}
	})

	t.Run("pinned checksum mismatch", func(t *testing.T) {
		t.Chdir(t.TempDir())
		createBundledFixture(t, version)
		writeBundleMetadataFixture(t, assets[0], strings.Repeat("0", 64))

		err := checkBundledAssets(version)
		if err == nil {
			t.Fatal("expected checkBundledAssets to fail on pinned checksum mismatch")
		}
		if !errors.Is(err, errPinnedChecksumMismatch) {
			t.Fatalf("expected errPinnedChecksumMismatch, got: %v", err)
		}
	})
}

func createBundledFixture(t *testing.T, version string) {
	t.Helper()

	if err := os.MkdirAll(bundledDirName, 0o755); err != nil {
		t.Fatalf("failed to create %s: %v", bundledDirName, err)
	}

	lines := make([]string, 0, len(assets))
	for _, asset := range assets {
		body := []byte("fixture-" + asset)
		assetPath := filepath.Join(bundledDirName, asset)
		if err := os.WriteFile(assetPath, body, 0o600); err != nil {
			t.Fatalf("failed to write fixture asset %s: %v", asset, err)
		}

		sum := sha256.Sum256(body)
		hexSum := hex.EncodeToString(sum[:])
		lines = append(lines, hexSum+" "+asset)
		writeBundleMetadataFixture(t, asset, hexSum)
	}

	checksumsPath := filepath.Join(bundledDirName, checksumsAssetName(version))
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(checksumsPath, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write fixture checksums: %v", err)
	}
}

func writeBundleMetadataFixture(t *testing.T, asset string, checksum string) {
	t.Helper()

	fileName := "bundle_fixture_" + strings.NewReplacer(".", "_", "-", "_").Replace(asset) + ".go"
	filePath := fileName
	fileContent := fmt.Sprintf(
		`package main

const (
	bundledMCPArchiveName    = %q
	bundledMCPArchiveSHA256  = %q
	bundledMCPExecutableName = "github-mcp-server"
)
`,
		asset,
		checksum,
	)
	if err := os.WriteFile(filePath, []byte(fileContent), 0o600); err != nil {
		t.Fatalf("failed to write bundle metadata fixture for %s: %v", asset, err)
	}
}
