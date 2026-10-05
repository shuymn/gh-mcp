// Package launch builds the github-mcp-server command line and environment and
// hands the process over to it.
package launch

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidEnvValue is returned when a credential value cannot be passed safely.
var ErrInvalidEnvValue = errors.New("invalid server environment value")

const (
	tokenKey = "GITHUB_PERSONAL_ACCESS_TOKEN"
	hostKey  = "GITHUB_HOST"
)

// baseEnvKeys are inherited so the server can run, resolve temp dirs, and reach
// GitHub through enterprise proxies.
var baseEnvKeys = []string{
	"PATH", "HOME", "USERPROFILE", "TMPDIR", "TMP", "TEMP",
	"SHELL", "COMSPEC", "SYSTEMROOT", "WINDIR",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY",
	"http_proxy", "https_proxy", "no_proxy", "all_proxy",
	"SSL_CERT_FILE", "SSL_CERT_DIR",
}

// withheldKeys are GITHUB_* variables the server must not inherit: gh's own
// credential variables, and the two keys gh-mcp sets itself.
var withheldKeys = []string{tokenKey, hostKey, "GITHUB_TOKEN", "GITHUB_ENTERPRISE_TOKEN"}

// Args returns the server arguments for the user's gh mcp arguments.
func Args(userArgs []string) []string {
	return append([]string{"stdio"}, userArgs...)
}

// Env returns the server environment: the base allowlist and every GITHUB_*
// variable from parent, plus the host and token from gh. On Windows, variable
// names are compared case-insensitively.
func Env(parent []string, goos, host, token string) ([]string, error) {
	for key, value := range map[string]string{hostKey: host, tokenKey: token} {
		if strings.ContainsAny(value, "\x00\r\n") {
			return nil, fmt.Errorf(
				"%w: %s contains a NUL byte or line break",
				ErrInvalidEnvValue,
				key,
			)
		}
	}

	same := func(a, b string) bool { return a == b }
	if goos == "windows" {
		same = strings.EqualFold
	}
	isAny := func(key string, keys []string) bool {
		for _, k := range keys {
			if same(key, k) {
				return true
			}
		}

		return false
	}

	env := make([]string, 0, len(parent))
	for _, item := range parent {
		key, _, ok := strings.Cut(item, "=")
		if !ok || key == "" {
			continue
		}
		hasPrefix := len(key) > len("GITHUB_") && same(key[:len("GITHUB_")], "GITHUB_")
		if isAny(key, baseEnvKeys) || (hasPrefix && !isAny(key, withheldKeys)) {
			env = append(env, item)
		}
	}

	return append(env, hostKey+"="+host, tokenKey+"="+token), nil
}
