//go:build darwin && amd64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Darwin_x86_64.tar.gz"
	bundledMCPArchiveSHA256  = "567a82e02d4b577ce1bd20af3e1ba477f2b69c8c51324983fe28e6a5017aa6e4"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Darwin_x86_64.tar.gz
var bundledMCPArchive []byte
