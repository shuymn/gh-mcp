package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// StaleAfter is how long an executable for another digest may go unused before
// an install removes it. Launchers refresh the modification time on every use,
// so a digest in active use is never stale.
const StaleAfter = 7 * 24 * time.Hour

var (
	// ErrNotBundled is returned when the cache misses and the build embeds no archive.
	ErrNotBundled = errors.New("this build does not bundle github-mcp-server")
	// ErrDigestMismatch is returned when an archive or executable does not match the lock.
	ErrDigestMismatch = errors.New("github-mcp-server digest mismatch")
	// ErrInsecureCache is returned when a cache directory fails privacy or type checks.
	ErrInsecureCache = errors.New("cache directory is insecure")
)

// Cache stores verified executables under Root/servers/<binary-sha256>/.
type Cache struct {
	Root string
}

// Ensure returns the path of the verified executable for p, extracting it from
// archive when the cache does not already hold a matching file. A fresh install
// also removes other digests unused for StaleAfter. Pruning can still race with
// a launcher of a long-unused version; such a launcher sees fs.ErrNotExist and
// may call Ensure again.
func (c Cache) Ensure(p Platform, goos string, archive []byte) (string, error) {
	dir := filepath.Join(c.Root, "servers", p.BinarySHA256)
	for _, path := range []string{c.Root, filepath.Join(c.Root, "servers"), dir} {
		if err := ensureCacheDir(path); err != nil {
			return "", err
		}
	}
	target := filepath.Join(dir, ExecutableName(goos))
	if fileDigest(target) == p.BinarySHA256 {
		// Record the use so other launchers do not prune this digest as stale.
		now := time.Now()
		_ = os.Chtimes(target, now, now)

		return target, nil
	}

	if len(archive) == 0 {
		return "", ErrNotBundled
	}
	if got := SHA256Hex(archive); got != p.ArchiveSHA256 {
		return "", fmt.Errorf("%w: archive %s expected=%s actual=%s",
			ErrDigestMismatch, p.Asset, p.ArchiveSHA256, got)
	}

	if err := install(dir, target, p, goos, archive); err != nil {
		return "", err
	}
	c.pruneStale(p.BinarySHA256, goos)

	return target, nil
}

// pruneStale removes digests other than keep whose executable has not been
// used for StaleAfter. It is best effort: anything it cannot inspect or remove
// (for example a running executable on Windows) stays for a later install.
func (c Cache) pruneStale(keep, goos string) {
	serversDir := filepath.Join(c.Root, "servers")
	entries, err := os.ReadDir(serversDir)
	if err != nil {
		return
	}

	cutoff := time.Now().Add(-StaleAfter)
	for _, entry := range entries {
		if entry.Name() == keep {
			continue
		}
		dir := filepath.Join(serversDir, entry.Name())
		// A directory without an executable (an interrupted install) ages by
		// its own modification time.
		info, err := os.Lstat(filepath.Join(dir, ExecutableName(goos)))
		if err != nil {
			info, err = os.Lstat(dir)
		}
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(dir)
		}
	}
}

func install(dir, target string, p Platform, goos string, archive []byte) error {
	tmp, err := os.CreateTemp(dir, ".install-*")
	if err != nil {
		return fmt.Errorf("failed to create cache file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	digest, err := ExtractExecutable(archive, p.Asset, ExecutableName(goos), tmp)
	if closeErr := tmp.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("failed to write cache file: %w", closeErr)
	}
	if err != nil {
		return err
	}
	if digest != p.BinarySHA256 {
		return fmt.Errorf("%w: executable expected=%s actual=%s",
			ErrDigestMismatch, p.BinarySHA256, digest)
	}
	if err := os.Chmod(tmpPath, 0o700); err != nil {
		return fmt.Errorf("failed to mark executable: %w", err)
	}

	if err := os.Rename(tmpPath, target); err != nil {
		// A concurrent launcher may have installed the same digest first, and
		// Windows refuses to replace an executable that is running.
		if fileDigest(target) == p.BinarySHA256 {
			return nil
		}

		return fmt.Errorf("failed to install executable: %w", err)
	}

	return nil
}

// fileDigest returns the hex SHA256 of a private, regular executable, or "" if
// the path is unsafe, non-executable, or unreadable.
func fileDigest(path string) string {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || checkCacheOwner(path, info) != nil {
		return ""
	}

	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return ""
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

func ensureCacheDir(root string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("failed to inspect cache directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: %s must be a directory, not a symbolic link", ErrInsecureCache, root)
	}

	return checkCacheOwner(root, info)
}
