package main

import (
	"errors"
	"fmt"

	"github.com/cli/go-gh/v2/pkg/auth"
)

// Define static errors
var (
	// ErrNotLoggedIn is returned when the user is not authenticated with GitHub
	ErrNotLoggedIn = errors.New("not logged in to GitHub. Please run `gh auth login`")
)

// authDetails holds the user's active GitHub host and token.
type authDetails struct {
	Host  string
	Token string
}

// authInterface defines the methods we need from the auth package for testing
type authInterface interface {
	DefaultHost() (string, error)
	TokenForHost(host string) (string, error)
}

// realAuth implements authInterface using the actual go-gh auth package
type realAuth struct{}

func (r *realAuth) DefaultHost() (string, error) {
	// auth.DefaultHost returns (host, source) where source indicates where the host value came from
	// We only need the host value, so we ignore the source
	host, _ := auth.DefaultHost()
	return host, nil
}

func (r *realAuth) TokenForHost(host string) (string, error) {
	// auth.TokenForHost returns (token, source) where source indicates where the token came from
	// We only need the token value, so we ignore the source
	token, _ := auth.TokenForHost(host)
	return token, nil
}

// getAuthDetails retrieves the current user's GitHub host and OAuth token
// from the gh CLI's authentication context.
func getAuthDetails() (*authDetails, error) {
	return getAuthDetailsWithAuth(&realAuth{})
}

// getAuthDetailsWithAuth is the testable version that accepts an auth interface
func getAuthDetailsWithAuth(a authInterface) (*authDetails, error) {
	host, err := a.DefaultHost()
	if err != nil {
		return nil, fmt.Errorf("failed to get default host: %w", err)
	}

	token, err := a.TokenForHost(host)
	if err != nil {
		return nil, fmt.Errorf("failed to get token for host %s: %w", host, err)
	}

	if token == "" {
		return nil, ErrNotLoggedIn
	}

	return &authDetails{Host: host, Token: token}, nil
}
