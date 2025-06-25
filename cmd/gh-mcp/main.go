package main

import (
	"context"
	"fmt"
	"log"
	"os/signal"
	"syscall"
)

const mcpImage = "ghcr.io/github/github-mcp-server:latest"

func main() {
	// Set up a context that listens for termination signals
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

// runner interface for dependency injection
type runner interface {
	getAuth() (*authDetails, error)
	newDockerClient() (dockerClientInterface, error)
	ensureImage(ctx context.Context, cli dockerClientInterface, imageName string) error
	runContainer(ctx context.Context, cli dockerClientInterface, env []string, imageName string) error
}

// realRunner implements runner using actual implementations
type realRunner struct{}

func (r *realRunner) getAuth() (*authDetails, error) {
	return getAuthDetails()
}

func (r *realRunner) newDockerClient() (dockerClientInterface, error) {
	return newDockerClient()
}

func (r *realRunner) ensureImage(ctx context.Context, cli dockerClientInterface, imageName string) error {
	return ensureImage(ctx, cli, imageName)
}

func (r *realRunner) runContainer(ctx context.Context, cli dockerClientInterface, env []string, imageName string) error {
	return runServerContainer(ctx, cli, env, imageName)
}

func run(ctx context.Context) error {
	return runWithRunner(ctx, &realRunner{})
}

func runWithRunner(ctx context.Context, r runner) error {
	// 1. Get Auth
	fmt.Println("🔐 Retrieving GitHub credentials...")
	auth, err := r.getAuth()
	if err != nil {
		return err
	}
	fmt.Printf("✅ Authenticated with %s\n", auth.Host)

	// 2. Init Docker client
	fmt.Println("🐳 Connecting to Docker...")
	cli, err := r.newDockerClient()
	if err != nil {
		return err
	}
	defer cli.Close()
	fmt.Println("✅ Docker client connected")

	// 3. Ensure image exists
	fmt.Println("📦 Checking for MCP server image...")
	if err := r.ensureImage(ctx, cli, mcpImage); err != nil {
		return err
	}

	// 4. Prepare environment
	env := []string{
		fmt.Sprintf("GITHUB_PERSONAL_ACCESS_TOKEN=%s", auth.Token),
		fmt.Sprintf("GITHUB_HOST=%s", auth.Host),
	}

	// 5. Run the container and stream I/O
	fmt.Println("✅ Ready! Starting MCP server...")
	if err := r.runContainer(ctx, cli, env, mcpImage); err != nil {
		return err
	}

	fmt.Println("👋 Session ended.")
	return nil
}
