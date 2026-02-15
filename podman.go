package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrPodmanSocketUnavailable indicates no reachable Podman docker-API socket was found.
	ErrPodmanSocketUnavailable = errors.New("podman socket unavailable")
	// ErrPodmanUnavailable indicates the podman CLI is not available.
	ErrPodmanUnavailable = errors.New("podman unavailable")
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

func podmanEnsureImageCLI(ctx context.Context, imageName string, streams *ioStreams) error {
	_, err := podmanPath()
	if err != nil {
		return err
	}

	exists, err := podmanImageExists(ctx, imageName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	cmd := exec.CommandContext(ctx, "podman", "pull", imageName)
	cmd.Stdin = nil
	cmd.Stdout = streams.err
	cmd.Stderr = streams.err
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("podman pull failed: %w", err)
	}
	return nil
}

func podmanRunCLI(ctx context.Context, env []string, imageName string, streams *ioStreams) error {
	_, err := podmanPath()
	if err != nil {
		return err
	}

	containerName := podmanContainerName()
	ctxRun, cancel := context.WithCancel(ctx)
	defer cancel()

	pr, pw := io.Pipe()
	stdinClosed := make(chan struct{})
	go func() {
		_, err := io.Copy(pw, streams.in)
		_ = pw.CloseWithError(err)
		close(stdinClosed)
	}()
	go func() {
		select {
		case <-stdinClosed:
			cancel()
		case <-ctx.Done():
			cancel()
		}
	}()

	args, cmdEnv, err := buildPodmanRunArgs(env, imageName, containerName)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctxRun, "podman", args...)
	cmd.Stdin = pr
	cmd.Stdout = streams.out
	cmd.Stderr = streams.err
	cmd.Env = append(os.Environ(), cmdEnv...)

	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = exec.CommandContext(cleanupCtx, "podman", "rm", "-f", containerName).Run()
	}()

	err = cmd.Run()
	_ = pr.Close()

	if err == nil {
		return nil
	}

	if ctxRun.Err() != nil || errors.Is(err, context.Canceled) {
		return nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Errorf("%w: %d", ErrContainerNonZeroExit, exitErr.ExitCode())
	}
	return fmt.Errorf("podman run failed: %w", err)
}

func podmanPath() (string, error) {
	path, err := exec.LookPath("podman")
	if err != nil {
		return "", ErrPodmanUnavailable
	}
	return path, nil
}

func podmanImageExists(ctx context.Context, imageName string) (bool, error) {
	cmd := exec.CommandContext(ctx, "podman", "image", "exists", imageName)
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if err == nil {
		return true, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("podman image exists failed: %w", err)
}

func podmanContainerName() string {
	return fmt.Sprintf("gh-mcp-%d-%d", os.Getpid(), time.Now().Unix())
}

func buildPodmanRunArgs(env []string, imageName, containerName string) ([]string, []string, error) {
	args := []string{
		"run",
		"--rm",
		"-i",
		"--name",
		containerName,
		"--pull=never",
	}

	var cmdEnv []string
	for _, e := range env {
		key, value, ok := strings.Cut(e, "=")
		if !ok || key == "" {
			return nil, nil, fmt.Errorf("invalid env entry %q", e)
		}
		args = append(args, "--env", key)
		cmdEnv = append(cmdEnv, key+"="+value)
	}

	args = append(args, imageName, "stdio")
	return args, cmdEnv, nil
}
