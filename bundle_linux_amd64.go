//go:build linux && amd64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Linux_x86_64.tar.gz"
	bundledMCPArchiveSHA256  = "221bb1e5b14cd298405e0e126686aabf32f1d9222d9537115e806a8fa8722f55"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Linux_x86_64.tar.gz
var bundledMCPArchive []byte
