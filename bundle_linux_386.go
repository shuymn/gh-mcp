//go:build linux && 386

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Linux_i386.tar.gz"
	bundledMCPArchiveSHA256  = "9dcfd6538e84067722a2736399e9ffcad6c377fa5e8bb1d8583fb01485b4796d"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Linux_i386.tar.gz
var bundledMCPArchive []byte
