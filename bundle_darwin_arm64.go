//go:build darwin && arm64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Darwin_arm64.tar.gz"
	bundledMCPArchiveSHA256  = "dbe22c73b3eb9491cc3e27dba45352b980b1c43e4c857645689a0251c5e06a21"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Darwin_arm64.tar.gz
var bundledMCPArchive []byte
