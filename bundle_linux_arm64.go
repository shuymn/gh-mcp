//go:build linux && arm64

package main

import _ "embed"

const (
	bundledMCPArchiveName    = "github-mcp-server_Linux_arm64.tar.gz"
	bundledMCPArchiveSHA256  = "25f8028304202674ec2e9977fec3ca0897cac33866dabb51aefd418bc0ce7ef2"
	bundledMCPExecutableName = "github-mcp-server"
)

//go:embed bundled/github-mcp-server_Linux_arm64.tar.gz
var bundledMCPArchive []byte
