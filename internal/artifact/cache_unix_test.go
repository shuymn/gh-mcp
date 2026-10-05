//go:build !windows

package artifact_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shuymn/gh-mcp/internal/artifact"
)

func TestEnsureRejectsExposedCacheDirectories(t *testing.T) {
	for _, component := range []string{"root", "servers", "digest"} {
		t.Run(component, func(t *testing.T) {
			p, archive := fixture(t, "server")
			cache := artifact.Cache{Root: privateDir(t)}
			path, err := cache.Ensure(p, testGOOS, archive)
			if err != nil {
				t.Fatal(err)
			}
			dir := cache.Root
			switch component {
			case "servers":
				dir = filepath.Join(dir, "servers")
			case "digest":
				dir = filepath.Dir(path)
			}
			if err := os.Chmod(dir, 0o777); err != nil {
				t.Fatal(err)
			}
			if _, err := cache.Ensure(
				p,
				testGOOS,
				archive,
			); !errors.Is(
				err,
				artifact.ErrInsecureCache,
			) {
				t.Errorf("exposed %s error = %v, want ErrInsecureCache", component, err)
			}
			info, err := os.Stat(dir)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o777 {
				t.Errorf("unsafe directory permissions were silently repaired: %v", info.Mode())
			}
		})
	}
}

func TestEnsureRejectsSymlinkDescendants(t *testing.T) {
	for _, component := range []string{"servers", "digest"} {
		t.Run(component, func(t *testing.T) {
			p, archive := fixture(t, "server")
			cache := artifact.Cache{Root: privateDir(t)}
			external := artifact.Cache{Root: privateDir(t)}
			path, err := external.Ensure(p, testGOOS, archive)
			if err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(cache.Root, "servers")
			target := filepath.Join(external.Root, "servers")
			if component == "digest" {
				if err := os.Mkdir(link, 0o700); err != nil {
					t.Fatal(err)
				}
				link = filepath.Join(link, p.BinarySHA256)
				target = filepath.Dir(path)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			if _, err := cache.Ensure(
				p,
				testGOOS,
				archive,
			); !errors.Is(
				err,
				artifact.ErrInsecureCache,
			) {
				t.Errorf("symlink %s error = %v, want ErrInsecureCache", component, err)
			}
		})
	}
}

func TestEnsureRepairsMissingExecutePermission(t *testing.T) {
	p, archive := fixture(t, "server")
	cache := artifact.Cache{Root: privateDir(t)}
	path, err := cache.Ensure(p, testGOOS, archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Ensure(p, testGOOS, archive); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("executable mode = %v, want 0700", info.Mode())
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "server" {
		t.Errorf("repaired executable = %q, %v; want server", got, err)
	}
}

func TestEnsureReplacesSymlinkExecutable(t *testing.T) {
	p, archive := fixture(t, "server")
	cache := artifact.Cache{Root: privateDir(t)}
	dir := filepath.Join(cache.Root, "servers", p.BinarySHA256)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "server")
	if err := os.WriteFile(external, []byte("server"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "github-mcp-server")
	if err := os.Symlink(external, path); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Ensure(p, testGOOS, archive); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("executable still points outside the cache: %v", info.Mode())
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "server" {
		t.Errorf("installed executable = %q, %v; want server", got, err)
	}
}

func TestEnsureIgnoresUndeletableStaleVersions(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	cache := artifact.Cache{Root: privateDir(t)}
	staleP, staleArchive := fixture(t, "server-v1")
	newP, newArchive := fixture(t, "server-v2")

	stalePath, err := cache.Ensure(staleP, testGOOS, staleArchive)
	if err != nil {
		t.Fatal(err)
	}
	staleDir := filepath.Dir(stalePath)
	old := time.Now().Add(-artifact.StaleAfter - time.Hour)
	if err := os.Chtimes(stalePath, old, old); err != nil {
		t.Fatal(err)
	}
	// Without write permission the executable inside cannot be removed.
	if err := os.Chmod(staleDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(staleDir, 0o700) })

	if _, err := cache.Ensure(newP, testGOOS, newArchive); err != nil {
		t.Fatalf("Ensure failed because pruning failed: %v", err)
	}
	if _, err := os.Stat(stalePath); err != nil {
		t.Errorf("undeletable stale executable: %v; want it left in place", err)
	}
}
