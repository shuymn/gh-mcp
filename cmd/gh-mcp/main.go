package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

const mcpImage = "ghcr.io/github/github-mcp-server:latest"

func main() {
	os.Exit(mainRun())
}

func mainRun() int {
	// Set up a context that listens for termination signals
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Initialize slog with text handler for CLI output
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	if err := run(ctx); err != nil {
		slog.Error("Error", "err", err)
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
) error {
	return runServerContainer(ctx, cli, env, imageName)
}

func run(ctx context.Context) error {
	return runWithRunner(ctx, &realRunner{})
}

func runWithRunner(ctx context.Context, r runner) error {
	// 1. Get Auth
	slog.Info("🔐 Retrieving GitHub credentials...")
	auth, err := r.getAuth()
	if err != nil {
		return err
	}
	slog.Info("✅ Authenticated", "host", auth.Host)

	// 2. Init Docker client
	slog.Info("🐳 Connecting to Docker...")
	cli, err := r.newDockerClient()
	if err != nil {
		return err
	}
	defer cli.Close()
	slog.Info("✅ Docker client connected")

	// 3. Ensure image exists
	slog.Info("📦 Checking for MCP server image...")
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
		"GITHUB_DYNAMIC_TOOLSETS",
		"GITHUB_READ_ONLY",
	}

	for _, envVar := range optionalEnvVars {
		if value := os.Getenv(envVar); value != "" {
			env = append(env, envVar+"="+value)
		}
	}

	// 5. Run the container and stream I/O
	slog.Info("✅ Ready! Starting MCP server...")
	if err := r.runContainer(ctx, cli, env, mcpImage); err != nil {
		return err
	}

	slog.Info("👋 Session ended.")
	return nil
}
