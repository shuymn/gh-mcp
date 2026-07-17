//go:build windows && 386

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Windows_i386.zip"
	bundledMCPArchiveSHA256  = "83db1eb8f783b86e8ed3124e60f5560064915262eb48f797095666089d3e123e"
	bundledMCPExecutableName = "github-mcp-server.exe"
)

//go:embed bundled/github-mcp-server_Windows_i386.zip
var bundledMCPArchive []byte
