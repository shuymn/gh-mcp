//go:build darwin && amd64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Darwin_x86_64.tar.gz"
	bundledMCPArchiveSHA256  = "5fef4459a7e67c64a8e7db3a858f992a6f03595225328f842b34f1b468a89c70"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Darwin_x86_64.tar.gz
var bundledMCPArchive []byte
