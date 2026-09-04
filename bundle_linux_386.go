//go:build linux && 386

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Linux_i386.tar.gz"
	bundledMCPArchiveSHA256  = "cbd287a7bdb3a501c2439228265148dfc01d32dd8ff39830c0cb0e6524fa8c30"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Linux_i386.tar.gz
var bundledMCPArchive []byte
