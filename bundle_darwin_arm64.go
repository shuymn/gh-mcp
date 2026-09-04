//go:build darwin && arm64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Darwin_arm64.tar.gz"
	bundledMCPArchiveSHA256  = "e559f9da66cd639d14f72ca5402d2ad8e258849d30458bbbf3347d5408b02676"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Darwin_arm64.tar.gz
var bundledMCPArchive []byte
