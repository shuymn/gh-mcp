# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a GitHub CLI extension that runs the github-mcp-server as a bundled binary using the user's existing `gh` authentication. It automates the process of retrieving GitHub credentials and launching the MCP server process with proper authentication.

## Common Development Commands

### Build

```bash
# Stage the locked server archive for this platform and build
task build
# Equivalent to:
go run ./tools/lock stage && go build -o gh-mcp .

# Build every locked platform into dist/ (what releases run)
go run ./tools/lock dist -out dist
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
go test ./...
go test -race -shuffle=on -count=10 ./...   # task test
```

### Server lock

```bash
go run ./tools/lock sync -version vX.Y.Z   # pin an upstream release (verifies attestations)
go run ./tools/lock verify                 # check server.lock.json against upstream
go run ./tools/release next -base-ref origin/main -write   # VERSION for a lock change
```

## Architecture

gh-mcp is a launcher that injects `gh` credentials into the upstream `github-mcp-server`.
It knows only the `stdio` subcommand and the `GITHUB_PERSONAL_ACCESS_TOKEN`/`GITHUB_HOST`
variables; user arguments and `GITHUB_*` variables pass through untouched.

1. **`internal/identity`**: gh's default host and token via `github.com/cli/go-gh/v2`.
2. **`internal/artifact`**: parses `server.lock.json`, extracts archives, and keeps verified
   executables in a content-addressed cache (`<user cache>/gh-mcp/servers/<sha256>/`).
   Installs go through a temp file and a hard link that never replaces a valid executable
   (Windows cannot replace open files). Cache hits touch a `.last-used` marker; a fresh install
   prunes other digests unused for 7 days (`StaleAfter`), best effort. `main.go` repeats
   Ensure + exec once when the executable vanished (`fs.ErrNotExist`), and nothing else.
3. **`internal/launch`**: builds args (`stdio` + user args) and env (base allowlist +
   `GITHUB_*`, credentials withheld/overridden), then `syscall.Exec` on Unix or spawn-and-wait
   on Windows.
4. **`main.go`**: embeds `server.lock.json` and `payload/` (release builds stage
   `payload/server.archive` per platform) and wires the three packages through the
   `launcher` struct, whose function fields are the test seams.
5. **Release automation**:
   - `server.lock.json` is the single source for the upstream version, supported platforms,
     and digests. Renovate bumps its version; `.github/workflows/sync-server-lock.yml` runs
     trusted base tools to record attested digests and `VERSION`, then creates one
     app-signed commit through the API (the `main` ruleset requires signed commits)
   - Every upstream release gets one gh-mcp release: Renovate proposes each version on its own
     branch, and `tools/release validate` rejects a lock update that skips a published release
   - Renovate auto-merges minor/patch updates after required CI; majors need review
   - CI (`ci.yml`) runs `tools/release validate` and `tools/lock verify`, then calls
     `release.yml` on `main`, which uses `tools/release plan`, tags, builds with
     `cli/gh-extension-precompile@v2` + `scripts/build-dist.sh`, attests, and publishes
   - `plan` skips published versions; a version released after a newer one is published with
     `--latest=false`

## Development Patterns

1. **Seams**: inject external effects (gh auth, cache root, exec) as function values or
   struct fields. Do not add interfaces only for tests.

2. **Error Handling**: errors bubble up with context; `main` prints one `gh-mcp: <err>` line.
   Successful runs print nothing (`GH_MCP_DEBUG=1` adds diagnostics).

3. **Upstream options**: never mirror upstream flags or env vars in gh-mcp; pass them through.

4. **Binary Naming**: The binary must be named `gh-mcp` to work as a GitHub CLI extension.

5. **Testing**: build archive fixtures in tests. No tests use real credentials, network,
   or real server binaries.

## Release Process

Normal upstream releases require no manual tag or release PR. Renovate opens the lock update
PR, `Sync server lock` completes it, GitHub auto-merges minor/patch updates after CI, and
successful `main` CI triggers the release.

For a project-only release, update `VERSION` in a normal PR. Recover a failed release by
rerunning the failed `Release` job in the same CI run so the tested commit remains fixed.
