package main

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"
)

// Define a static error for testing.
var errServerNonZero = errors.New("server exited with non-zero status: 1")

// mockRunner implements runner for testing.
type mockRunner struct {
	authDetails     *authDetails
	authErr         error
	runServerErr    error
	runServerCalled bool
	capturedEnv     []string
}

func (m *mockRunner) getAuth() (*authDetails, error) {
	return m.authDetails, m.authErr
}

func (m *mockRunner) runServer(_ context.Context, env []string, _ *ioStreams) error {
	m.runServerCalled = true
	m.capturedEnv = env
	return m.runServerErr
}

func TestRunWithRunner(t *testing.T) {
	tests := []struct {
		name          string
		mock          *mockRunner
		wantErr       error
		wantRunServer bool
		wantHost      string
	}{
		{
			name: "auth error",
			mock: &mockRunner{
				authErr: ErrNotLoggedIn,
			},
			wantErr: ErrNotLoggedIn,
		},
		{
			name: "run server error",
			mock: &mockRunner{
				authDetails: &authDetails{
					Host:  "https://github.com",
					Token: "test-token",
				},
				runServerErr: errServerNonZero,
			},
			wantErr:       errServerNonZero,
			wantRunServer: true,
		},
		{
			name: "enterprise host",
			mock: &mockRunner{
				authDetails: &authDetails{
					Host:  "https://github.enterprise.com",
					Token: "enterprise-token",
				},
			},
			wantRunServer: true,
			wantHost:      "https://github.enterprise.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runWithRunner(t.Context(), tt.mock)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want %v", err, tt.wantErr)
			}
			if tt.mock.runServerCalled != tt.wantRunServer {
				t.Errorf(
					"runServer called = %t, want %t",
					tt.mock.runServerCalled,
					tt.wantRunServer,
				)
			}
			if tt.wantHost != "" {
				expectedHost := "GITHUB_HOST=" + tt.wantHost
				if !slices.Contains(tt.mock.capturedEnv, expectedHost) {
					t.Errorf("%s not found in %v", expectedHost, tt.mock.capturedEnv)
				}
			}
		})
	}
}

func TestOptionalEnvironmentVariables(t *testing.T) {
	// Set test values using t.Setenv (automatically cleaned up).
	t.Setenv("GITHUB_TOOLSETS", "repos,issues")
	t.Setenv("GITHUB_TOOLS", "get_file_contents")
	t.Setenv("GITHUB_DYNAMIC_TOOLSETS", "1")
	t.Setenv("GITHUB_READ_ONLY", "1")
	t.Setenv("GITHUB_LOCKDOWN_MODE", "1")

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
		"GITHUB_TOOLS":                 "get_file_contents",
		"GITHUB_DYNAMIC_TOOLSETS":      "1",
		"GITHUB_READ_ONLY":             "1",
		"GITHUB_LOCKDOWN_MODE":         "1",
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
	t.Setenv("GITHUB_TOOLS", "")
	t.Setenv("GITHUB_DYNAMIC_TOOLSETS", "")
	t.Setenv("GITHUB_READ_ONLY", "")
	t.Setenv("GITHUB_LOCKDOWN_MODE", "")

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

func TestRunWithRunnerRejectsInvalidServerEnvValue(t *testing.T) {
	t.Run("invalid token", func(t *testing.T) {
		mock := &mockRunner{
			authDetails: &authDetails{
				Host:  "https://github.com",
				Token: "test-token\ninvalid",
			},
		}

		err := runWithRunner(t.Context(), mock)
		if err == nil {
			t.Fatal("expected error for invalid token env value")
		}
		if !errors.Is(err, ErrInvalidServerEnvValue) {
			t.Fatalf("expected ErrInvalidServerEnvValue, got: %v", err)
		}
		if mock.runServerCalled {
			t.Fatal("runServer called with invalid token env value")
		}
	})

	t.Run("invalid optional env", func(t *testing.T) {
		t.Setenv("GITHUB_TOOLSETS", "repos,issues\npull_requests")

		mock := &mockRunner{
			authDetails: &authDetails{
				Host:  "https://github.com",
				Token: "test-token",
			},
		}

		err := runWithRunner(t.Context(), mock)
		if err == nil {
			t.Fatal("expected error for invalid optional env value")
		}
		if !errors.Is(err, ErrInvalidServerEnvValue) {
			t.Fatalf("expected ErrInvalidServerEnvValue, got: %v", err)
		}
		if mock.runServerCalled {
			t.Fatal("runServer called with invalid optional env value")
		}
	})
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
			t.Setenv("LOG_LEVEL", tt.envValue)

			result := parseLogLevel()
			if result != tt.expected {
				t.Errorf("parseLogLevel() = %v, want %v", result, tt.expected)
			}
		})
	}
}
