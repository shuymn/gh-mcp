package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

var (
	// ErrPodmanSocketUnavailable indicates no reachable Podman docker-API socket was found.
	ErrPodmanSocketUnavailable = errors.New("podman socket unavailable")
)

type dockerClientFactory interface {
	newDockerClient() (dockerClientInterface, error)
	newDockerClientWithHost(host string) (dockerClientInterface, error)
}

type podmanHostCandidate struct {
	host    string
	useEnv  bool // when true, create client via FromEnv (DOCKER_HOST and friends)
	enabled bool
}

func podmanDockerHostCandidates() []podmanHostCandidate {
	var candidates []podmanHostCandidate

	if dockerHost := os.Getenv("DOCKER_HOST"); dockerHost != "" {
		candidates = append(candidates, podmanHostCandidate{useEnv: true, enabled: true})
	}

	if runtime.GOOS == "windows" {
		return candidates
	}

	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		candidates = append(candidates, podmanHostCandidate{
			host:    "unix://" + filepath.Join(xdg, "podman", "podman.sock"),
			enabled: true,
		})
	}

	uid := strconv.Itoa(os.Getuid())
	candidates = append(candidates,
		podmanHostCandidate{host: "unix:///run/user/" + uid + "/podman/podman.sock", enabled: true},
		podmanHostCandidate{host: "unix:///run/podman/podman.sock", enabled: true},
	)

	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		machineSock := filepath.Join(home, ".local", "share", "containers", "podman", "machine", "podman.sock")
		if _, err := os.Stat(machineSock); err == nil {
			candidates = append(candidates, podmanHostCandidate{
				host:    "unix://" + machineSock,
				enabled: true,
			})
		}
	}

	return dedupePodmanHostCandidates(candidates)
}

func dedupePodmanHostCandidates(in []podmanHostCandidate) []podmanHostCandidate {
	seen := map[string]struct{}{}
	var out []podmanHostCandidate
	for _, c := range in {
		key := c.host
		if c.useEnv {
			key = "<env>"
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, c)
	}
	return out
}

func newPodmanDockerClient(ctx context.Context, f dockerClientFactory) (dockerClientInterface, string, error) {
	candidates := podmanDockerHostCandidates()
	if len(candidates) == 0 {
		return nil, "", ErrPodmanSocketUnavailable
	}

	var lastErr error
	for _, cand := range candidates {
		if !cand.enabled {
			continue
		}

		var (
			cli dockerClientInterface
			err error
		)

		if cand.useEnv {
			cli, err = f.newDockerClient()
		} else {
			cli, err = f.newDockerClientWithHost(cand.host)
		}
		if err != nil {
			lastErr = err
			continue
		}

		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err = cli.Ping(pingCtx)
		cancel()
		if err == nil {
			if cand.useEnv {
				return cli, os.Getenv("DOCKER_HOST"), nil
			}
			return cli, cand.host, nil
		}

		lastErr = err
		_ = cli.Close()
	}

	if lastErr != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrPodmanSocketUnavailable, lastErr)
	}
	return nil, "", ErrPodmanSocketUnavailable
}

