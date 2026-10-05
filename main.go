// Command gh-mcp runs the bundled github-mcp-server with gh's credentials.
package main

import (
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/cli/go-gh/v2/pkg/auth"
	"github.com/shuymn/gh-mcp/internal/artifact"
	"github.com/shuymn/gh-mcp/internal/identity"
	"github.com/shuymn/gh-mcp/internal/launch"
)

//go:embed server.lock.json
var lockData []byte

// payload holds payload/server.archive in release builds. It is staged per
// platform by `go run ./tools/lock stage` and absent in plain `go build`.
//
//go:embed all:payload
var payload embed.FS

const payloadArchive = "payload/server.archive"

// launcher is everything gh-mcp needs from its environment.
type launcher struct {
	args    []string
	environ []string
	goos    string
	goarch  string
	debug   bool
	stderr  io.Writer

	lock      []byte
	archive   func() ([]byte, error)
	cacheRoot func() (string, error)
	identity  identity.Source
	exec      func(path string, args, env []string) (int, error)
}

func main() {
	os.Exit(launcher{
		args:      os.Args[1:],
		environ:   os.Environ(),
		goos:      runtime.GOOS,
		goarch:    runtime.GOARCH,
		debug:     os.Getenv("GH_MCP_DEBUG") != "",
		stderr:    os.Stderr,
		lock:      lockData,
		archive:   readPayload,
		cacheRoot: defaultCacheRoot,
		identity:  identity.Source{DefaultHost: auth.DefaultHost, TokenForHost: auth.TokenForHost},
		exec:      launch.Exec,
	}.run())
}

func (l launcher) run() int {
	code, err := l.launch()
	if err != nil {
		fmt.Fprintf(l.stderr, "gh-mcp: %v\n", err)
	}

	return code
}

func (l launcher) launch() (int, error) {
	id, err := identity.Resolve(l.identity)
	if err != nil {
		return 1, err
	}

	lock, err := artifact.ParseLock(l.lock)
	if err != nil {
		return 1, err
	}
	platform, err := lock.Platform(l.goos, l.goarch)
	if err != nil {
		return 1, err
	}

	root, err := l.cacheRoot()
	if err != nil {
		return 1, err
	}
	archive, err := l.archive()
	if err != nil {
		return 1, err
	}
	env, err := launch.Env(l.environ, l.goos, id.Host, id.Token)
	if err != nil {
		return 1, err
	}

	l.debugf("host=%s server=%s", id.Host, lock.Version)

	cache := artifact.Cache{Root: root}
	for attempt := 1; ; attempt++ {
		code, err := l.ensureAndExec(cache, platform, archive, env)
		// Another launcher may prune a long-unused digest between Ensure and
		// exec. Only a vanished file is retried, and only once; with no
		// embedded archive the retry cannot reinstall and reports ErrNotBundled.
		if attempt < 2 && errors.Is(err, fs.ErrNotExist) {
			l.debugf("server executable vanished; preparing it again: %v", err)
			continue
		}
		if errors.Is(err, artifact.ErrNotBundled) {
			return code, fmt.Errorf("%w; build with `task build`", err)
		}

		return code, err
	}
}

func (l launcher) ensureAndExec(
	cache artifact.Cache,
	platform artifact.Platform,
	archive []byte,
	env []string,
) (int, error) {
	path, err := cache.Ensure(platform, l.goos, archive)
	if err != nil {
		return 1, err
	}
	l.debugf("path=%s", path)

	return l.exec(path, launch.Args(l.args), env)
}

func (l launcher) debugf(format string, args ...any) {
	if l.debug {
		fmt.Fprintf(l.stderr, "gh-mcp: "+format+"\n", args...)
	}
}

func readPayload() ([]byte, error) {
	data, err := payload.ReadFile(payloadArchive)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read bundled archive: %w", err)
	}

	return data, nil
}

func defaultCacheRoot() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("failed to locate user cache directory: %w", err)
	}

	return filepath.Join(dir, "gh-mcp"), nil
}
