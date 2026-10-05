//go:build !windows

package launch

import (
	"fmt"
	"syscall"
)

// Exec replaces the current process with the server. It returns only on failure.
func Exec(path string, args, env []string) (int, error) {
	// #nosec G204 -- path is the digest-verified server; args are the user's own gh mcp arguments.
	err := syscall.Exec(path, append([]string{path}, args...), env)

	return 1, fmt.Errorf("failed to exec github-mcp-server: %w", err)
}
