package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
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

	if err := run(ctx, opts); err != nil {
		slog.ErrorContext(ctx, "Error", "err", err)
		return 1
	}
	return 0
}

// runner interface for dependency injection
type runner interface {
	getAuth() (*authDetails, error)
	newDockerClient() (dockerClientInterface, error)
	newDockerClientWithHost(host string) (dockerClientInterface, error)
	ensureImage(ctx context.Context, cli dockerClientInterface, imageName string, progress io.Writer) error
	podmanEnsureImageCLI(ctx context.Context, imageName string, streams *ioStreams) error
	podmanRunCLI(ctx context.Context, env []string, imageName string, streams *ioStreams) error
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

func (r *realRunner) newDockerClientWithHost(host string) (dockerClientInterface, error) {
	return newDockerClientWithHost(host)
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
	progress io.Writer,
) error {
	return ensureImage(ctx, cli, imageName, progress)
}

func (r *realRunner) podmanEnsureImageCLI(ctx context.Context, imageName string, streams *ioStreams) error {
	return podmanEnsureImageCLI(ctx, imageName, streams)
}

func (r *realRunner) podmanRunCLI(
	ctx context.Context,
	env []string,
	imageName string,
	streams *ioStreams,
) error {
	return podmanRunCLI(ctx, env, imageName, streams)
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

func run(ctx context.Context, opts options) error {
	return runWithRunner(ctx, &realRunner{}, opts)
}

func runWithRunner(ctx context.Context, r runner, opts options) error {
	streams := defaultIOStreams()

	// 1. Get Auth
	slog.InfoContext(ctx, "🔐 Retrieving GitHub credentials...")
	auth, err := r.getAuth()
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "✅ Authenticated", "host", auth.Host)

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

	switch opts.engine {
	case engineDocker:
		if err := runDocker(ctx, r, env, mcpImage, streams); err != nil {
			return err
		}
	case enginePodman:
		if err := runPodman(ctx, r, env, mcpImage, streams); err != nil {
			return err
		}
	case engineAuto:
		if err := runDocker(ctx, r, env, mcpImage, streams); err != nil {
			if errors.Is(err, ErrDockerUnavailable) {
				slog.WarnContext(ctx, "Docker unavailable, falling back to Podman")
				return runPodman(ctx, r, env, mcpImage, streams)
			}
			return err
		}
	default:
		return fmt.Errorf("unknown engine: %q", opts.engine)
	}

	slog.InfoContext(ctx, "👋 Session ended.")
	return nil
}

func runDocker(
	ctx context.Context,
	r runner,
	env []string,
	imageName string,
	streams *ioStreams,
) error {
	slog.InfoContext(ctx, "🐳 Connecting to Docker...")
	cli, err := r.newDockerClient()
	if err != nil {
		return err
	}
	defer cli.Close()

	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := cli.Ping(pingCtx); err != nil {
		return fmt.Errorf("%w: %w", ErrDockerUnavailable, err)
	}
	slog.InfoContext(ctx, "✅ Docker client connected")

	slog.InfoContext(ctx, "📦 Checking for MCP server image...")
	if err := r.ensureImage(ctx, cli, imageName, streams.err); err != nil {
		return err
	}

	slog.InfoContext(ctx, "✅ Ready! Starting MCP server...")
	return r.runContainer(ctx, cli, env, imageName, streams)
}

func runPodmanSocket(
	ctx context.Context,
	r runner,
	env []string,
	imageName string,
	streams *ioStreams,
) error {
	slog.InfoContext(ctx, "🦭 Connecting to Podman (socket)...")
	cli, host, err := newPodmanDockerClient(ctx, r)
	if err != nil {
		return err
	}
	defer cli.Close()

	slog.InfoContext(ctx, "✅ Podman docker API reachable", "host", host)
	slog.InfoContext(ctx, "📦 Checking for MCP server image...")
	if err := r.ensureImage(ctx, cli, imageName, streams.err); err != nil {
		return err
	}

	slog.InfoContext(ctx, "✅ Ready! Starting MCP server...")
	return r.runContainer(ctx, cli, env, imageName, streams)
}

func runPodman(
	ctx context.Context,
	r runner,
	env []string,
	imageName string,
	streams *ioStreams,
) error {
	err := runPodmanSocket(ctx, r, env, imageName, streams)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrPodmanSocketUnavailable) {
		return err
	}

	slog.WarnContext(ctx, "Podman socket unavailable, falling back to podman CLI")
	if err := r.podmanEnsureImageCLI(ctx, imageName, streams); err != nil {
		return err
	}
	return r.podmanRunCLI(ctx, env, imageName, streams)
}
