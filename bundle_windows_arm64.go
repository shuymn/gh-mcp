//go:build windows && arm64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Windows_arm64.zip"
	bundledMCPArchiveSHA256  = "c4803f3cee5220ad55fe05c6390caab82d45a9a9f853e1c08c0462396fca1861"
	bundledMCPExecutableName = "github-mcp-server.exe"
)

//go:embed bundled/github-mcp-server_Windows_arm64.zip
var bundledMCPArchive []byte
