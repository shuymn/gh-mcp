package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

const mcpImage = "ghcr.io/github/github-mcp-server@sha256:a2b5fb79b1cee851bfc3532dfe480c3dc5736974ca9d93a7a9f68e52ce4b62a0" // v0.30.3

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

	opts, err := parseOptions(os.Args[1:])
	if err != nil {
		// flag.FlagSet will have already written details to stderr (via fs.SetOutput)
		return 2
	}
	_ = opts

	if err := run(ctx); err != nil {
		slog.ErrorContext(ctx, "Error", "err", err)
		return 1
	}
	return 0
}

// runner interface for dependency injection
type runner interface {
	getAuth() (*authDetails, error)
	newDockerClient() (dockerClientInterface, error)
	ensureImage(ctx context.Context, cli dockerClientInterface, imageName string) error
	runContainer(
		ctx context.Context,
		cli dockerClientInterface,
		env []string,
		imageName string,
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

func (r *realRunner) newDockerClient() (dockerClientInterface, error) {
	return newDockerClient()
}

func (r *realRunner) getAuthInterface() authInterface {
	if r.authInterface == nil {
		r.authInterface = &realAuth{}
	}
	return r.authInterface
}

func (r *realRunner) ensureImage(
	ctx context.Context,
	cli dockerClientInterface,
	imageName string,
) error {
	return ensureImage(ctx, cli, imageName)
}

func (r *realRunner) runContainer(
	ctx context.Context,
	cli dockerClientInterface,
	env []string,
	imageName string,
	streams *ioStreams,
) error {
	return runServerContainer(ctx, cli, env, imageName, streams)
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

	// 2. Init Docker client
	slog.InfoContext(ctx, "🐳 Connecting to Docker...")
	cli, err := r.newDockerClient()
	if err != nil {
		return err
	}
	defer cli.Close()
	slog.InfoContext(ctx, "✅ Docker client connected")

	// 3. Ensure image exists
	slog.InfoContext(ctx, "📦 Checking for MCP server image...")
	if err := r.ensureImage(ctx, cli, mcpImage); err != nil {
		return err
	}

	// 4. Prepare environment
	env := []string{
		"GITHUB_PERSONAL_ACCESS_TOKEN=" + auth.Token,
		"GITHUB_HOST=" + auth.Host,
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
			env = append(env, envVar+"="+value)
		}
	}

	// 5. Run the container and stream I/O
	slog.InfoContext(ctx, "✅ Ready! Starting MCP server...")
	if err := r.runContainer(ctx, cli, env, mcpImage, defaultIOStreams()); err != nil {
		return err
	}

	slog.InfoContext(ctx, "👋 Session ended.")
	return nil
}
