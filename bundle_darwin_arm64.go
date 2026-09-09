//go:build darwin && arm64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Darwin_arm64.tar.gz"
	bundledMCPArchiveSHA256  = "efb582c287e0dcc8897625d729a77f98c3f00a7af94b7ab6845acfe7f3274b28"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Darwin_arm64.tar.gz
var bundledMCPArchive []byte
