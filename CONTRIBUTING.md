# Contributing to gh-mcp

Thank you for your interest in contributing to gh-mcp!

## Development Setup

### Prerequisites

- Go 1.21 or later
- Docker (for testing)
- Make (optional, for using Makefile commands)

### Building from Source

```bash
# Clone the repository
git clone https://github.com/shuymn/gh-mcp.git
cd gh-mcp

# Build the extension
go build -o gh-mcp .

# Or use make
make build

# Install locally as a gh extension
gh extension install .
```

## Development Workflow

### Running Tests

```bash
# Run all tests with race detection
make test

# Run with verbose output
make test-verbose

# Run with coverage
make test-coverage

# Or use go directly
go test -race ./...
```

### Linting

```bash
# Run linter
make lint

# Format code
make fmt
```

### Checking Everything

```bash
# Run all checks (test, lint, build)
make check
```

## Project Structure

```
gh-mcp/
├── main.go           # Entry point and orchestration
├── auth.go           # GitHub authentication via gh CLI
├── auth_test.go      # Unit tests for auth
├── docker.go         # Docker container management
├── docker_test.go    # Unit tests for docker
├── main_test.go      # Unit tests for main orchestration
├── .github/
│   └── workflows/
│       ├── ci.yml     # CI pipeline
│       └── release.yml # Release automation
├── .golangci.yaml    # Linter configuration
├── .octocov.yml      # Coverage reporting
├── Makefile          # Build automation
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

2. **Docker Management (`docker.go`)**:
   - Creates Docker client with environment configuration
   - Checks for and pulls the MCP server image if needed
   - Creates and runs container with GitHub credentials as environment variables
   - Manages bidirectional I/O streaming between terminal and container
   - Handles graceful shutdown and cleanup

3. **Main Orchestration (`main.go`)**:
   - Sets up signal handling for Ctrl+C
   - Coordinates the authentication and Docker flow
   - Provides user feedback with emoji status messages
   - Uses dependency injection for testing

## Testing Guidelines

- All components use interfaces for external dependencies to enable unit testing
- Mock all external dependencies (GitHub API, Docker daemon)
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
5. Ensure all tests pass (`make check`)
6. Commit your changes with clear messages
7. Push to your fork
8. Open a Pull Request

## Questions?

Feel free to open an issue for any questions or discussions!