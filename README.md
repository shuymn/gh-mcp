# gh-mcp

A GitHub CLI extension that seamlessly runs the [github-mcp-server](https://github.com/github/github-mcp-server) as a bundled binary using your existing `gh` authentication.

## Overview

`gh-mcp` eliminates the manual setup of GitHub Personal Access Tokens for MCP (Model Context Protocol) servers. It automatically retrieves your GitHub credentials from the `gh` CLI and launches a bundled `github-mcp-server` binary with proper authentication.

## Prerequisites

- [GitHub CLI (`gh`)](https://cli.github.com/) installed and authenticated (`gh auth login`)

## Platform Support

`gh-mcp` runtime support is limited to platforms where bundled `github-mcp-server` archives are available:

- `darwin/amd64`
- `darwin/arm64`
- `linux/386`
- `linux/amd64`
- `linux/arm64`
- `windows/386`
- `windows/amd64`
- `windows/arm64`

Release assets may still include additional targets produced by `cli/gh-extension-precompile` (for example `freebsd-*` and `linux/arm`), but those targets are not supported by `gh-mcp` runtime because no bundled `github-mcp-server` binary is available for them.

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

With environment variables:

```json
{
  "github": {
    "command": "gh",
    "args": ["mcp"],
    "env": {
      "GITHUB_TOOLSETS": "repos,issues,pull_requests",
      "GITHUB_READ_ONLY": "1"
    }
  }
}
```

### Using with Claude Code

To add this as an MCP server to Claude Code:

```bash
claude mcp add-json github '{"command":"gh","args":["mcp"]}'
```

With environment variables:

```bash
claude mcp add-json github '{"command":"gh","args":["mcp"],"env":{"GITHUB_TOOLSETS":"repos,issues","GITHUB_READ_ONLY":"1"}}'
```

### Running Directly

You can also run the server directly:

```bash
gh mcp
```

This will:
1. 🔐 Retrieve your GitHub credentials from `gh` CLI
2. 📦 Extract and verify the bundled MCP server binary
3. 🚀 Start the MCP server with your credentials
4. Stream I/O between your terminal and the server process

Press `Ctrl+C` to gracefully shut down the server.

## Configuration

The extension passes through several environment variables to configure the MCP server:

### Process Environment Trust Model

`gh-mcp` starts `github-mcp-server` with a minimal child-process environment:

- Required `GITHUB_*` variables are set by `gh-mcp`
- Only a fixed allowlist from the parent process is forwarded (`PATH`, temp-dir vars, proxy/cert vars)

Proxy variables are intentionally forwarded to support enterprise networks. If you run `gh mcp` from an untrusted wrapper process, clear proxy/certificate variables before launch.

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

1. The extension retrieves your GitHub credentials from your existing `gh` CLI authentication
2. It validates the bundled archive against a pinned SHA256 and extracts the `github-mcp-server` binary for your platform
3. Your credentials are securely passed to the server process
4. The temporary extracted binary is automatically removed when you exit

## Troubleshooting

### "Not logged in to GitHub"
Run `gh auth login` to authenticate with GitHub first.

### "failed to get default host"
No default GitHub host is configured in `gh`. Run `gh auth status` and authenticate/select a default account.

### "no bundled github-mcp-server for platform"
Your OS/architecture is not supported by bundled runtime assets. Check [Platform Support](#platform-support) and use a supported target.

### "Bundled binary checksum mismatch"
The bundled binary did not pass integrity verification. Reinstall or upgrade the extension.

### "bundled temp parent directory is insecure"
The cache parent directory for extracted binaries failed ownership/permission checks. On Unix-like systems, ensure your user owns the cache path and that permissions are private (for example, `0700`).

### "server exited with non-zero status: <code>"
The bundled `github-mcp-server` started but returned an error. Check MCP client configuration and `GITHUB_*` environment values.

### "invalid server environment value"
One of the forwarded environment values contains a line break or NUL byte. Remove control characters from `GITHUB_*` values before running `gh mcp`.

## Security

- Your GitHub token is never stored by this extension
- Credentials are passed to the server process via environment variables
- Runtime integrity: bundled archives are verified with embedded SHA256 before execution
- Supply-chain integrity: release update scripts verify GitHub release attestations before pinning SHA256 values in source
- Trust model note: runtime does not re-run attestation checks; it relies on pinned hashes generated during release asset preparation
- No data persists after the session ends

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

For development information, see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Related Projects

- [github-mcp-server](https://github.com/github/github-mcp-server) - The MCP server this extension runs
- [GitHub CLI](https://github.com/cli/cli) - The official GitHub command line tool
- [go-gh](https://github.com/cli/go-gh) - The Go library for GitHub CLI extensions
