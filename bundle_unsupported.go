//go:build !(darwin && (amd64 || arm64)) && !(linux && (386 || amd64 || arm64)) && !(windows && (386 || amd64 || arm64))

package main

const (
	bundledMCPArchiveName    = ""
	bundledMCPArchiveSHA256  = ""
	bundledMCPExecutableName = ""
)

var bundledMCPArchive []byte
