//go:build windows && amd64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Windows_x86_64.zip"
	bundledMCPArchiveSHA256  = "33e2ac4c49f2cd5c58b5ab6bc2dd724fa92684c8256b0306c22ed6c3c04b7f49"
	bundledMCPExecutableName = "github-mcp-server.exe"
)

//go:embed bundled/github-mcp-server_Windows_x86_64.zip
var bundledMCPArchive []byte
