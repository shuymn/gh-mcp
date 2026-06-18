//go:build linux && 386

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Linux_i386.tar.gz"
	bundledMCPArchiveSHA256  = "149ce46c2aeacd11d68ec968cc7572e01ea53d008d11a3721b8cbd5ac53644f6"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Linux_i386.tar.gz
var bundledMCPArchive []byte
