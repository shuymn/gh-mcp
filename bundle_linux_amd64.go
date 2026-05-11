//go:build linux && amd64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Linux_x86_64.tar.gz"
	bundledMCPArchiveSHA256  = "d7290b31a9f91048ca25443256ed0c70128d83a9ff53d7861e73109325555a9c"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Linux_x86_64.tar.gz
var bundledMCPArchive []byte
