# **Technical Implementation Guide: gh-mcp**

Project Name: gh-mcp  
Target Audience: Worker Agents  
Last Updated: June 25, 2025

## **1. Overview**

This document provides detailed technical guidance for implementing the gh-mcp extension. It covers project structure, key library usage, code snippets, and error handling strategies.

## **2. Project Setup & Dependencies**

Initialize the Go project and add the necessary dependencies.

### Initialize Go module  

```bash
go mod init github.com/<your-username>/gh-mcp
```

### Add dependencies  

```bash
go get github.com/cli/go-gh/v2  
go get github.com/docker/docker/client  
go get github.com/docker/docker/api/types  
go get github.com/docker/docker/api/types/container  
go get github.com/docker/docker/pkg/stdcopy
```

### **Recommended File Structure**

```text
.  
├── go.mod  
├── go.sum  
└── cmd/gh-mcp/  
    ├── main.go         # Main entry point, CLI command setup  
    ├── auth.go         # Logic for retrieving auth details via go-gh  
    └── docker.go       # Docker client and container management logic
```

## **3. Core Component Implementation**

### **3.1. Authentication (auth.go)**

This component's sole responsibility is to fetch the active gh credentials.

**getAuthDetails() function:**

```go
package main

import (  
 "fmt"  
 "github.com/cli/go-gh/v2/pkg/auth"  
)

// authDetails holds the user's active GitHub host and token.  
type authDetails struct {  
 Host  string  
 Token string  
}

// getAuthDetails retrieves the current user's GitHub host and OAuth token  
// from the gh CLI's authentication context.  
func getAuthDetails() (*authDetails, error) {  
 host, _ := auth.DefaultHost()  
 token, _ := auth.TokenForHost(host)

 if token == "" {  
  return nil, fmt.Errorf("not logged in to GitHub. Please run `gh auth login`")  
 }

 return &authDetails{Host: host, Token: token}, nil  
}
```

### **3.2. Docker Management (docker.go)**

This component handles all interactions with the Docker daemon.

Docker Client Initialization:  
Use client.NewClientWithOpts to create a client that respects the user's local Docker environment configuration.  

```go
// In docker.go  
import "github.com/docker/docker/client"

func newDockerClient() (*client.Client, error) {  
 cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())  
 if err != nil {  
  return nil, fmt.Errorf("failed to create docker client: %w. Is the Docker daemon running?", err)  
 }  
 return cli, nil  
}
```

Image Pulling:  
The ensureImage function should check for the image locally and only pull if it's missing. Stream the pull output to os.Stdout to provide user feedback.  

```go
// In docker.go  
import (  
 "context"  
 "fmt"  
 "io"  
 "os"  
 "github.com/docker/docker/api/types"  
 "github.com/docker/docker/client"  
)

func ensureImage(ctx context.Context, cli *client.Client, imageName string) error {  
 fmt.Printf("Checking for image: %s...n", imageName)  
 _, _, err := cli.ImageInspectWithRaw(ctx, imageName)  
 if err == nil {  
  fmt.Println("✓ Image found locally.")  
  return nil // Image exists  
 }

 if !errdefs.IsNotFound(err) { // Using containerd errdefs  
  return fmt.Errorf("failed to inspect image: %w", err)  
 }

 fmt.Printf("Image not found. Pulling from %s...n", imageName)  
 reader, err := cli.ImagePull(ctx, imageName, types.ImagePullOptions{})  
 if err != nil {  
  return fmt.Errorf("failed to pull docker image '%s': %w", imageName, err)  
 }  
 defer reader.Close()  
   
 // Pipe the pull output to the user's terminal for progress  
 if _, err := io.Copy(os.Stdout, reader); err != nil {  
  return fmt.Errorf("failed to read image pull progress: %w", err)  
 }  
   
 fmt.Println("n✓ Image pulled successfully.")  
 return nil  
}
```

Container Execution and I/O Streaming:  
This is the most critical part. The container must be started, its streams attached, and I/O piped concurrently.  

```go
// In docker.go  
import (  
    "context"  
    "fmt"  
    "io"  
    "os"  
    "github.com/docker/docker/api/types/container"  
    "github.com/docker/docker/client"  
    "github.com/docker/docker/pkg/stdcopy"  
)

func runServerContainer(ctx context.Context, cli *client.Client, env []string, imageName string) error {  
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
  if err != nil && !errors.Is(err, io.EOF) {  
   fmt.Fprintf(os.Stderr, "Error reading from container: %vn", err)  
  }  
 }()  
    // Copy input from terminal to container  
 go func() {  
  _, err := io.Copy(hijackedResp.Conn, os.Stdin)  
  if err != nil && !errors.Is(err, io.EOF) {  
   fmt.Fprintf(os.Stderr, "Error writing to container: %vn", err)  
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
```

## **4. Main Orchestration (main.go)**

The main function ties everything together. It should be a clean, readable sequence of calls to the functions defined in auth.go and docker.go.

```go
// In main.go  
package main

import (  
    "context"  
    "fmt"  
    "log"  
    "os"  
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

func run(ctx context.Context) error {  
 // 1. Get Auth  
 auth, err := getAuthDetails()  
 if err != nil {  
  return err  
 }  
   
 // 2. Init Docker client  
 cli, err := newDockerClient()  
 if err != nil {  
  return err  
 }  
 defer cli.Close()

 // 3. Ensure image exists  
 if err := ensureImage(ctx, cli, mcpImage); err != nil {  
  return err  
 }

 // 4. Prepare environment  
 env := []string{  
  fmt.Sprintf("GITHUB_PERSONAL_ACCESS_TOKEN=%s", auth.Token),  
  fmt.Sprintf("GITHUB_HOST=%s", auth.Host),  
 }

 // 5. Run the container and stream I/O  
 if err := runServerContainer(ctx, cli, env, mcpImage); err != nil {  
  return err  
 }  
   
 fmt.Println("✓ Session finished.")  
 return nil  
}
```
