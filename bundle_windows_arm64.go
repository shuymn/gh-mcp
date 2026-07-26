//go:build windows && arm64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Windows_arm64.zip"
	bundledMCPArchiveSHA256  = "68a67fb249f846058c58cab88e1e73981650fca29e1c843be523959af00a0070"
	bundledMCPExecutableName = "github-mcp-server.exe"
)

//go:embed bundled/github-mcp-server_Windows_arm64.zip
var bundledMCPArchive []byte
