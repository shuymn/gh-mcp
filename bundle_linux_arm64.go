//go:build linux && arm64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Linux_arm64.tar.gz"
	bundledMCPArchiveSHA256  = "508cb9032a541bb533494883e49136c2cb01818a69667cf8cefb3a63f9722deb"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Linux_arm64.tar.gz
var bundledMCPArchive []byte
