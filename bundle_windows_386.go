//go:build windows && 386

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Windows_i386.zip"
	bundledMCPArchiveSHA256  = "a6db266eddef5cb13cad353b1af28c57d1a79884a91de263565b22c42c03eb97"
	bundledMCPExecutableName = "github-mcp-server.exe"
)

//go:embed bundled/github-mcp-server_Windows_i386.zip
var bundledMCPArchive []byte
