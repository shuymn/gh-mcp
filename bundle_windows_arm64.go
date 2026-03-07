//go:build windows && arm64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Windows_arm64.zip"
	bundledMCPArchiveSHA256  = "e9d2f0cf5c072522816d7e9cb1a2b8076d5f3f9770d122e261b609346400fe78"
	bundledMCPExecutableName = "github-mcp-server.exe"
)

//go:embed bundled/github-mcp-server_Windows_arm64.zip
var bundledMCPArchive []byte
