//go:build darwin && amd64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Darwin_x86_64.tar.gz"
	bundledMCPArchiveSHA256  = "e7cc6b5d16bcb86b01cae2ba32fa7525266a10c96db8d64df63a172ebb7b8f95"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Darwin_x86_64.tar.gz
var bundledMCPArchive []byte
