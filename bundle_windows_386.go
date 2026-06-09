//go:build windows && 386

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Windows_i386.zip"
	bundledMCPArchiveSHA256  = "11fa93c1835b9d6ec20c9383ac1b9696689af048b4a3a3dc97c2c9f2047b70e4"
	bundledMCPExecutableName = "github-mcp-server.exe"
)

//go:embed bundled/github-mcp-server_Windows_i386.zip
var bundledMCPArchive []byte
