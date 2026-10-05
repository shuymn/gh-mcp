package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const (
	// StaleAfter is how long an executable for another digest may go unused
	// before an install removes it. Launchers record every use, so a digest in
	// active use is never stale.
	StaleAfter = 7 * 24 * time.Hour

	// useMarker records the last use by its modification time. It is separate
	// from the executable because updating the executable's times on Windows
	// opens it without read sharing, which would fail concurrent launches.
	useMarker = ".last-used"
)

var (
	// ErrNotBundled is returned when the cache misses and the build embeds no archive.
	ErrNotBundled = errors.New("this build does not bundle github-mcp-server")
	// ErrDigestMismatch is returned when an archive or executable does not match the lock.
	ErrDigestMismatch = errors.New("github-mcp-server digest mismatch")
	// ErrInsecureCache is returned when a cache directory fails privacy or type checks.
	ErrInsecureCache = errors.New("cache directory is insecure")

	errInstallContended = errors.New("cached executable kept changing during install")
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
		recordUse(dir)

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
	recordUse(dir)
	c.pruneStale(p.BinarySHA256)

	return target, nil
}

// recordUse marks the digest in dir as used now, so other launchers do not
// prune it as stale. It is best effort.
func recordUse(dir string) {
	marker := filepath.Join(dir, useMarker)
	now := time.Now()
	if err := os.Chtimes(marker, now, now); errors.Is(err, fs.ErrNotExist) {
		_ = os.WriteFile(marker, nil, 0o600)
	}
}

// pruneStale removes digests other than keep that have not been used for
// StaleAfter. A digest without a use marker (an interrupted install) ages by
// its directory. It is best effort: anything it cannot inspect or remove (for
// example a running executable on Windows) stays for a later install.
func (c Cache) pruneStale(keep string) {
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
		info, err := os.Lstat(filepath.Join(dir, useMarker))
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

	return publish(tmpPath, target, p.BinarySHA256)
}

// publish puts the finished file at target without ever replacing a valid one.
// A hard link fails when target exists, so concurrent installers never swap a
// file another launcher may be hashing or running; Windows refuses to replace
// an open file. Only a corrupt target is removed and linked again.
func publish(tmpPath, target, digest string) error {
	for range 2 {
		err := os.Link(tmpPath, target)
		if err == nil {
			return nil
		}
		if !errors.Is(err, fs.ErrExist) {
			// Filesystems without hard links fall back to an atomic rename.
			if err := os.Rename(tmpPath, target); err != nil && fileDigest(target) != digest {
				return fmt.Errorf("failed to install executable: %w", err)
			}
			return nil
		}
		if fileDigest(target) == digest {
			return nil
		}
		if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("failed to replace executable: %w", err)
		}
	}

	return fmt.Errorf("%w: %s", errInstallContended, target)
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
