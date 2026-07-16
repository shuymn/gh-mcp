//go:build linux && amd64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Linux_x86_64.tar.gz"
	bundledMCPArchiveSHA256  = "27443d173f209e60d4af9777e624bfea3de1af24897d46cc7324f01cf279a41d"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Linux_x86_64.tar.gz
var bundledMCPArchive []byte
