package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/containerd/containerd/errdefs"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Define static errors
var (
	// ErrContainerNonZeroExit is a sentinel error for container non-zero exit
	ErrContainerNonZeroExit = errors.New("container exited with non-zero status")
)

// dockerClientInterface defines the methods we need from the Docker client for testing
type dockerClientInterface interface {
	ImageInspectWithRaw(ctx context.Context, imageID string) (image.InspectResponse, []byte, error)
	ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)
	ContainerCreate(
		ctx context.Context,
		config *container.Config,
		hostConfig *container.HostConfig,
		networkingConfig *network.NetworkingConfig,
		platform *ocispec.Platform,
		containerName string,
	) (container.CreateResponse, error)
	ContainerAttach(
		ctx context.Context,
		container string,
		options container.AttachOptions,
	) (types.HijackedResponse, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerWait(
		ctx context.Context,
		containerID string,
		condition container.WaitCondition,
	) (<-chan container.WaitResponse, <-chan error)
	ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
	Close() error
}

// realDockerClient wraps the actual Docker client to implement our interface
type realDockerClient struct {
	*client.Client
}

// newDockerClient creates a new Docker client configured from the environment
func newDockerClient() (dockerClientInterface, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf(
			"failed to create docker client: %w. Is the Docker daemon running?",
			err,
		)
	}
	return &realDockerClient{cli}, nil
}

// ensureImage checks if the image exists locally and pulls it if necessary
func ensureImage(ctx context.Context, cli dockerClientInterface, imageName string) error {
	slog.Info("Checking for image", "image", imageName)

	_, _, err := cli.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		slog.Info("✓ Image found locally")
		return nil // Image exists
	}

	if !errdefs.IsNotFound(err) {
		return fmt.Errorf("failed to inspect image: %w", err)
	}

	slog.Info("⬇️  Pulling image (this may take a moment)...")
	reader, err := cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull docker image '%s': %w", imageName, err)
	}
	defer reader.Close()

	// Pipe the pull output to the user's terminal for progress
	if _, err := io.Copy(os.Stdout, reader); err != nil {
		return fmt.Errorf("failed to read image pull progress: %w", err)
	}

	slog.Info("✓ Image pulled successfully")
	return nil
}

// runServerContainer creates and runs the MCP server container with I/O streaming
func runServerContainer(
	ctx context.Context,
	cli dockerClientInterface,
	env []string,
	imageName string,
) error {
	// 1. Create the container
	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image:        imageName,
		Env:          env,
		Cmd:          []string{"stdio"},
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		OpenStdin:    true,
		Tty:          false, // Important for piping stdio
	}, &container.HostConfig{
		AutoRemove: true,
	}, nil, nil, "")
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	// 2. Attach to the container's streams
	hijackedResp, err := cli.ContainerAttach(ctx, resp.ID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return fmt.Errorf("failed to attach to container: %w", err)
	}
	defer hijackedResp.Close()

	// 3. Start the container
	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}
	slog.Info("🚀 Starting github-mcp-server in Docker. Press Ctrl+C to exit.")

	// 4. Set up concurrent I/O streaming
	// Channel to signal when stdin is closed
	stdinClosed := make(chan struct{})

	// Copy output from container to terminal
	go func() {
		// StdCopy demultiplexes the container's stdout and stderr streams.
		_, err := stdcopy.StdCopy(os.Stdout, os.Stderr, hijackedResp.Reader)
		if err != nil && !errors.Is(err, io.EOF) {
			// Only log errors if context is not canceled
			select {
			case <-ctx.Done():
				// Context was canceled, we're shutting down - ignore error
			default:
				slog.Error("Error reading from container", "err", err)
			}
		}
	}()

	// Copy input from terminal to container
	go func() {
		_, err := io.Copy(hijackedResp.Conn, os.Stdin)
		// When stdin is closed (EOF), signal that we should stop
		if err == nil || errors.Is(err, io.EOF) {
			close(stdinClosed)
		} else {
			select {
			case <-ctx.Done():
				// Context was canceled, we're shutting down - ignore error
			default:
				slog.Error("Error writing to container", "err", err)
			}
		}
		if err := hijackedResp.CloseWrite(); err != nil {
			select {
			case <-ctx.Done():
				// Context was canceled, we're shutting down - ignore error
			default:
				slog.Error("Error closing write to container", "err", err)
			}
		}
	}()

	// 5. Wait for the container to exit
	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)

	// Helper to stop the container gracefully
	stopContainer := func() {
		stopCtx := context.Background()
		if err := cli.ContainerStop(stopCtx, resp.ID, container.StopOptions{}); err != nil {
			slog.Warn("Failed to stop container", "err", err)
		}
	}

	select {
	case err := <-errCh:
		if err != nil {
			// Ignore context canceled errors - these happen when user presses Ctrl+C
			if errors.Is(err, context.Canceled) {
				stopContainer()
				return nil
			}
			return fmt.Errorf("error waiting for container: %w", err)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			return fmt.Errorf("%w: %d", ErrContainerNonZeroExit, status.StatusCode)
		}
	case <-stdinClosed:
		// Stdin was closed (EOF) - Claude Code terminated
		stopContainer()
		return nil
	case <-ctx.Done():
		// Context was canceled (Ctrl+C pressed)
		stopContainer()
		return nil
	}

	return nil
}
