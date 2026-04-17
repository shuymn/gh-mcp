//go:build windows && arm64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Windows_arm64.zip"
	bundledMCPArchiveSHA256  = "8cf6c9d92510d91a3098fe3a5c51d6691b0ca579d41175d20ac53cdf7b830634"
	bundledMCPExecutableName = "github-mcp-server.exe"
)

//go:embed bundled/github-mcp-server_Windows_arm64.zip
var bundledMCPArchive []byte
