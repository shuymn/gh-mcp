package main

import (
	"io"
	"os"
)

// ioStreams holds the I/O streams for server communication.
type ioStreams struct {
	in  io.Reader
	out io.Writer
	err io.Writer
}

// defaultIOStreams returns the default I/O streams using os.Std*.
func defaultIOStreams() *ioStreams {
	return &ioStreams{
		in:  os.Stdin,
		out: os.Stdout,
		err: os.Stderr,
	}
}
