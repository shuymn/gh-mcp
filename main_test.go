package main

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"
)

// Define static errors for testing
var (
	errNotLoggedIn   = errors.New("not logged in to GitHub. Please run `gh auth login`")
	errServerNonZero = errors.New("server exited with non-zero status: 1")
)

// mockRunner implements runner for testing.
type mockRunner struct {
	authDetails  *authDetails
	authErr      error
	runServerErr error
	capturedEnv  []string
}

func (m *mockRunner) getAuth() (*authDetails, error) {
	return m.authDetails, m.authErr
}

func (m *mockRunner) runServer(_ context.Context, env []string, _ *ioStreams) error {
	m.capturedEnv = env
	return m.runServerErr
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
			name: "run server error",
			mock: &mockRunner{
				authDetails: &authDetails{
					Host:  "github.com",
					Token: "test-token",
				},
				runServerErr: errServerNonZero,
			},
			wantErr: "server exited with non-zero status: 1",
		},
		{
			name: "enterprise host",
			mock: &mockRunner{
				authDetails: &authDetails{
					Host:  "github.enterprise.com",
					Token: "enterprise-token",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runWithRunner(t.Context(), tt.mock)

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

func TestOptionalEnvironmentVariables(t *testing.T) {
	// Set test values using t.Setenv (automatically cleaned up).
	t.Setenv("GITHUB_TOOLSETS", "repos,issues")
	t.Setenv("GITHUB_DYNAMIC_TOOLSETS", "1")
	t.Setenv("GITHUB_READ_ONLY", "1")

	// Create a mock that captures the env parameter.
	mock := &mockRunner{
		authDetails: &authDetails{
			Host:  "https://github.com",
			Token: "test-token",
		},
	}

	err := runWithRunner(t.Context(), mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that all expected env vars are present.
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
	// Ensure env vars are not set.
	t.Setenv("GITHUB_TOOLSETS", "")
	t.Setenv("GITHUB_DYNAMIC_TOOLSETS", "")
	t.Setenv("GITHUB_READ_ONLY", "")

	mock := &mockRunner{
		authDetails: &authDetails{
			Host:  "https://github.com",
			Token: "test-token",
		},
	}

	err := runWithRunner(t.Context(), mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that only required env vars are present.
	requiredEnvs := map[string]string{
		"GITHUB_PERSONAL_ACCESS_TOKEN": "test-token",
		"GITHUB_HOST":                  "https://github.com",
	}

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
