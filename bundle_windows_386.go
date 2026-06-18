//go:build windows && 386

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Windows_i386.zip"
	bundledMCPArchiveSHA256  = "4973f5e7343bc5d8c28b1f04c9c57588f995b980195ce9c55c7adde42dbe9c53"
	bundledMCPExecutableName = "github-mcp-server.exe"
)

//go:embed bundled/github-mcp-server_Windows_i386.zip
var bundledMCPArchive []byte
