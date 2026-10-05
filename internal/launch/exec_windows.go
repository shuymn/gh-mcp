//go:build windows

package launch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
)

// Exec runs the server with inherited stdio and returns its exit code. Windows
// has no exec, so gh-mcp waits for the child. Ctrl+C reaches the child through
// the shared console, so the parent ignores it and keeps waiting.
func Exec(path string, args, env []string) (int, error) {
	// The child owns its lifetime; nothing cancels it from this side.
	cmd := exec.CommandContext(context.Background(), path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env

	// Notify suppresses default console termination on Windows; Ignore does not.
	// Notifications may be dropped: the child receives the same console events.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), nil
	default:
		return 1, fmt.Errorf("failed to run github-mcp-server: %w", err)
	}
}
