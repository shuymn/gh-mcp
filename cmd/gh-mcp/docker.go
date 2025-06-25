package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// dockerClientInterface defines the methods we need from the Docker client for testing
type dockerClientInterface interface {
	ImageInspectWithRaw(ctx context.Context, imageID string) (types.ImageInspect, []byte, error)
	ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error)
	ContainerAttach(ctx context.Context, container string, options container.AttachOptions) (types.HijackedResponse, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error)
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
		return nil, fmt.Errorf("failed to create docker client: %w. Is the Docker daemon running?", err)
	}
	return &realDockerClient{cli}, nil
}

// ensureImage checks if the image exists locally and pulls it if necessary
func ensureImage(ctx context.Context, cli dockerClientInterface, imageName string) error {
	fmt.Printf("Checking for image: %s...\n", imageName)

	_, _, err := cli.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		fmt.Println("✓ Image found locally.")
		return nil // Image exists
	}

	if !client.IsErrNotFound(err) {
		return fmt.Errorf("failed to inspect image: %w", err)
	}

	fmt.Printf("⬇️  Pulling image (this may take a moment)...\n")
	reader, err := cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull docker image '%s': %w", imageName, err)
	}
	defer reader.Close()

	// Pipe the pull output to the user's terminal for progress
	if _, err := io.Copy(os.Stdout, reader); err != nil {
		return fmt.Errorf("failed to read image pull progress: %w", err)
	}

	fmt.Println("\n✓ Image pulled successfully.")
	return nil
}

// runServerContainer creates and runs the MCP server container with I/O streaming
func runServerContainer(ctx context.Context, cli dockerClientInterface, env []string, imageName string) error {
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
	fmt.Println("🚀 Starting github-mcp-server in Docker. Press Ctrl+C to exit.")

	// 4. Set up concurrent I/O streaming
	// Copy output from container to terminal
	go func() {
		// StdCopy demultiplexes the container's stdout and stderr streams.
		_, err := stdcopy.StdCopy(os.Stdout, os.Stderr, hijackedResp.Reader)
		if err != nil && err != io.EOF {
			fmt.Fprintf(os.Stderr, "Error reading from container: %v\n", err)
		}
	}()

	// Copy input from terminal to container
	go func() {
		_, err := io.Copy(hijackedResp.Conn, os.Stdin)
		if err != nil && err != io.EOF {
			fmt.Fprintf(os.Stderr, "Error writing to container: %v\n", err)
		}
		hijackedResp.CloseWrite() // Signal end of input
	}()

	// 5. Wait for the container to exit
	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("error waiting for container: %w", err)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			return fmt.Errorf("container exited with non-zero status: %d", status.StatusCode)
		}
	}

	return nil
}
