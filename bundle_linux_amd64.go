//go:build linux && amd64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Linux_x86_64.tar.gz"
	bundledMCPArchiveSHA256  = "95843162759da2c31dde082dd145be35db82164594796c294414b69790c2290e"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Linux_x86_64.tar.gz
var bundledMCPArchive []byte
