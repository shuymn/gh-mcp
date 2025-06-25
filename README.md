# gh-mcp

A GitHub CLI extension that seamlessly runs the [github-mcp-server](https://github.com/github/github-mcp-server) in a Docker container using your existing `gh` authentication.

## Overview

`gh-mcp` eliminates the manual setup of GitHub Personal Access Tokens for MCP (Model Context Protocol) servers. It automatically retrieves your GitHub credentials from the `gh` CLI and launches the MCP server in a Docker container with proper authentication.

## Prerequisites

- [GitHub CLI (`gh`)](https://cli.github.com/) installed and authenticated (`gh auth login`)
- [Docker](https://www.docker.com/) installed and running
- Go 1.24.4 or later (only needed for development)

## Installation

```bash
gh extension install shuymn/gh-mcp
```

## Usage

Simply run:

```bash
gh mcp
```

The extension will:
1. 🔐 Retrieve your GitHub credentials from `gh` CLI
2. 🐳 Connect to Docker
3. 📦 Pull the MCP server image (if not already present)
4. 🚀 Start the MCP server with your credentials
5. Stream I/O between your terminal and the container

Press `Ctrl+C` to gracefully shut down the server.

## Configuration

The extension passes through several environment variables to configure the MCP server:

### Toolsets
Control which GitHub API toolsets are available:

```bash
# Enable specific toolsets
GITHUB_TOOLSETS="repos,issues,pull_requests" gh mcp

# Enable all toolsets
GITHUB_TOOLSETS="all" gh mcp
```

### Dynamic Toolset Discovery
Enable dynamic toolset discovery (beta feature):

```bash
GITHUB_DYNAMIC_TOOLSETS=1 gh mcp
```

### Read-Only Mode
Run the server in read-only mode to prevent modifications:

```bash
GITHUB_READ_ONLY=1 gh mcp
```

### Combining Options
You can combine multiple options:

```bash
GITHUB_READ_ONLY=1 GITHUB_TOOLSETS="repos,issues" gh mcp
```

## How It Works

1. **Authentication**: The extension uses the `github.com/cli/go-gh/v2` library to access your existing `gh` CLI authentication, supporting both github.com and GitHub Enterprise instances.

2. **Container Management**: It uses the Docker SDK to:
   - Check if the `ghcr.io/github/github-mcp-server:latest` image exists locally
   - Pull the image if needed (with progress feedback)
   - Create and run a container with your GitHub token and host as environment variables
   - Set up bidirectional I/O streaming between your terminal and the container

3. **Cleanup**: The container is automatically removed when you exit (using Docker's `--rm` flag equivalent).

## Development

### Building from Source

```bash
# Clone the repository
git clone https://github.com/shuymn/gh-mcp.git
cd gh-mcp

# Build the extension
go build -o gh-mcp ./cmd/gh-mcp

# Install locally as a gh extension
gh extension install .
```

### Running Tests

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run with coverage
go test -cover ./...
```

### Project Structure

```
gh-mcp/
├── cmd/gh-mcp/
│   ├── main.go       # Entry point and orchestration
│   ├── auth.go       # GitHub authentication via gh CLI
│   ├── auth_test.go  # Unit tests for auth
│   ├── docker.go     # Docker container management
│   ├── docker_test.go # Unit tests for docker
│   └── main_test.go  # Unit tests for main orchestration
├── go.mod           # Go module definition
├── go.sum           # Dependency checksums
└── README.md        # This file
```

## Troubleshooting

### "Not logged in to GitHub"
Run `gh auth login` to authenticate with GitHub first.

### "Docker daemon is not running"
Make sure Docker Desktop (or Docker service) is running on your system.

### "Failed to pull image"
- Check your internet connection
- Verify you have access to `ghcr.io` (GitHub Container Registry)
- The first pull may take a few minutes depending on your connection

### Container exits immediately
Check the container logs or ensure the MCP server image is working correctly.

## Security

- Your GitHub token is never stored by this extension
- Credentials are passed to the container via environment variables
- The container runs with `--rm` to ensure cleanup
- No data persists after the session ends

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Related Projects

- [github-mcp-server](https://github.com/github/github-mcp-server) - The MCP server this extension runs
- [GitHub CLI](https://github.com/cli/cli) - The official GitHub command line tool
- [go-gh](https://github.com/cli/go-gh) - The Go library for GitHub CLI extensions