//go:build windows && amd64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Windows_x86_64.zip"
	bundledMCPArchiveSHA256  = "6fb5373eda23ee2199b9ff530716d2ada20116de5edeb4c17ed270737d1755b3"
	bundledMCPExecutableName = "github-mcp-server.exe"
)

//go:embed bundled/github-mcp-server_Windows_x86_64.zip
var bundledMCPArchive []byte
