//go:build darwin && arm64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Darwin_arm64.tar.gz"
	bundledMCPArchiveSHA256  = "43557c903a13fce990975e0adf43f7dce085442bfc00692de62a1ed65b41617a"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Darwin_arm64.tar.gz
var bundledMCPArchive []byte
