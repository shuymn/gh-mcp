//go:build windows && 386

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Windows_i386.zip"
	bundledMCPArchiveSHA256  = "371ffaf470206eaf3056a7d00fe748b756b88502097b107fd2ff60a00aba111f"
	bundledMCPExecutableName = "github-mcp-server.exe"
)

//go:embed bundled/github-mcp-server_Windows_i386.zip
var bundledMCPArchive []byte
