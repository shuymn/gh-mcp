//go:build windows && amd64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Windows_x86_64.zip"
	bundledMCPArchiveSHA256  = "c91eca7ffd5492c2b273dfcc8747d4b54afddcde342704572a6917c73757f608"
	bundledMCPExecutableName = "github-mcp-server.exe"
)

//go:embed bundled/github-mcp-server_Windows_x86_64.zip
var bundledMCPArchive []byte
