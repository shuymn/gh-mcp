package main

import (
	"errors"
	"testing"
)

// mockAuth implements authInterface for testing
type mockAuth struct {
	defaultHost        string
	tokenForHost       string
	tokenForHostCalled bool
	requestedForHost   string
}

func (m *mockAuth) DefaultHost() string {
	return m.defaultHost
}

func (m *mockAuth) TokenForHost(host string) string {
	m.tokenForHostCalled = true
	m.requestedForHost = host
	return m.tokenForHost
}

func TestGetAuthDetailsWithAuth(t *testing.T) {
	tests := []struct {
		name    string
		mock    *mockAuth
		want    *authDetails
		wantErr error
	}{
		{
			name: "successful auth",
			mock: &mockAuth{
				defaultHost:  "github.com",
				tokenForHost: "test-token-123",
			},
			want: &authDetails{
				Host:  "https://github.com",
				Token: "test-token-123",
			},
		},
		{
			name: "empty token - not logged in",
			mock: &mockAuth{
				defaultHost:  "github.com",
				tokenForHost: "",
			},
			wantErr: ErrNotLoggedIn,
		},
		{
			name: "empty host",
			mock: &mockAuth{
				defaultHost:  "",
				tokenForHost: "some-token",
			},
			wantErr: ErrNoHost,
		},
		{
			name: "enterprise host",
			mock: &mockAuth{
				defaultHost:  "github.enterprise.com",
				tokenForHost: "enterprise-token",
			},
			want: &authDetails{
				Host:  "https://github.enterprise.com",
				Token: "enterprise-token",
			},
		},
		{
			name: "host already has https prefix",
			mock: &mockAuth{
				defaultHost:  "https://github.enterprise.com",
				tokenForHost: "enterprise-token",
			},
			want: &authDetails{
				Host:  "https://github.enterprise.com",
				Token: "enterprise-token",
			},
		},
		{
			name: "host already has http prefix",
			mock: &mockAuth{
				defaultHost:  "http://github.enterprise.com",
				tokenForHost: "enterprise-token",
			},
			want: &authDetails{
				Host:  "http://github.enterprise.com",
				Token: "enterprise-token",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getAuthDetails(tt.mock)

			if tt.mock.defaultHost == "" {
				if tt.mock.tokenForHostCalled {
					t.Errorf(
						"TokenForHost called with %q for empty default host",
						tt.mock.requestedForHost,
					)
				}
			} else {
				if !tt.mock.tokenForHostCalled {
					t.Error("TokenForHost was not called")
				} else if tt.mock.requestedForHost != tt.mock.defaultHost {
					t.Errorf(
						"TokenForHost called with %q, want %q",
						tt.mock.requestedForHost,
						tt.mock.defaultHost,
					)
				}
			}

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("error = %v, want %v", err, tt.wantErr)
				}
				if got != nil {
					t.Errorf("auth details = %#v, want nil", got)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if got.Host != tt.want.Host {
				t.Errorf("Host = %q, want %q", got.Host, tt.want.Host)
			}
			if got.Token != tt.want.Token {
				t.Errorf("Token = %q, want %q", got.Token, tt.want.Token)
			}
		})
	}
}
