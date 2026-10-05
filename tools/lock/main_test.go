package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/shuymn/gh-mcp/internal/artifact"
)

func TestBuildCommandsRejectLockFlag(t *testing.T) {
	for _, command := range []string{"stage", "dist"} {
		t.Run(command, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeLock(t, buildLockPath, testLock())
			// Reject before any downloads or mutation of the staged payload.
			if err := os.Mkdir("payload", 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(payloadPath, []byte("existing payload"), 0o644); err != nil {
				t.Fatal(err)
			}

			err := run(t.Context(), []string{command, "-lock", "alternate.json", "-out", "dist"})
			if !errors.Is(err, errUsage) {
				t.Fatalf("run(%s -lock) error = %v, want errUsage", command, err)
			}
			if got, err := os.ReadFile(
				payloadPath,
			); err != nil ||
				string(got) != "existing payload" {
				t.Errorf("payload = %q, %v; want existing payload", got, err)
			}
		})
	}
}

func testLock() artifact.Lock {
	return artifact.Lock{
		Version: "v1.2.3",
		Platforms: map[string]artifact.Platform{"linux/amd64": {
			Asset:         "server.tar.gz",
			ArchiveSHA256: strings.Repeat("a", 64),
			BinarySHA256:  strings.Repeat("a", 64),
		}},
	}
}

func writeLock(t *testing.T, path string, lock artifact.Lock) {
	t.Helper()
	data, err := lock.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseChecksums(t *testing.T) {
	input := "abc  github-mcp-server_Linux_x86_64.tar.gz\n\nmalformed line here\ndef github-mcp-server_Windows_arm64.zip\n"

	got, err := parseChecksums(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["github-mcp-server_Linux_x86_64.tar.gz"] != "abc" ||
		got["github-mcp-server_Windows_arm64.zip"] != "def" {
		t.Errorf("parseChecksums() = %v", got)
	}
}
