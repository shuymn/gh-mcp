//go:build windows && arm64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Windows_arm64.zip"
	bundledMCPArchiveSHA256  = "258ad2694bbeb9dc8b8c63078d861b729095e5fda726500e4197eacb698a023c"
	bundledMCPExecutableName = "github-mcp-server.exe"
)

//go:embed bundled/github-mcp-server_Windows_arm64.zip
var bundledMCPArchive []byte
