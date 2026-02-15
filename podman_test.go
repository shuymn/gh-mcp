package main

import (
	"errors"
	"testing"
)

type fakeDockerFactory struct {
	envClient dockerClientInterface
	envErr    error

	hostClients map[string]dockerClientInterface
	hostErrs    map[string]error

	calls []string
}

func (f *fakeDockerFactory) newDockerClient() (dockerClientInterface, error) {
	f.calls = append(f.calls, "<env>")
	return f.envClient, f.envErr
}

func (f *fakeDockerFactory) newDockerClientWithHost(host string) (dockerClientInterface, error) {
	f.calls = append(f.calls, host)
	if err, ok := f.hostErrs[host]; ok {
		return nil, err
	}
	if c, ok := f.hostClients[host]; ok {
		return c, nil
	}
	return nil, errors.New("unknown host")
}

func TestPodmanDockerHostCandidates_DockerHostFirst(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///example.sock")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	cands := podmanDockerHostCandidates()
	if len(cands) == 0 {
		t.Fatalf("expected at least one candidate")
	}
	if !cands[0].useEnv {
		t.Fatalf("expected first candidate to use env when DOCKER_HOST is set, got %#v", cands[0])
	}
}

func TestNewPodmanDockerClient_TriesEnvThenSocket(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("DOCKER_HOST", "unix:///env.sock")
	t.Setenv("XDG_RUNTIME_DIR", xdg)

	wantHost := "unix://" + xdg + "/podman/podman.sock"

	envClient := &mockDockerClient{pingErr: errors.New("no daemon")}
	socketClient := &mockDockerClient{}

	f := &fakeDockerFactory{
		envClient: envClient,
		hostClients: map[string]dockerClientInterface{
			wantHost: socketClient,
		},
		hostErrs: map[string]error{},
	}

	cli, host, err := newPodmanDockerClient(t.Context(), f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cli != socketClient {
		t.Fatalf("unexpected client: %#v", cli)
	}
	if host != wantHost {
		t.Fatalf("host=%q, want %q", host, wantHost)
	}
	if len(f.calls) < 2 || f.calls[0] != "<env>" || f.calls[1] != wantHost {
		t.Fatalf("unexpected call order: %v", f.calls)
	}
}

func TestBuildPodmanRunArgs_SecretsNotInArgs(t *testing.T) {
	secret := "super-secret-token"
	env := []string{
		"GITHUB_PERSONAL_ACCESS_TOKEN=" + secret,
		"GITHUB_HOST=https://github.com",
	}

	args, cmdEnv, err := buildPodmanRunArgs(env, "ghcr.io/example/image:latest", "test-container")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, a := range args {
		if a == secret || (len(a) > 0 && a == "GITHUB_PERSONAL_ACCESS_TOKEN="+secret) {
			t.Fatalf("secret leaked into args: %v", args)
		}
		if a == "GITHUB_PERSONAL_ACCESS_TOKEN="+secret {
			t.Fatalf("secret leaked into args: %v", args)
		}
	}

	foundKeyOnly := false
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--env" && args[i+1] == "GITHUB_PERSONAL_ACCESS_TOKEN" {
			foundKeyOnly = true
			break
		}
	}
	if !foundKeyOnly {
		t.Fatalf("expected --env GITHUB_PERSONAL_ACCESS_TOKEN in args: %v", args)
	}

	foundEnv := false
	for _, e := range cmdEnv {
		if e == "GITHUB_PERSONAL_ACCESS_TOKEN="+secret {
			foundEnv = true
			break
		}
	}
	if !foundEnv {
		t.Fatalf("expected secret to be present only in cmd env, got %v", cmdEnv)
	}
}
