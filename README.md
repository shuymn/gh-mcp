# gh-mcp

A GitHub CLI extension that seamlessly runs the [github-mcp-server](https://github.com/github/github-mcp-server) as a bundled binary using your existing `gh` authentication.

## Overview

`gh-mcp` eliminates the manual setup of GitHub Personal Access Tokens for MCP (Model Context Protocol) servers. It automatically retrieves your GitHub credentials from the `gh` CLI and launches a bundled `github-mcp-server` binary with proper authentication.

## Prerequisites

- [GitHub CLI (`gh`)](https://cli.github.com/) installed and authenticated (`gh auth login`)

## Platform Support

`gh-mcp` is released for the platforms where upstream publishes `github-mcp-server` archives:

- `darwin/amd64`
- `darwin/arm64`
- `linux/386`
- `linux/amd64`
- `linux/arm64`
- `windows/386`
- `windows/amd64`
- `windows/arm64`

## Installation

```bash
gh extension install shuymn/gh-mcp
```

### Updating

To update the extension to the latest version:

```bash
gh extension upgrade mcp
```

## Usage

### MCP Configuration

Add this to your MCP client configuration:

```json
{
  "github": {
    "command": "gh",
    "args": ["mcp"]
  }
}
```

With server options:

```json
{
  "github": {
    "command": "gh",
    "args": ["mcp", "--toolsets=repos,issues,pull_requests", "--read-only"]
  }
}
```

### Using with Claude Code

To add this as an MCP server to Claude Code:

```bash
claude mcp add-json github '{"command":"gh","args":["mcp"]}'
```

With server options:

```bash
claude mcp add-json github '{"command":"gh","args":["mcp","--toolsets=repos,issues","--read-only"]}'
```

### Running Directly

You can also run the server directly:

```bash
gh mcp
```

This will:
1. Retrieve your GitHub credentials from `gh` CLI
2. Use the verified `github-mcp-server` from the cache, extracting the bundled copy on first run
3. Replace itself with `github-mcp-server stdio`, so stdio, signals, and the exit code belong to the server

`gh-mcp` prints nothing unless something fails. Set `GH_MCP_DEBUG=1` to print the host, server version, and executable path.

## Configuration

`gh-mcp` does not define its own server options. Everything you pass reaches `github-mcp-server stdio` unchanged, so every upstream option works, including ones added after this release.

### Arguments

Arguments after `gh mcp` are appended to `github-mcp-server stdio`:

```bash
gh mcp --toolsets=repos,issues --read-only
gh mcp --exclude-tools=delete_file --lockdown-mode
```

Run `gh mcp --help` to see the options of the bundled server.

### Environment Variables

Every `GITHUB_*` variable is forwarded, so the upstream environment equivalents work too:

```bash
GITHUB_TOOLSETS="repos,issues,pull_requests" gh mcp
GITHUB_READ_ONLY=1 gh mcp
GITHUB_EXCLUDE_TOOLS=delete_file gh mcp
```

The server lists every tool in the enabled toolsets, even ones your `gh` token lacks scopes for. Use `--toolsets`, `--tools`, or `--exclude-tools` to narrow the list.

`gh-mcp` always sets `GITHUB_PERSONAL_ACCESS_TOKEN` and `GITHUB_HOST` from `gh` and never forwards `GITHUB_TOKEN` or `GITHUB_ENTERPRISE_TOKEN`.

### Process Environment Trust Model

The server receives a minimal environment:

- `GITHUB_PERSONAL_ACCESS_TOKEN` and `GITHUB_HOST`, set by `gh-mcp`
- Other `GITHUB_*` variables from the parent process
- A fixed allowlist: `PATH`, home and temp-dir variables, proxy and certificate variables

Proxy variables are intentionally forwarded to support enterprise networks. If you run `gh mcp` from an untrusted wrapper process, clear proxy/certificate variables before launch.

## How It Works

1. The extension retrieves your GitHub credentials from your existing `gh` CLI authentication
2. `server.lock.json` pins the upstream version and the SHA256 of the server executable for each platform
3. The executable is cached at `<user cache dir>/gh-mcp/servers/<sha256>/`. Each launch re-hashes it; a missing or modified file is replaced from the archive bundled in `gh-mcp`. After installing a new version, `gh-mcp` removes cached versions that have not been used for 7 days
4. `gh-mcp` replaces itself with the server (on Windows it runs the server and waits)

## Troubleshooting

### "not logged in to GitHub"
Run `gh auth login` to authenticate with GitHub first.

### "gh has no default host"
No default GitHub host is configured in `gh`. Run `gh auth status` and authenticate/select a default account.

### "no bundled github-mcp-server for platform"
Your OS/architecture is not supported. Check [Platform Support](#platform-support).

### "this build does not bundle github-mcp-server"
You built `gh-mcp` with plain `go build`. Build with `task build`, or install a release with `gh extension install shuymn/gh-mcp`.

### "github-mcp-server digest mismatch"
The bundled archive does not match `server.lock.json`. Reinstall or upgrade the extension.

### "cache directory is insecure"
A cache directory is a symbolic link, is owned by another user, or is accessible to other users. Remove `<user cache dir>/gh-mcp` and run `gh mcp` again.

### "invalid server environment value"
The token or host from `gh` contains a line break or NUL byte. Check `gh auth status`.

### Upgrading from v3
- `LOG_LEVEL` no longer has an effect. Use `GH_MCP_DEBUG=1`.
- Arguments after `gh mcp` are now passed to the server instead of being ignored.
- All `GITHUB_*` variables are forwarded. `GITHUB_DYNAMIC_TOOLSETS` was removed upstream and has no effect.

## Security

- Your GitHub token is never stored by this extension
- Credentials are passed to the server process via environment variables
- Supply-chain integrity: CI verifies `server.lock.json` against the release attestation of the upstream checksums file, and release binaries carry build-provenance attestations (`gh attestation verify <binary> --repo shuymn/gh-mcp`)
- Runtime integrity: the cached executable is checked against the locked SHA256 on every launch
- Threat model: the cache must be a private directory owned by you. A process running as your user is out of scope, because it can read the `gh` token directly

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

For development information, see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Related Projects

- [github-mcp-server](https://github.com/github/github-mcp-server) - The MCP server this extension runs
- [GitHub CLI](https://github.com/cli/cli) - The official GitHub command line tool
- [go-gh](https://github.com/cli/go-gh) - The Go library for GitHub CLI extensions
