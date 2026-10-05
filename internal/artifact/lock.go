// Package artifact resolves the pinned github-mcp-server executable for the
// current platform and materializes it in a content-addressed cache.
package artifact

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
)

// UpstreamRepository is the GitHub repository that publishes the server archives.
const UpstreamRepository = "github/github-mcp-server"

var (
	// ErrInvalidLock is returned when server.lock.json is malformed.
	ErrInvalidLock = errors.New("invalid server lock")
	// ErrUnsupportedPlatform is returned when the lock has no entry for a platform.
	ErrUnsupportedPlatform = errors.New("no bundled github-mcp-server for platform")

	versionPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	sha256Pattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Lock pins one upstream release and the digests of each supported platform.
type Lock struct {
	Version   string              `json:"version"`
	Platforms map[string]Platform `json:"platforms"`
}

// Platform pins the upstream archive and the executable it contains.
type Platform struct {
	Asset         string `json:"asset"`
	ArchiveSHA256 string `json:"archive_sha256"`
	BinarySHA256  string `json:"binary_sha256"`
}

// ParseLock decodes and validates server.lock.json.
func ParseLock(data []byte) (Lock, error) {
	var lock Lock
	if err := json.Unmarshal(data, &lock); err != nil {
		return Lock{}, fmt.Errorf("%w: %w", ErrInvalidLock, err)
	}
	if err := lock.Validate(); err != nil {
		return Lock{}, err
	}

	return lock, nil
}

// Validate reports whether every field has its canonical shape.
func (l Lock) Validate() error {
	if !versionPattern.MatchString(l.Version) {
		return fmt.Errorf("%w: version %q is not vMAJOR.MINOR.PATCH", ErrInvalidLock, l.Version)
	}
	if len(l.Platforms) == 0 {
		return fmt.Errorf("%w: no platforms", ErrInvalidLock)
	}
	for key, p := range l.Platforms {
		if p.Asset == "" {
			return fmt.Errorf("%w: %s has no asset", ErrInvalidLock, key)
		}
		if !sha256Pattern.MatchString(p.ArchiveSHA256) ||
			!sha256Pattern.MatchString(p.BinarySHA256) {
			return fmt.Errorf("%w: %s has a malformed digest", ErrInvalidLock, key)
		}
	}

	return nil
}

// Platform returns the entry for goos/goarch.
func (l Lock) Platform(goos, goarch string) (Platform, error) {
	p, ok := l.Platforms[PlatformKey(goos, goarch)]
	if !ok {
		return Platform{}, fmt.Errorf("%w: %s", ErrUnsupportedPlatform, PlatformKey(goos, goarch))
	}

	return p, nil
}

// PlatformKeys returns the platform keys in a stable order.
func (l Lock) PlatformKeys() []string {
	keys := make([]string, 0, len(l.Platforms))
	for key := range l.Platforms {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	return keys
}

// Marshal encodes the lock in its canonical file form.
func (l Lock) Marshal() ([]byte, error) {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to encode server lock: %w", err)
	}

	return append(data, '\n'), nil
}

// PlatformKey formats a Go platform as used in the lock.
func PlatformKey(goos, goarch string) string {
	return goos + "/" + goarch
}

// ExecutableName returns the server executable name inside upstream archives.
func ExecutableName(goos string) string {
	if goos == "windows" {
		return "github-mcp-server.exe"
	}

	return "github-mcp-server"
}
