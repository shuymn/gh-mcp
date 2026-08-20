//go:build linux && arm64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Linux_arm64.tar.gz"
	bundledMCPArchiveSHA256  = "b37e0e80f863fd9e3ebb50be773fa0c3f73ddb9d9e735a48d5ad6dc2bf5795ac"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Linux_arm64.tar.gz
var bundledMCPArchive []byte
