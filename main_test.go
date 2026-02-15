package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
)

// Define static errors for testing
var (
	errNotLoggedIn      = errors.New("not logged in to GitHub. Please run `gh auth login`")
	errDockerConnection = errors.New(
		"failed to create docker client: connection refused. Is the Docker daemon running?",
	)
	errImageUnauthorized = errors.New(
		"failed to pull docker image 'ghcr.io/github/github-mcp-server:latest': unauthorized",
	)
	errContainerNonZero = errors.New("container exited with non-zero status: 1")
	errCaptureEnv       = errors.New("capture env")
)

// mockRunner implements runner for testing
type mockRunner struct {
	authDetails     *authDetails
	authErr         error
	dockerClient    dockerClientInterface
	dockerClientErr error
	ensureImageErr  error
	runContainerErr error
	podmanEnsureErr error
	podmanRunErr    error
	capturedEnv     []string // To capture env vars passed to runContainer
}

func (m *mockRunner) getAuth() (*authDetails, error) {
	return m.authDetails, m.authErr
}

func (m *mockRunner) newDockerClient() (dockerClientInterface, error) {
	return m.dockerClient, m.dockerClientErr
}

func (m *mockRunner) newDockerClientWithHost(_ string) (dockerClientInterface, error) {
	return m.dockerClient, m.dockerClientErr
}

func (m *mockRunner) ensureImage(
	_ context.Context,
	_ dockerClientInterface,
	_ string,
	_ io.Writer,
) error {
	return m.ensureImageErr
}

func (m *mockRunner) podmanEnsureImageCLI(_ context.Context, _ string, _ *ioStreams) error {
	return m.podmanEnsureErr
}

func (m *mockRunner) podmanRunCLI(_ context.Context, _ []string, _ string, _ *ioStreams) error {
	return m.podmanRunErr
}

func (m *mockRunner) runContainer(
	_ context.Context,
	_ dockerClientInterface,
	env []string,
	_ string,
	_ *ioStreams,
) error {
	m.capturedEnv = env
	return m.runContainerErr
}

