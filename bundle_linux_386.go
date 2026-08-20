//go:build linux && 386

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Linux_i386.tar.gz"
	bundledMCPArchiveSHA256  = "af05fa17615a7baccb6106d0ec66b03476f4035de7a75c63232a124ddc1a5727"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Linux_i386.tar.gz
var bundledMCPArchive []byte
