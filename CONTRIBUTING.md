# Contributing to gh-mcp

Thank you for your interest in contributing to gh-mcp!

## Development Setup

### Prerequisites

- Go 1.21 or later
- GitHub CLI (`gh`)
- Bash 4.1 or later (`brew install bash` on macOS)
- ShellCheck
- Task (optional, for using Taskfile commands)

### Building from Source

```bash
# Clone the repository
git clone https://github.com/shuymn/gh-mcp.git
cd gh-mcp

# Build the extension
go run ./scripts/prepare
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
`go run ./scripts/prepare` and `scripts/update-bundled-mcp-server.sh` use authenticated `gh` requests (including release attestation verification),
so run `gh auth login` locally or set `GH_TOKEN` (or `GITHUB_TOKEN`) in CI.
The runtime binary does not perform attestation verification; instead it verifies downloaded archives against these pinned SHA256 constants.

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
go run ./scripts/prepare
go test -race ./...
```

### Linting

```bash
# Run linter
task lint

# Check shell scripts with shellcheck and shfmt
task shell:check

# Run release workflow script regression tests
task shell:test

# Format Go and shell code
task fmt
```

### Checking Everything

```bash
# Run all checks (shell, test, lint, build)
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
│       ├── bump.yml                   # Upstream release preparation
│       ├── ci.yml                     # CI pipeline
│       ├── merge-upstream-release.yml # Trusted post-CI merge
│       └── release.yml                # Release publishing
├── scripts/
│   └── workflows/     # Statically checked release workflow logic
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

Stable `github-mcp-server` updates are released through an automated pipeline:

1. After a one-day stabilization window, Renovate opens one update PR for
   `mcp_version.go`.
2. `Prepare upstream release` verifies the upstream attestations and checksums, updates
   `VERSION` and the pinned archive hashes in one commit, and pushes it to the PR.
3. After required CI passes, the trusted post-CI workflow checks the current PR identity,
   exact base/head, and canonical metadata again before merging patch and minor updates.
   Major updates remain open for compatibility review.
4. After the merge commit passes CI on `main`, `Release` creates the version tag, builds
   all extension artifacts, generates build-provenance attestations, and publishes the
   GitHub release.

Release jobs are serialized. If CI completion order is inverted, an older candidate
verifies the newer immutable release and exits instead of publishing versions backward.
Workflow YAML delegates non-trivial shell logic to `scripts/workflows/`; `task check`
validates those scripts with shellcheck and shfmt.

For a project-only release, bump `VERSION` in a normal PR. The same CI-gated release path
runs after merge. If the `Release` job fails, rerun that failed CI job; it resumes an
existing draft for the same tested commit and never rewrites a published release.

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
