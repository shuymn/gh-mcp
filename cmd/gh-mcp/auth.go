package main

import (
	"errors"

	"github.com/cli/go-gh/v2/pkg/auth"
)

// Define static errors
var (
	// ErrNotLoggedIn is returned when the user is not authenticated with GitHub
	ErrNotLoggedIn = errors.New("not logged in to GitHub. Please run `gh auth login`")
	// ErrNoHost is returned when no default host is configured
	ErrNoHost = errors.New("failed to get default host")
)

// authDetails holds the user's active GitHub host and token.
type authDetails struct {
	Host  string
	Token string
}

// authInterface defines the methods we need from the auth package for testing
type authInterface interface {
	DefaultHost() string
	TokenForHost(host string) string
}

// realAuth implements authInterface using the actual go-gh auth package
type realAuth struct{}

func (r *realAuth) DefaultHost() string {
	// auth.DefaultHost returns (host, source) where source indicates where the host value came from
	// We only need the host value, so we ignore the source
	host, _ := auth.DefaultHost()
	return host
}

func (r *realAuth) TokenForHost(host string) string {
	// auth.TokenForHost returns (token, source) where source indicates where the token came from
	// We only need the token value, so we ignore the source
	token, _ := auth.TokenForHost(host)
	return token
}

// getAuthDetails retrieves the current user's GitHub host and OAuth token
// from the gh CLI's authentication context.
func getAuthDetails(a authInterface) (*authDetails, error) {
	host := a.DefaultHost()
	if host == "" {
		return nil, ErrNoHost
	}

	token := a.TokenForHost(host)
	if token == "" {
		return nil, ErrNotLoggedIn
	}

	return &authDetails{Host: host, Token: token}, nil
}
