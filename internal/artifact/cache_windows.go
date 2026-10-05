//go:build windows

package artifact

import "os"

// checkCacheOwner relies on the per-user ACLs of %LocalAppData% on Windows.
func checkCacheOwner(string, os.FileInfo) error {
	return nil
}
