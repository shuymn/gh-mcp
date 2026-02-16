# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a GitHub CLI extension that runs the github-mcp-server as a bundled binary using the user's existing `gh` authentication. It automates the process of retrieving GitHub credentials and launching the MCP server process with proper authentication.

## Common Development Commands

### Build

```bash
# Build the extension
go build -o gh-mcp .

# Clean build
go clean && go build -o gh-mcp .

# Cross-platform builds
GOOS=linux GOARCH=amd64 go build -o gh-mcp
GOOS=windows GOARCH=amd64 go build -o gh-mcp.exe
GOOS=darwin GOARCH=amd64 go build -o gh-mcp
```

### Development

```bash
# Format code
go fmt ./...

# Vet code for issues
go vet ./...

# Tidy dependencies
go mod tidy

# Install locally as Go binary
go install

# Install as GitHub CLI extension (from project root)
gh extension install .

# Test the extension directly
./gh-mcp
```

### Testing

```bash
# Run tests (when tests are added)
go test ./...

# Run tests with coverage
go test -cover ./...

# Run tests verbosely
go test -v ./...
```

## Architecture

The project consists of three main components:

1. **Authentication (`auth.go`)**: 
   - Retrieves GitHub credentials from `gh` CLI using `github.com/cli/go-gh/v2`
   - Returns host and token for the authenticated user
   - Uses dependency injection for testability

2. **Bundled Server Runtime (`server.go`)**:
   - Selects and verifies bundled `github-mcp-server` archives
   - Extracts the server binary for the current platform
   - Runs `github-mcp-server stdio` with GitHub credentials as environment variables
   - Manages bidirectional I/O streaming between terminal and server process
   - Handles graceful shutdown and cleanup

3. **Main Orchestration (`main.go`)**:
   - Sets up signal handling for Ctrl+C
   - Coordinates the authentication and bundled server flow
   - Provides user feedback with emoji status messages
   - Uses dependency injection for testing

4. **Release Automation**: The `.github/workflows/release.yml` workflow:
   - Triggers on version tags (e.g., `v1.0.0`)
   - Uses `cli/gh-extension-precompile@v2` for multi-platform builds
   - Generates attestations for security
   - Creates GitHub releases automatically

## Development Patterns

When extending this CLI:

1. **Dependency Injection**: All components use interfaces for external dependencies to enable unit testing without real API calls or real server process execution.

2. **Error Handling**: Errors bubble up with context, providing clear messages for users. Auth and server runtime errors include helpful suggestions.

3. **I/O Streaming**: Server process I/O is wired directly to stdio. Handle context cancellation and process termination carefully.

4. **Binary Naming**: The binary must be named `gh-mcp` to work as a GitHub CLI extension.

5. **Testing**: Unit tests mock all external dependencies. No integration tests that would use real GitHub credentials or real server binaries.

## Release Process

1. Tag the version: `git tag v1.0.0`
2. Push the tag: `git push origin v1.0.0`
3. GitHub Actions automatically builds and releases for all platforms

The release workflow handles all cross-platform compilation and artifact generation.
