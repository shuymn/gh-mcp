//go:build windows && amd64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Windows_x86_64.zip"
	bundledMCPArchiveSHA256  = "14882ca059cd2eccc037388d586b552b60a891b126050f8cefff8beab3c9157f"
	bundledMCPExecutableName = "github-mcp-server.exe"
)

//go:embed bundled/github-mcp-server_Windows_x86_64.zip
var bundledMCPArchive []byte
