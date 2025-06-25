package main

import (
	"testing"
)

// mockAuth implements authInterface for testing
type mockAuth struct {
	defaultHost  string
	tokenForHost string
}

func (m *mockAuth) DefaultHost() string {
	return m.defaultHost
}

func (m *mockAuth) TokenForHost(_ string) string {
	return m.tokenForHost
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
			name: "empty host",
			mock: &mockAuth{
				defaultHost:  "",
				tokenForHost: "some-token",
			},
			wantErr: ErrNoHost.Error(),
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
