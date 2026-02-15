package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

type options struct {
	engine string
}

const (
	engineAuto   = "auto"
	engineDocker = "docker"
	enginePodman = "podman"
)

func parseOptions(args []string) (options, error) {
	defaultEngine := os.Getenv("GH_MCP_ENGINE")
	if defaultEngine == "" {
		defaultEngine = engineAuto
	}

	fs := flag.NewFlagSet("gh-mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	engineFlag := fs.String("engine", defaultEngine, "Container engine to use: auto, docker, podman")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}

	engine, err := normalizeEngine(*engineFlag)
	if err != nil {
		return options{}, err
	}

	return options{engine: engine}, nil
}

func normalizeEngine(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "", engineAuto:
		return engineAuto, nil
	case engineDocker:
		return engineDocker, nil
	case enginePodman:
		return enginePodman, nil
	default:
		return "", fmt.Errorf("invalid --engine value %q (expected auto|docker|podman)", v)
	}
}

