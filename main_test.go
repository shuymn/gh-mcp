package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/shuymn/gh-mcp/internal/artifact"
	"github.com/shuymn/gh-mcp/internal/identity"
)

type execCall struct {
	path string
	args []string
	env  []string
}

func testLauncher(
	t *testing.T,
	token string,
	archive []byte,
	lock artifact.Lock,
) (*launcher, *execCall, *bytes.Buffer) {
	t.Helper()

	lockJSON, err := lock.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "gh-mcp")
	call := &execCall{}
	stderr := &bytes.Buffer{}

	return &launcher{
		args:      []string{"--read-only"},
		environ:   []string{"GITHUB_TOOLSETS=repos", "GH_TOKEN=leak"},
		goos:      "linux",
		goarch:    "amd64",
		stderr:    stderr,
		lock:      lockJSON,
		archive:   func() ([]byte, error) { return archive, nil },
		cacheRoot: func() (string, error) { return root, nil },
		identity: identity.Source{
			DefaultHost:  func() (string, string) { return "github.com", "" },
			TokenForHost: func(string) (string, string) { return token, "" },
		},
		exec: func(path string, args, env []string) (int, error) {
			*call = execCall{path, args, env}
			return 7, nil
		},
	}, call, stderr
}

func serverFixture(t *testing.T) ([]byte, artifact.Lock) {
	t.Helper()

	content := []byte("server")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(
		&tar.Header{
			Name:     "github-mcp-server",
			Mode:     0o755,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes(), artifact.Lock{
		Version: "v1.14.0",
		Platforms: map[string]artifact.Platform{"linux/amd64": {
			Asset:         "github-mcp-server_Linux_x86_64.tar.gz",
			ArchiveSHA256: artifact.SHA256Hex(buf.Bytes()),
			BinarySHA256:  artifact.SHA256Hex(content),
		}},
	}
}

func TestRunExecsServerWithCredentialsAndArgs(t *testing.T) {
	archive, lock := serverFixture(t)
	l, call, stderr := testLauncher(t, "gho_x", archive, lock)

	if code := l.run(); code != 7 {
		t.Fatalf("run() = %d, want the server's exit code 7; stderr=%s", code, stderr)
	}
	if !strings.HasSuffix(call.path, "github-mcp-server") {
		t.Errorf("exec path = %s", call.path)
	}
	if want := []string{"stdio", "--read-only"}; !slices.Equal(call.args, want) {
		t.Errorf("exec args = %v, want %v", call.args, want)
	}
	want := []string{
		"GITHUB_TOOLSETS=repos",
		"GITHUB_HOST=https://github.com",
		"GITHUB_PERSONAL_ACCESS_TOKEN=gho_x",
	}
	if !slices.Equal(call.env, want) {
		t.Errorf("exec env = %v, want %v", call.env, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want silence on success", stderr)
	}
}

func TestRunReportsErrorsWithoutStartingServer(t *testing.T) {
	archive, lock := serverFixture(t)

	tests := []struct {
		name    string
		token   string
		archive []byte
		goarch  string
		want    string
	}{
		{name: "not logged in", archive: archive, goarch: "amd64", want: "gh auth login"},
		{name: "no payload", token: "t", goarch: "amd64", want: "task build"},
		{
			name:    "unsupported platform",
			token:   "t",
			archive: archive,
			goarch:  "riscv64",
			want:    "linux/riscv64",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, call, stderr := testLauncher(t, tt.token, tt.archive, lock)
			l.goarch = tt.goarch

			if code := l.run(); code != 1 {
				t.Errorf("run() = %d, want 1", code)
			}
			if call.path != "" {
				t.Errorf("server started: %+v", call)
			}
			if !strings.HasPrefix(stderr.String(), "gh-mcp: ") ||
				!strings.Contains(stderr.String(), tt.want) {
				t.Errorf("stderr = %q, want a gh-mcp line mentioning %q", stderr, tt.want)
			}
		})
	}
}

func TestRunPreparesAgainWhenExecutableVanishes(t *testing.T) {
	archive, lock := serverFixture(t)

	tests := []struct {
		name      string
		archive   []byte
		failures  []error
		wantCalls int
		wantCode  int
		wantErr   string
	}{
		{
			name:      "vanished once is reinstalled",
			archive:   archive,
			failures:  []error{fs.ErrNotExist},
			wantCalls: 2,
			wantCode:  7,
		},
		{
			name:      "vanished twice gives up",
			archive:   archive,
			failures:  []error{fs.ErrNotExist, fs.ErrNotExist},
			wantCalls: 2,
			wantCode:  1,
			wantErr:   "file does not exist",
		},
		{
			name:      "permission error is not retried",
			archive:   archive,
			failures:  []error{fs.ErrPermission},
			wantCalls: 1,
			wantCode:  1,
			wantErr:   "permission denied",
		},
		{
			name:      "build without payload cannot reinstall",
			failures:  []error{fs.ErrNotExist},
			wantCalls: 1,
			wantCode:  1,
			wantErr:   "task build",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, _, stderr := testLauncher(t, "t", archive, lock)
			// Install once so a build without payload starts from a cache hit.
			if code := l.run(); code != 7 {
				t.Fatalf("warm-up run() = %d; stderr=%s", code, stderr)
			}
			l.archive = func() ([]byte, error) { return tt.archive, nil }

			calls := 0
			l.exec = func(path string, _, _ []string) (int, error) {
				calls++
				if calls > len(tt.failures) {
					return 7, nil
				}
				// Simulate a concurrent prune between Ensure and exec.
				if errors.Is(tt.failures[calls-1], fs.ErrNotExist) {
					if err := os.RemoveAll(filepath.Dir(path)); err != nil {
						t.Fatal(err)
					}
				}
				return 1, &fs.PathError{Op: "exec", Path: path, Err: tt.failures[calls-1]}
			}
			stderr.Reset()

			if code := l.run(); code != tt.wantCode {
				t.Errorf("run() = %d, want %d; stderr=%s", code, tt.wantCode, stderr)
			}
			if calls != tt.wantCalls {
				t.Errorf("exec calls = %d, want %d", calls, tt.wantCalls)
			}
			if !strings.Contains(stderr.String(), tt.wantErr) {
				t.Errorf("stderr = %q, want it to mention %q", stderr, tt.wantErr)
			}
		})
	}
}

func TestEmbeddedLockIsValid(t *testing.T) {
	if _, err := artifact.ParseLock(lockData); err != nil {
		t.Fatalf("server.lock.json: %v", err)
	}
}
