// Package identity resolves the GitHub host and token that gh is logged in with.
package identity

import (
	"errors"
	"strings"
)

var (
	// ErrNoHost is returned when gh has no default host.
	ErrNoHost = errors.New("gh has no default host; run `gh auth status`")
	// ErrNotLoggedIn is returned when gh has no token for the host.
	ErrNotLoggedIn = errors.New("not logged in to GitHub; run `gh auth login`")
)

// Identity is the GitHub host and token passed to github-mcp-server.
type Identity struct {
	// Host is the server URL, for example https://github.com.
	Host  string
	Token string
}

// Source looks up gh authentication. The second return values of go-gh's
// auth.DefaultHost and auth.TokenForHost (where the value came from) are ignored.
type Source struct {
	DefaultHost  func() (string, string)
	TokenForHost func(host string) (string, string)
}

// Resolve returns the identity for gh's default host.
func Resolve(src Source) (Identity, error) {
	host, _ := src.DefaultHost()
	if host == "" {
		return Identity{}, ErrNoHost
	}

	token, _ := src.TokenForHost(host)
	if token == "" {
		return Identity{}, ErrNotLoggedIn
	}

	if !strings.Contains(host, "://") {
		host = "https://" + host
	}

	return Identity{Host: host, Token: token}, nil
}
