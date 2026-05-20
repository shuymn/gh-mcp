//go:build linux && amd64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Linux_x86_64.tar.gz"
	bundledMCPArchiveSHA256  = "201082f569a846eaefd4318f13bccb5d9227c2cec45037d1d292ee83111173c1"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Linux_x86_64.tar.gz
var bundledMCPArchive []byte
