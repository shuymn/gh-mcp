package main

import (
	"errors"
	"testing"
)

// Define static errors for testing
var (
	errHostTest  = errors.New("host error")
	errTokenTest = errors.New("token error")
)

// mockAuth implements authInterface for testing
type mockAuth struct {
	defaultHost     string
	defaultHostErr  error
	tokenForHost    string
	tokenForHostErr error
}

func (m *mockAuth) DefaultHost() (string, error) {
	return m.defaultHost, m.defaultHostErr
}

func (m *mockAuth) TokenForHost(_ string) (string, error) {
	return m.tokenForHost, m.tokenForHostErr
}

func TestGetAuthDetailsWithAuth(t *testing.T) {
	tests := []struct {
		name    string
		mock    *mockAuth
		want    *authDetails
		wantErr string
	}{
		{
			name: "successful auth",
			mock: &mockAuth{
				defaultHost:  "github.com",
				tokenForHost: "test-token-123",
			},
			want: &authDetails{
				Host:  "github.com",
				Token: "test-token-123",
			},
		},
		{
			name: "empty token - not logged in",
			mock: &mockAuth{
				defaultHost:  "github.com",
				tokenForHost: "",
			},
			wantErr: ErrNotLoggedIn.Error(),
		},
		{
			name: "default host error",
			mock: &mockAuth{
				defaultHostErr: errHostTest,
			},
			wantErr: "failed to get default host: host error",
		},
		{
			name: "token for host error",
			mock: &mockAuth{
				defaultHost:     "github.com",
				tokenForHostErr: errTokenTest,
			},
			wantErr: "failed to get token for host github.com: token error",
		},
		{
			name: "enterprise host",
			mock: &mockAuth{
				defaultHost:  "github.enterprise.com",
				tokenForHost: "enterprise-token",
			},
			want: &authDetails{
				Host:  "github.enterprise.com",
				Token: "enterprise-token",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getAuthDetailsWithAuth(tt.mock)

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
