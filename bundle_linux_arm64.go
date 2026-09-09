//go:build linux && arm64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Linux_arm64.tar.gz"
	bundledMCPArchiveSHA256  = "0f613c61fa524b30278a249ae199220465ff051e79177e58866e5e677191fda9"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Linux_arm64.tar.gz
var bundledMCPArchive []byte
