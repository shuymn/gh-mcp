package artifact_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shuymn/gh-mcp/internal/artifact"
)

const testGOOS = "linux"

func privateDir(t *testing.T) string {
	t.Helper()

	root := filepath.Join(t.TempDir(), "gh-mcp")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}

	return root
}

func tarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, entry := range []struct {
		name string
		body []byte
	}{{"README.md", []byte("readme")}, {name, content}} {
		if err := tw.WriteHeader(
			&tar.Header{
				Name:     entry.name,
				Mode:     0o755,
				Size:     int64(len(entry.body)),
				Typeflag: tar.TypeReg,
			},
		); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func zipArchive(t *testing.T, name string, content []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func fixture(t *testing.T, content string) (artifact.Platform, []byte) {
	t.Helper()

	archive := tarGz(t, "bin/github-mcp-server", []byte(content))

	return artifact.Platform{
		Asset:         "github-mcp-server_Linux_x86_64.tar.gz",
		ArchiveSHA256: artifact.SHA256Hex(archive),
		BinarySHA256:  artifact.SHA256Hex([]byte(content)),
	}, archive
}

func TestParseLock(t *testing.T) {
	digest := strings.Repeat("a", 64)
	valid := `{"version":"v1.2.3","platforms":{"linux/amd64":{"asset":"a.tar.gz","archive_sha256":"` +
		digest + `","binary_sha256":"` + digest + `"}}}`

	lock, err := artifact.ParseLock([]byte(valid))
	if err != nil {
		t.Fatalf("ParseLock(valid) error = %v", err)
	}
	if _, err := lock.Platform("linux", "amd64"); err != nil {
		t.Errorf("Platform(linux, amd64) error = %v", err)
	}
	if _, err := lock.Platform(
		"freebsd",
		"amd64",
	); !errors.Is(
		err,
		artifact.ErrUnsupportedPlatform,
	) {
		t.Errorf("Platform(freebsd, amd64) error = %v, want ErrUnsupportedPlatform", err)
	}

	for name, data := range map[string]string{
		"not json":      `{`,
		"no v prefix":   strings.Replace(valid, "v1.2.3", "1.2.3", 1),
		"no platforms":  `{"version":"v1.2.3","platforms":{}}`,
		"short digest":  strings.Replace(valid, digest, "abc", 1),
		"upper digest":  strings.Replace(valid, digest, strings.ToUpper(digest), 1),
		"missing asset": strings.Replace(valid, `"a.tar.gz"`, `""`, 1),
	} {
		if _, err := artifact.ParseLock([]byte(data)); !errors.Is(err, artifact.ErrInvalidLock) {
			t.Errorf("ParseLock(%s) error = %v, want ErrInvalidLock", name, err)
		}
	}
}

func TestExtractExecutable(t *testing.T) {
	content := []byte("server")
	for _, tc := range []struct {
		asset   string
		archive []byte
	}{
		{"x.tar.gz", tarGz(t, "dir/github-mcp-server", content)},
		{"x.zip", zipArchive(t, "dir/github-mcp-server", content)},
	} {
		var out bytes.Buffer
		digest, err := artifact.ExtractExecutable(tc.archive, tc.asset, "github-mcp-server", &out)
		if err != nil {
			t.Fatalf("%s: error = %v", tc.asset, err)
		}
		if out.String() != "server" || digest != artifact.SHA256Hex(content) {
			t.Errorf("%s: got %q digest %s", tc.asset, out.String(), digest)
		}

		if _, err := artifact.ExtractExecutable(
			tc.archive,
			tc.asset,
			"missing",
			&out,
		); !errors.Is(
			err,
			artifact.ErrExecutableNotFound,
		) {
			t.Errorf("%s: missing entry error = %v, want ErrExecutableNotFound", tc.asset, err)
		}
	}

	if _, err := artifact.ExtractExecutable(
		nil,
		"x.rar",
		"github-mcp-server",
		&bytes.Buffer{},
	); !errors.Is(
		err,
		artifact.ErrUnsupportedArchive,
	) {
		t.Errorf("rar error = %v, want ErrUnsupportedArchive", err)
	}
}

func TestEnsureInstallsThenReusesCache(t *testing.T) {
	p, archive := fixture(t, "server-v1")
	cache := artifact.Cache{Root: privateDir(t)}

	path, err := cache.Ensure(p, testGOOS, archive)
	if err != nil {
		t.Fatalf("first Ensure error = %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "server-v1" {
		t.Fatalf("installed content = %q", got)
	}
	if want := filepath.Join(
		cache.Root,
		"servers",
		p.BinarySHA256,
		"github-mcp-server",
	); path != want {
		t.Errorf("path = %s, want %s", path, want)
	}

	// A cache hit needs no archive, so builds without a payload still run.
	again, err := cache.Ensure(p, testGOOS, nil)
	if err != nil || again != path {
		t.Errorf("cached Ensure = %s, %v; want %s", again, err, path)
	}
}

func TestEnsureReplacesTamperedExecutable(t *testing.T) {
	p, archive := fixture(t, "server-v1")
	cache := artifact.Cache{Root: privateDir(t)}

	path, err := cache.Ensure(p, testGOOS, archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := cache.Ensure(p, testGOOS, nil); !errors.Is(err, artifact.ErrNotBundled) {
		t.Errorf("Ensure without archive error = %v, want ErrNotBundled", err)
	}
	if _, err := cache.Ensure(p, testGOOS, archive); err != nil {
		t.Fatalf("Ensure with archive error = %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "server-v1" {
		t.Errorf("content after repair = %q", got)
	}
}

func TestEnsureRejectsDigestMismatch(t *testing.T) {
	p, archive := fixture(t, "server-v1")

	badArchive := p
	badArchive.ArchiveSHA256 = strings.Repeat("0", 64)
	if _, err := (artifact.Cache{Root: privateDir(t)}).Ensure(
		badArchive,
		testGOOS,
		archive,
	); !errors.Is(
		err,
		artifact.ErrDigestMismatch,
	) {
		t.Errorf("archive mismatch error = %v, want ErrDigestMismatch", err)
	}

	badBinary := p
	badBinary.BinarySHA256 = strings.Repeat("0", 64)
	root := privateDir(t)
	if _, err := (artifact.Cache{Root: root}).Ensure(
		badBinary,
		testGOOS,
		archive,
	); !errors.Is(
		err,
		artifact.ErrDigestMismatch,
	) {
		t.Errorf("binary mismatch error = %v, want ErrDigestMismatch", err)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "servers", badBinary.BinarySHA256))
	if len(entries) != 0 {
		t.Errorf("rejected install left files: %v", entries)
	}
}

func TestEnsureRetainsOtherVersionsForPendingLaunches(t *testing.T) {
	cache := artifact.Cache{Root: privateDir(t)}
	oldP, oldArchive := fixture(t, "server-v1")
	newP, newArchive := fixture(t, "server-v2")

	oldPath, err := cache.Ensure(oldP, testGOOS, oldArchive)
	if err != nil {
		t.Fatal(err)
	}
	// Another launcher installs a new digest before the first one calls exec.
	if _, err := cache.Ensure(newP, testGOOS, newArchive); err != nil {
		t.Fatal(err)
	}

	if got, err := os.ReadFile(oldPath); err != nil || string(got) != "server-v1" {
		t.Errorf("pending launcher executable = %q, %v; want server-v1", got, err)
	}
}

// age makes the cache entry holding path (an executable or an entry
// directory) look last used `by` ago.
func age(t *testing.T, path string, by time.Duration) {
	t.Helper()

	dir := path
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		dir = filepath.Dir(path)
	}
	old := time.Now().Add(-by)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err := os.Chtimes(filepath.Join(dir, entry.Name()), old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)

	return err == nil
}

func TestEnsurePrunesStaleVersionsAfterInstall(t *testing.T) {
	cache := artifact.Cache{Root: privateDir(t)}
	staleP, staleArchive := fixture(t, "server-v1")
	recentP, recentArchive := fixture(t, "server-v2")
	newP, newArchive := fixture(t, "server-v3")

	stalePath, err := cache.Ensure(staleP, testGOOS, staleArchive)
	if err != nil {
		t.Fatal(err)
	}
	recentPath, err := cache.Ensure(recentP, testGOOS, recentArchive)
	if err != nil {
		t.Fatal(err)
	}
	// An install interrupted before the rename leaves a directory without an executable.
	interrupted := filepath.Join(cache.Root, "servers", strings.Repeat("e", 64))
	if err := os.Mkdir(interrupted, 0o700); err != nil {
		t.Fatal(err)
	}
	age(t, stalePath, artifact.StaleAfter+time.Hour)
	age(t, interrupted, artifact.StaleAfter+time.Hour)
	age(t, recentPath, artifact.StaleAfter-time.Hour)

	if _, err := cache.Ensure(newP, testGOOS, newArchive); err != nil {
		t.Fatal(err)
	}

	if exists(filepath.Dir(stalePath)) || exists(interrupted) {
		t.Error("stale digests survived a fresh install")
	}
	if !exists(recentPath) {
		t.Error("a digest used within StaleAfter was pruned")
	}
}

func TestEnsureCacheHitRecordsUse(t *testing.T) {
	cache := artifact.Cache{Root: privateDir(t)}
	usedP, usedArchive := fixture(t, "server-v1")
	newP, newArchive := fixture(t, "server-v2")

	usedPath, err := cache.Ensure(usedP, testGOOS, usedArchive)
	if err != nil {
		t.Fatal(err)
	}
	age(t, usedPath, artifact.StaleAfter+time.Hour)

	// A cache hit refreshes the use time, so the next install keeps this digest.
	if _, err := cache.Ensure(usedP, testGOOS, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Ensure(newP, testGOOS, newArchive); err != nil {
		t.Fatal(err)
	}

	if !exists(usedPath) {
		t.Error("a digest just used was pruned")
	}
}

func TestEnsureCacheHitDoesNotPrune(t *testing.T) {
	cache := artifact.Cache{Root: privateDir(t)}
	staleP, staleArchive := fixture(t, "server-v1")
	currentP, currentArchive := fixture(t, "server-v2")

	stalePath, err := cache.Ensure(staleP, testGOOS, staleArchive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Ensure(currentP, testGOOS, currentArchive); err != nil {
		t.Fatal(err)
	}
	age(t, stalePath, artifact.StaleAfter+time.Hour)

	if _, err := cache.Ensure(currentP, testGOOS, nil); err != nil {
		t.Fatal(err)
	}
	if !exists(stalePath) {
		t.Error("a cache hit pruned another digest; only fresh installs prune")
	}
}

func TestEnsureConcurrentFirstRun(t *testing.T) {
	p, archive := fixture(t, "server-v1")
	cache := artifact.Cache{Root: privateDir(t)}

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			path, err := cache.Ensure(p, testGOOS, archive)
			if err == nil {
				var got []byte
				if got, err = os.ReadFile(path); err == nil && string(got) != "server-v1" {
					err = errors.New("incomplete executable: " + string(got))
				}
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
}

func TestEnsureRejectsSymlinkRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}

	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "cache")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	p, archive := fixture(t, "server-v1")
	if _, err := (artifact.Cache{Root: link}).Ensure(
		p,
		testGOOS,
		archive,
	); !errors.Is(
		err,
		artifact.ErrInsecureCache,
	) {
		t.Errorf("symlink root error = %v, want ErrInsecureCache", err)
	}
}
