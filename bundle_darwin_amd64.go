//go:build darwin && amd64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Darwin_x86_64.tar.gz"
	bundledMCPArchiveSHA256  = "d820577e8199dac70ef8a306dbb95fda3b9a26adc30e1f93d7d20cec9a4b0b6d"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Darwin_x86_64.tar.gz
var bundledMCPArchive []byte
