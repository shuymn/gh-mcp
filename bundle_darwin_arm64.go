//go:build darwin && arm64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Darwin_arm64.tar.gz"
	bundledMCPArchiveSHA256  = "d813a8fa7d5983742f5f9c3729f22d149c09499d49769e8e770e493b590fbf56"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Darwin_arm64.tar.gz
var bundledMCPArchive []byte
