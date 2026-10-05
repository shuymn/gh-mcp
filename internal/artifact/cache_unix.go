//go:build !windows

package artifact

import (
	"fmt"
	"os"
	"syscall"
)

// checkCacheOwner rejects foreign-owned or exposed paths. Tightening permissions
// cannot make existing contents trustworthy after another user could alter them.
func checkCacheOwner(root string, info os.FileInfo) error {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int64(stat.Uid) != int64(os.Geteuid()) {
		return fmt.Errorf("%w: %s must be owned by the current user", ErrInsecureCache, root)
	}
	if info.Mode().IsRegular() && info.Mode().Perm()&0o100 == 0 {
		return fmt.Errorf("%w: %s must be executable by the current user", ErrInsecureCache, root)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: %s must not be accessible by other users", ErrInsecureCache, root)
	}

	return nil
}
