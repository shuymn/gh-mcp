//go:build windows && amd64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Windows_x86_64.zip"
	bundledMCPArchiveSHA256  = "c08872e69f700d4219e7b4ab9607d56d7993171519ee32b62fccb8fba0cab673"
	bundledMCPExecutableName = "github-mcp-server.exe"
)

//go:embed bundled/github-mcp-server_Windows_x86_64.zip
var bundledMCPArchive []byte
