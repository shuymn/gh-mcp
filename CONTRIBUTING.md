# Contributing to gh-mcp

Thank you for your interest in contributing to gh-mcp!

## Development Setup

### Prerequisites

- Go (version in `go.mod`)
- GitHub CLI (`gh`), logged in with `gh auth login`
- Task (optional, for using Taskfile commands)

### Building from Source

```bash
# Clone the repository
git clone https://github.com/shuymn/gh-mcp.git
cd gh-mcp

# Stage the locked github-mcp-server archive for this platform, then build
go run ./tools/lock stage
go build -o gh-mcp .

# Or use task
task build

# Install locally as a gh extension
gh extension install .
```

A plain `go build` without staging also succeeds. That binary embeds no server
archive, so it only runs when the cache already holds the locked server, and
otherwise exits with a message asking you to build with `task build`.

### Updating github-mcp-server

`server.lock.json` is the single source for the bundled server: the upstream version, the
supported platforms, and for each platform the archive and executable SHA256.

```bash
# Pin a release: downloads every archive, verifies the attested checksums file,
# and records the archive and executable digests
go run ./tools/lock sync -version v1.15.0

# Check the lock against the attested upstream release (CI runs this)
go run ./tools/lock verify
```

`stage` and `dist` always read the repository's `server.lock.json`, the file embedded in the
launcher. Only `sync` and `verify` accept `-lock` to work on another lock file.

Downloads are cached under `.cache/upstream/` (gitignored). The tools call `gh`, so run
`gh auth login` locally or set `GH_TOKEN` (or `GITHUB_TOKEN`) in CI.

When the upstream version changes, `VERSION` must change with it (see Release Process).
`go run ./tools/release next -base-ref origin/main -write` writes the expected value.

## Development Workflow

```bash
task test           # tests with race detection and shuffle
task test:coverage  # coverage report
task lint           # golangci-lint
task fmt            # format Go code
task check          # lint, build, test
```

Tests never need the server archive, network access, or real credentials.

## Project Structure

```
gh-mcp/
├── main.go                 # Wires identity, artifact, and launch together
├── server.lock.json        # Pinned upstream version and digests per platform
├── payload/                # Release builds stage server.archive here (gitignored)
├── internal/
│   ├── identity/           # gh host and token
│   ├── artifact/           # Lock parsing, archive extraction, content-addressed cache
│   └── launch/             # Server arguments, environment, exec (Unix) / spawn (Windows)
├── tools/
│   ├── lock/               # sync, verify, stage, dist
│   └── release/            # next, validate, plan
├── scripts/build-dist.sh   # Build hook for cli/gh-extension-precompile
└── .github/workflows/
    ├── ci.yml              # Lint, test, build, and the Release call on main
    ├── release.yml         # Tag, build, attest, publish
    └── sync-server-lock.yml # Completes Renovate's server.lock.json updates
```

## Architecture Overview

gh-mcp is a launcher: it injects gh's credentials into the upstream server and gets out of
the way. It knows only the `stdio` subcommand and the `GITHUB_PERSONAL_ACCESS_TOKEN` and
`GITHUB_HOST` variables; every other option passes through untouched.

1. **Identity** resolves gh's default host and token with `github.com/cli/go-gh/v2`.
2. **Artifact** looks up the locked executable digest for the platform. The executable lives
   at `<user cache>/gh-mcp/servers/<sha256>/`. On a cache hit gh-mcp re-hashes it and touches
   a `.last-used` marker beside it (never the executable: on Windows that would block
   concurrent launches). On a miss it extracts the embedded archive to a temporary file,
   checks the digest, and hard-links it into place, so a valid executable is never replaced
   while another launcher may be reading or running it. After a fresh install only,
   it removes other digests unused for 7 days, best effort: anything it cannot remove stays,
   and cleanup never fails the launch.
   This lowers the chance of deleting an executable another launcher is about to run but does
   not rule it out. If the executable vanishes between preparation and launch, gh-mcp prepares
   it once more; a hash mismatch, a permission error, or a server that exits with an error is
   never retried. Repeated concurrent pruning can still make that retry fail, and a build
   without an embedded archive cannot reinstall at all.
3. **Launch** appends the user's arguments to `stdio`, builds the environment from a base
   allowlist plus every `GITHUB_*` variable, and replaces the process with the server
   (`syscall.Exec`). Windows has no exec, so gh-mcp waits for the child and returns its exit code.

Threat model: archives are trusted because CI verifies them against upstream's attested
checksums. Another user cannot tamper with the cache, which must be a private, non-symlink
directory owned by the current user. Existing directories with broader permissions are
rejected, not chmodded, because their contents may already have been altered. A process
running as the same user is out of scope: it
can already read gh's token.

## Testing Guidelines

- Inject external effects (gh auth, cache location, exec) as function values or fields
- Build archive fixtures in tests; do not use real server binaries or credentials
- Use table-driven tests where appropriate
- Ensure tests are deterministic and fast

## Release Process

Every stable `github-mcp-server` release gets exactly one gh-mcp release. Upstream updates:

1. After a one-day stabilization window, Renovate opens one PR per upstream release, each
   capped at the next patch, the next minor's `.0`, or the next major's `.0`. Minor and patch
   PRs have auto-merge enabled.
2. `Sync server lock` runs trusted tools from the base commit. It refuses PRs that touch
   anything but `server.lock.json` and `VERSION`, records the attested digests, computes
   `VERSION`, and creates one commit through the API with the app token. GitHub signs it,
   as the `main` ruleset requires, and CI runs again.
3. CI verifies the lock against the attested release and checks two rules. `VERSION` must
   follow the bump rule: an upstream major, minor, or patch update bumps the same component
   of gh-mcp. The new upstream version must also be the next published stable release after
   the base, so a later PR stays blocked until the earlier one merges. GitHub merges once the
   required checks pass. Major updates wait for review.
4. After the merge commit passes CI on `main`, `Release` tags the commit, builds every locked
   platform, generates build-provenance attestations, and publishes the release.

Release jobs are serialized. `release plan` skips a version that is already published. If CI
finished out of order and a newer version is already out, the older one is still published
but does not become the latest release. If the `Release` job fails, rerun it; the build
uploads into the existing draft for the same tag.

For a project-only release, raise `VERSION` in a normal PR.

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
