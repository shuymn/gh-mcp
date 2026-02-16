package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

// ErrInvalidServerEnvValue is returned when an environment value is unsafe for process execution.
var ErrInvalidServerEnvValue = errors.New("invalid server environment value")

func main() {
	os.Exit(mainRun())
}

func parseLogLevel() slog.Level {
	levelStr := os.Getenv("LOG_LEVEL")
	if levelStr == "" {
		return slog.LevelInfo
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.ToUpper(levelStr))); err != nil {
		return slog.LevelInfo
	}

	return level
}

func mainRun() int {
	// Set up a context that listens for termination signals
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Initialize slog with text handler for CLI output
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: parseLogLevel(),
	}))
	slog.SetDefault(logger)

	if err := run(ctx); err != nil {
		slog.ErrorContext(ctx, "Error", "err", err)
		return 1
	}
	return 0
}

// runner interface for dependency injection
type runner interface {
	getAuth() (*authDetails, error)
	runServer(
		ctx context.Context,
		env []string,
		streams *ioStreams,
	) error
}

// realRunner implements runner using actual implementations
type realRunner struct {
	authInterface authInterface
}

func (r *realRunner) getAuth() (*authDetails, error) {
	return getAuthDetails(r.getAuthInterface())
}

func (r *realRunner) getAuthInterface() authInterface {
	if r.authInterface == nil {
		r.authInterface = &realAuth{}
	}
	return r.authInterface
}

func (r *realRunner) runServer(
	ctx context.Context,
	env []string,
	streams *ioStreams,
) error {
	return runBundledServer(ctx, env, streams)
}

func run(ctx context.Context) error {
	return runWithRunner(ctx, &realRunner{})
}

func runWithRunner(ctx context.Context, r runner) error {
	// 1. Get Auth
	slog.InfoContext(ctx, "🔐 Retrieving GitHub credentials...")
	auth, err := r.getAuth()
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "✅ Authenticated", "host", auth.Host)

	// 2. Validate bundled server version before startup.
	slog.InfoContext(ctx, "📦 Preparing bundled MCP server...", "version", mcpServerVersion)

	// 3. Prepare environment
	env := make([]string, 0, 2)
	env, err = appendServerEnv(env, "GITHUB_PERSONAL_ACCESS_TOKEN", auth.Token)
	if err != nil {
		return err
	}
	env, err = appendServerEnv(env, "GITHUB_HOST", auth.Host)
	if err != nil {
		return err
	}

	// Pass through optional environment variables if they are set
	optionalEnvVars := []string{
		"GITHUB_TOOLSETS",
		"GITHUB_TOOLS",
		"GITHUB_DYNAMIC_TOOLSETS",
		"GITHUB_READ_ONLY",
		"GITHUB_LOCKDOWN_MODE",
	}

	for _, envVar := range optionalEnvVars {
		if value := os.Getenv(envVar); value != "" {
			env, err = appendServerEnv(env, envVar, value)
			if err != nil {
				return err
			}
		}
	}

	// 4. Run the bundled server and stream I/O.
	slog.InfoContext(ctx, "✅ Ready! Starting MCP server...")
	if err := r.runServer(ctx, env, defaultIOStreams()); err != nil {
		return err
	}

	slog.InfoContext(ctx, "👋 Session ended.")
	return nil
}

func appendServerEnv(env []string, key, value string) ([]string, error) {
	if err := validateServerEnvValue(key, value); err != nil {
		return nil, err
	}

	return append(env, key+"="+value), nil
}

func validateServerEnvValue(key, value string) error {
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%w: %s contains NUL byte", ErrInvalidServerEnvValue, key)
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%w: %s contains line break", ErrInvalidServerEnvValue, key)
	}

	return nil
}
