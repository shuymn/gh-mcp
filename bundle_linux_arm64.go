//go:build linux && arm64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Linux_arm64.tar.gz"
	bundledMCPArchiveSHA256  = "c51dc6cf192c35a328b9f71696d42c38a9a3ba3c2ffe010da836bed071d1ac8a"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Linux_arm64.tar.gz
var bundledMCPArchive []byte
