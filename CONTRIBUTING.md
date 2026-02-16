# Contributing to gh-mcp

Thank you for your interest in contributing to gh-mcp!

## Development Setup

### Prerequisites

- Go 1.21 or later
- GitHub CLI (`gh`)
- Task (optional, for using Taskfile commands)

### Building from Source

```bash
# Clone the repository
git clone https://github.com/shuymn/gh-mcp.git
cd gh-mcp

# Build the extension
./scripts/prepare-bundled-mcp-server.sh
go build -o gh-mcp .

# Or use task
task build

# Install locally as a gh extension
gh extension install .
```

### Updating Bundled github-mcp-server

When bumping the bundled MCP server version, refresh pinned metadata from the target release:

```bash
./scripts/update-bundled-mcp-server.sh v0.30.3
```

This updates `mcp_version.go` and SHA256 constants in `bundle_*.go`.
Release archives under `bundled/` are downloaded on demand and are gitignored.
`scripts/prepare-bundled-mcp-server.sh` and `scripts/update-bundled-mcp-server.sh` use authenticated `gh` requests (including release attestation verification),
so run `gh auth login` locally or set `GH_TOKEN` (or `GITHUB_TOKEN`) in CI.

## Development Workflow

### Running Tests

```bash
# Run all tests with race detection
task test

# Run with verbose output
task test:verbose

# Run with coverage
task test:coverage

# Or use go directly
./scripts/prepare-bundled-mcp-server.sh
go test -race ./...
```

### Linting

```bash
# Run linter
task lint

# Format code
task fmt
```

### Checking Everything

```bash
# Run all checks (test, lint, build)
task check
```

## Project Structure

```
gh-mcp/
├── main.go           # Entry point and orchestration
├── auth.go           # GitHub authentication via gh CLI
├── auth_test.go      # Unit tests for auth
├── server.go         # Bundled github-mcp-server execution
├── server_test.go    # Unit tests for server helpers
├── bundled/          # Downloaded github-mcp-server release archives (gitignored)
├── main_test.go      # Unit tests for main orchestration
├── .github/
│   └── workflows/
│       ├── ci.yml     # CI pipeline
│       └── release.yml # Release automation
├── .golangci.yaml    # Linter configuration
├── .octocov.yml      # Coverage reporting
├── Taskfile.yml      # Build automation
├── go.mod            # Go module definition
├── go.sum            # Dependency checksums
└── README.md         # User documentation
```

## Architecture Overview

The project consists of three main components:

1. **Authentication (`auth.go`)**: 
   - Retrieves GitHub credentials from `gh` CLI using `github.com/cli/go-gh/v2`
   - Returns host and token for the authenticated user
   - Uses dependency injection for testability

2. **Bundled Server Runtime (`server.go`)**:
   - Selects the bundled `github-mcp-server` archive for the current platform
   - Verifies archive integrity with pinned SHA256
   - Extracts and executes `github-mcp-server stdio`
   - Streams stdin/stdout/stderr directly to/from the server process
   - Handles graceful shutdown and cleanup of temporary extracted files

3. **Main Orchestration (`main.go`)**:
   - Sets up signal handling for Ctrl+C
   - Coordinates the authentication and bundled-server execution flow
   - Provides user feedback with emoji status messages
   - Uses dependency injection for testing

## Testing Guidelines

- All components use interfaces for external dependencies to enable unit testing
- Mock all external dependencies (GitHub auth and server runner)
- Use table-driven tests where appropriate
- Ensure tests are deterministic and fast

## Release Process

Releases are automated via GitHub Actions:

1. Tag the version: `git tag v1.0.0`
2. Push the tag: `git push origin v1.0.0`
3. GitHub Actions automatically builds and releases for all platforms

The release workflow handles all cross-platform compilation and artifact generation.

## Code Style

- Follow standard Go conventions
- Use `golangci-lint` for consistent formatting
- Keep functions focused and testable
- Use meaningful variable and function names
- Add comments for complex logic

## Submitting Changes

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes
4. Add tests for new functionality
5. Ensure all tests pass (`task check`)
6. Commit your changes with clear messages
7. Push to your fork
8. Open a Pull Request

## Questions?

Feel free to open an issue for any questions or discussions!
