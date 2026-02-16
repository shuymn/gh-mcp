# gh-mcp

A GitHub CLI extension that seamlessly runs the [github-mcp-server](https://github.com/github/github-mcp-server) as a bundled binary using your existing `gh` authentication.

## Overview

`gh-mcp` eliminates the manual setup of GitHub Personal Access Tokens for MCP (Model Context Protocol) servers. It automatically retrieves your GitHub credentials from the `gh` CLI and launches a bundled `github-mcp-server` binary with proper authentication.

## Prerequisites

- [GitHub CLI (`gh`)](https://cli.github.com/) installed and authenticated (`gh auth login`)

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
2. It extracts and runs a bundled `github-mcp-server` release binary for your platform
3. Your credentials are securely passed to the server process
4. The temporary extracted binary is automatically removed when you exit

## Troubleshooting

### "Not logged in to GitHub"
Run `gh auth login` to authenticate with GitHub first.

### "Bundled binary checksum mismatch"
The bundled binary did not pass integrity verification. Reinstall or upgrade the extension.

### Server exits immediately
Check your MCP client configuration and ensure your GitHub token/host is valid.

## Security

- Your GitHub token is never stored by this extension
- Credentials are passed to the server process via environment variables
- The bundled archive is verified with SHA256 before execution
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
