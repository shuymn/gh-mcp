//go:build darwin && amd64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Darwin_x86_64.tar.gz"
	bundledMCPArchiveSHA256  = "6e73f5c9738050e44318d37aa919cb8ed2e29453d6341e942dc9c40c9c7ced5b"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Darwin_x86_64.tar.gz
var bundledMCPArchive []byte
