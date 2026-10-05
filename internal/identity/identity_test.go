package identity_test

import (
	"errors"
	"testing"

	"github.com/shuymn/gh-mcp/internal/identity"
)

func source(host, token string) identity.Source {
	return identity.Source{
		DefaultHost:  func() (string, string) { return host, "default" },
		TokenForHost: func(string) (string, string) { return token, "keyring" },
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name, host, token string
		want              identity.Identity
		wantErr           error
	}{
		{
			name:  "hostname gets https",
			host:  "github.com",
			token: "gho_x",
			want:  identity.Identity{Host: "https://github.com", Token: "gho_x"},
		},
		{
			name:  "url kept",
			host:  "http://ghe.local",
			token: "t",
			want:  identity.Identity{Host: "http://ghe.local", Token: "t"},
		},
		{name: "no host", host: "", token: "t", wantErr: identity.ErrNoHost},
		{name: "no token", host: "github.com", token: "", wantErr: identity.ErrNotLoggedIn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := identity.Resolve(source(tt.host, tt.token))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Resolve() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