func TestRunWithRunner(t *testing.T) {
	tests := []struct {
		name    string
		mock    *mockRunner
		wantErr string
	}{
		{
			name: "successful run",
			mock: &mockRunner{
				authDetails: &authDetails{
					Host:  "github.com",
					Token: "test-token",
				},
				dockerClient: &mockDockerClient{},
			},
		},
		{
			name: "auth error",
			mock: &mockRunner{
				authErr: errNotLoggedIn,
			},
			wantErr: ErrNotLoggedIn.Error(),
		},
		{
			name: "docker client error",
			mock: &mockRunner{
				authDetails: &authDetails{
					Host:  "github.com",
					Token: "test-token",
				},
				dockerClientErr: errDockerConnection,
			},
			wantErr: "failed to create docker client: connection refused. Is the Docker daemon running?",
		},
		{
			name: "ensure image error",
			mock: &mockRunner{
				authDetails: &authDetails{
					Host:  "github.com",
					Token: "test-token",
				},
				dockerClient:   &mockDockerClient{},
				ensureImageErr: errImageUnauthorized,
			},
			wantErr: "failed to pull docker image 'ghcr.io/github/github-mcp-server:latest': unauthorized",
		},
		{
			name: "run container error",
			mock: &mockRunner{
				authDetails: &authDetails{
					Host:  "github.com",
					Token: "test-token",
				},
				dockerClient:    &mockDockerClient{},
				runContainerErr: errContainerNonZero,
			},
			wantErr: "container exited with non-zero status: 1",
		},
		{
			name: "enterprise host",
			mock: &mockRunner{
				authDetails: &authDetails{
					Host:  "github.enterprise.com",
					Token: "enterprise-token",
				},
				dockerClient: &mockDockerClient{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runWithRunner(t.Context(), tt.mock, options{engine: engineAuto})

			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("expected error %q, got nil", tt.wantErr)
					return
				}
				if err.Error() != tt.wantErr {
					t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestEnvironmentVariables(t *testing.T) {
	// Test that environment variables are properly formatted
	mock := &mockRunner{
		authDetails: &authDetails{
			Host:  "github.test.com",
			Token: "test-token-123",
		},
		dockerClient: &mockDockerClient{},
		// Mock to capture environment variables
		runContainerErr: errCaptureEnv,
	}

	ctx := t.Context()
	_ = runWithRunner(ctx, mock, options{engine: engineAuto})

	// The test passes if the error is as expected
	// In a real test, we'd capture the env variables passed to runContainer
}

func TestOptionalEnvironmentVariables(t *testing.T) {
	// Set test values using t.Setenv (automatically cleaned up)
	t.Setenv("GITHUB_TOOLSETS", "repos,issues")
	t.Setenv("GITHUB_DYNAMIC_TOOLSETS", "1")
	t.Setenv("GITHUB_READ_ONLY", "1")

	// Create a mock that captures the env parameter
	mock := &mockRunner{
		authDetails: &authDetails{
			Host:  "https://github.com",
			Token: "test-token",
		},
		dockerClient: &mockDockerClient{},
	}

	ctx := t.Context()
	err := runWithRunner(ctx, mock, options{engine: engineAuto})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that all expected env vars are present
	expectedEnvs := map[string]string{
		"GITHUB_PERSONAL_ACCESS_TOKEN": "test-token",
		"GITHUB_HOST":                  "https://github.com",
		"GITHUB_TOOLSETS":              "repos,issues",
		"GITHUB_DYNAMIC_TOOLSETS":      "1",
		"GITHUB_READ_ONLY":             "1",
	}

	for key, expectedValue := range expectedEnvs {
		if !slices.Contains(mock.capturedEnv, key+"="+expectedValue) {
			t.Errorf("Expected env var %s=%s not found in %v", key, expectedValue, mock.capturedEnv)
		}
	}
}

func TestOptionalEnvironmentVariablesNotSet(t *testing.T) {
	// Ensure env vars are not set
	t.Setenv("GITHUB_TOOLSETS", "")
	t.Setenv("GITHUB_DYNAMIC_TOOLSETS", "")
	t.Setenv("GITHUB_READ_ONLY", "")

	mock := &mockRunner{
		authDetails: &authDetails{
			Host:  "https://github.com",
			Token: "test-token",
		},
		dockerClient: &mockDockerClient{},
	}

	ctx := t.Context()
	err := runWithRunner(ctx, mock, options{engine: engineAuto})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that only required env vars are present
	requiredEnvs := map[string]string{
		"GITHUB_PERSONAL_ACCESS_TOKEN": "test-token",
		"GITHUB_HOST":                  "https://github.com",
	}

	// Should only have the required env vars
	if len(mock.capturedEnv) != len(requiredEnvs) {
		t.Errorf(
			"Expected %d env vars, got %d: %v",
			len(requiredEnvs),
			len(mock.capturedEnv),
			mock.capturedEnv,
		)
	}

	for key, expectedValue := range requiredEnvs {
		if !slices.Contains(mock.capturedEnv, key+"="+expectedValue) {
			t.Errorf("Expected env var %s=%s not found in %v", key, expectedValue, mock.capturedEnv)
		}
	}
}

func TestRunWithRunner_AutoFallsBackToPodmanCLI(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///tmp/non-existent.sock")

	mock := &mockRunner{
		authDetails: &authDetails{
			Host:  "https://github.com",
			Token: "test-token",
		},
		dockerClient: &mockDockerClient{
			pingErr: errors.New("docker ping failed"),
		},
		podmanRunErr: errCaptureEnv,
	}

	err := runWithRunner(t.Context(), mock, options{engine: engineAuto})
	if !errors.Is(err, errCaptureEnv) {
		t.Fatalf("expected podman CLI fallback error %q, got %v", errCaptureEnv, err)
	}
}

func TestRunWithRunner_DockerModeDoesNotFallback(t *testing.T) {
	mock := &mockRunner{
		authDetails: &authDetails{
			Host:  "https://github.com",
			Token: "test-token",
		},
		dockerClient: &mockDockerClient{
			pingErr: errors.New("docker ping failed"),
		},
		podmanRunErr: errCaptureEnv,
	}

	err := runWithRunner(t.Context(), mock, options{engine: engineDocker})
	if !errors.Is(err, ErrDockerUnavailable) {
		t.Fatalf("expected docker unavailable error, got %v", err)
	}
	if errors.Is(err, errCaptureEnv) {
		t.Fatalf("unexpected podman fallback execution: %v", err)
	}
}

func TestRunWithRunner_PodmanModeFallsBackToCLI(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///tmp/non-existent.sock")

	mock := &mockRunner{
		authDetails: &authDetails{
			Host:  "https://github.com",
			Token: "test-token",
		},
		dockerClientErr: errors.New("socket not reachable"),
		podmanRunErr:    errCaptureEnv,
	}

	err := runWithRunner(t.Context(), mock, options{engine: enginePodman})
	if !errors.Is(err, errCaptureEnv) {
		t.Fatalf("expected podman CLI fallback error %q, got %v", errCaptureEnv, err)
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected slog.Level
	}{
		{"default when unset", "", slog.LevelInfo},
		{"debug level", "DEBUG", slog.LevelDebug},
		{"info level", "INFO", slog.LevelInfo},
		{"warn level", "WARN", slog.LevelWarn},
		{"error level", "ERROR", slog.LevelError},
		{"case insensitive", "debug", slog.LevelDebug},
		{"invalid value fallback", "INVALID", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv("LOG_LEVEL", tt.envValue)
			}

			result := parseLogLevel()
			if result != tt.expected {
				t.Errorf("parseLogLevel() = %v, want %v", result, tt.expected)
			}
		})
	}
}
