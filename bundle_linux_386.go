//go:build linux && 386

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Linux_i386.tar.gz"
	bundledMCPArchiveSHA256  = "7b26c437f1168a490ab528515a0e729a4ff7bb94a35ed1c482fc107bb0189b3e"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Linux_i386.tar.gz
var bundledMCPArchive []byte
