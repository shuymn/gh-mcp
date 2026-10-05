// Command lock maintains server.lock.json and stages bundled archives.
//
//	lock sync   [-lock FILE] [-version vX.Y.Z]  rewrite the lock from an attested upstream release
//	lock verify [-lock FILE]                    fail unless the lock matches the attested release
//	lock stage  [-platform OS/ARCH]              copy the archive to payload/server.archive
//	lock dist   -out DIR                         build gh-mcp for every locked platform
//
// stage and dist always read server.lock.json, the file the launcher embeds, so
// the staged archive matches the digests the binary verifies.
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cli/go-gh/v2"
	"github.com/shuymn/gh-mcp/internal/artifact"
)

const (
	// buildLockPath is the lock main.go embeds; build inputs always come from it.
	buildLockPath = "server.lock.json"
	payloadPath   = "payload/server.archive"
	downloadRoot  = ".cache/upstream"
	retryAttempts = 3
	retryDelay    = 2 * time.Second

	// checksumFields is the field count of a "<sha256>  <name>" checksums line.
	checksumFields = 2
)

// upstreamAssets maps each supported Go platform to its upstream archive. It is
// the only list of supported platforms; the lock and release builds follow it.
var upstreamAssets = map[string]string{
	"darwin/amd64":  "github-mcp-server_Darwin_x86_64.tar.gz",
	"darwin/arm64":  "github-mcp-server_Darwin_arm64.tar.gz",
	"linux/386":     "github-mcp-server_Linux_i386.tar.gz",
	"linux/amd64":   "github-mcp-server_Linux_x86_64.tar.gz",
	"linux/arm64":   "github-mcp-server_Linux_arm64.tar.gz",
	"windows/386":   "github-mcp-server_Windows_i386.zip",
	"windows/amd64": "github-mcp-server_Windows_x86_64.zip",
	"windows/arm64": "github-mcp-server_Windows_arm64.zip",
}

var (
	errUsage        = errors.New("usage: lock {sync|verify|stage|dist} [flags]")
	errLockOutdated = errors.New(
		"server.lock.json does not match the attested upstream release",
	)
	errChecksum       = errors.New("upstream checksum mismatch")
	errChecksumAbsent = errors.New("upstream checksum not listed")
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:])
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "lock:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	parse := func() error {
		if err := flags.Parse(args[1:]); err != nil {
			return fmt.Errorf("%w: %w", errUsage, err)
		}
		return nil
	}

	switch args[0] {
	case "sync":
		lockPath := flags.String("lock", buildLockPath, "path to the lock file")
		version := flags.String(
			"version",
			"",
			"upstream version (default: the locked version)",
		)
		if err := parse(); err != nil {
			return err
		}
		return syncLock(ctx, *lockPath, *version)
	case "verify":
		lockPath := flags.String("lock", buildLockPath, "path to the lock file")
		if err := parse(); err != nil {
			return err
		}
		return verifyLock(ctx, *lockPath)
	case "stage":
		platform := flags.String(
			"platform",
			artifact.PlatformKey(runtime.GOOS, runtime.GOARCH),
			"platform to stage",
		)
		if err := parse(); err != nil {
			return err
		}
		lock, err := readLock(buildLockPath)
		if err != nil {
			return err
		}
		return stage(ctx, lock, *platform)
	case "dist":
		outDir := flags.String("out", "", "output directory")
		if err := parse(); err != nil {
			return err
		}
		if *outDir == "" {
			return errUsage
		}
		return dist(ctx, *outDir)
	default:
		return errUsage
	}
}

func syncLock(ctx context.Context, lockPath, version string) error {
	if version == "" {
		current, err := readLock(lockPath)
		if err != nil {
			return err
		}
		version = current.Version
	}

	lock, err := attestedLock(ctx, version)
	if err != nil {
		return err
	}
	data, err := lock.Marshal()
	if err != nil {
		return err
	}
	if err := os.WriteFile(lockPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", lockPath, err)
	}
	fmt.Printf("Locked %s %s.\n", artifact.UpstreamRepository, version)

	return nil
}

func verifyLock(ctx context.Context, lockPath string) error {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", lockPath, err)
	}
	current, err := artifact.ParseLock(data)
	if err != nil {
		return err
	}

	want, err := attestedLock(ctx, current.Version)
	if err != nil {
		return err
	}
	wantData, err := want.Marshal()
	if err != nil {
		return err
	}
	if !bytes.Equal(data, wantData) {
		return fmt.Errorf("%w; run `go run ./tools/lock sync`", errLockOutdated)
	}
	fmt.Printf(
		"Verified %s against attested %s %s.\n",
		lockPath,
		artifact.UpstreamRepository,
		current.Version,
	)

	return nil
}

// attestedLock downloads every upstream archive for version, checks it against
// the release's attested checksums file, and records the executable digests.
func attestedLock(ctx context.Context, version string) (artifact.Lock, error) {
	dir := filepath.Join(downloadRoot, version)
	checksumsName := fmt.Sprintf(
		"github-mcp-server_%s_checksums.txt",
		strings.TrimPrefix(version, "v"),
	)
	if err := download(ctx, version, checksumsName, dir); err != nil {
		return artifact.Lock{}, err
	}
	checksumsPath := filepath.Join(dir, checksumsName)
	if err := retry(ctx, func() error {
		return ghRun(
			ctx,
			"release",
			"verify-asset",
			version,
			checksumsPath,
			"--repo",
			artifact.UpstreamRepository,
		)
	}); err != nil {
		return artifact.Lock{}, err
	}
	checksums, err := readChecksums(checksumsPath)
	if err != nil {
		return artifact.Lock{}, err
	}

	lock := artifact.Lock{Version: version, Platforms: map[string]artifact.Platform{}}
	for key, asset := range upstreamAssets {
		expected, ok := checksums[asset]
		if !ok {
			return artifact.Lock{}, fmt.Errorf("%w: %s", errChecksumAbsent, asset)
		}
		archive, err := fetchArchive(ctx, version, asset, expected, dir)
		if err != nil {
			return artifact.Lock{}, err
		}
		binary, err := artifact.ExtractExecutable(
			archive,
			asset,
			artifact.ExecutableName(goosOf(key)),
			io.Discard,
		)
		if err != nil {
			return artifact.Lock{}, fmt.Errorf("%s: %w", asset, err)
		}
		lock.Platforms[key] = artifact.Platform{
			Asset:         asset,
			ArchiveSHA256: expected,
			BinarySHA256:  binary,
		}
	}

	return lock, lock.Validate()
}

// stage copies the locked archive for platform to payloadPath.
func stage(ctx context.Context, lock artifact.Lock, platform string) error {
	p, ok := lock.Platforms[platform]
	if !ok {
		return fmt.Errorf("%w: %s", artifact.ErrUnsupportedPlatform, platform)
	}

	archive, err := fetchArchive(
		ctx,
		lock.Version,
		p.Asset,
		p.ArchiveSHA256,
		filepath.Join(downloadRoot, lock.Version),
	)
	if err != nil {
		return err
	}
	binary, err := artifact.ExtractExecutable(
		archive,
		p.Asset,
		artifact.ExecutableName(goosOf(platform)),
		io.Discard,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", p.Asset, err)
	}
	if binary != p.BinarySHA256 {
		return fmt.Errorf(
			"%w: %s executable expected=%s actual=%s",
			errChecksum,
			p.Asset,
			p.BinarySHA256,
			binary,
		)
	}

	if err := os.WriteFile(payloadPath, archive, 0o644); err != nil {
		return fmt.Errorf("failed to stage %s: %w", payloadPath, err)
	}
	fmt.Printf("Staged %s for %s.\n", p.Asset, platform)

	return nil
}

// dist builds dist/<os>-<arch>[.exe] for every locked platform, the layout
// cli/gh-extension-precompile uploads.
func dist(ctx context.Context, outDir string) error {
	lock, err := readLock(buildLockPath)
	if err != nil {
		return err
	}
	defer os.Remove(payloadPath)

	for _, key := range lock.PlatformKeys() {
		if err := stage(ctx, lock, key); err != nil {
			return err
		}

		goos, goarch, _ := strings.Cut(key, "/")
		out := filepath.Join(outDir, goos+"-"+goarch)
		if goos == "windows" {
			out += ".exe"
		}
		cmd := exec.CommandContext(
			ctx,
			"go",
			"build",
			"-trimpath",
			"-ldflags=-s -w",
			"-o",
			out,
			".",
		)
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to build %s: %w", key, err)
		}
	}

	return nil
}

// fetchArchive returns the archive bytes, downloading only when the cached
// copy is missing or does not match expected.
func fetchArchive(ctx context.Context, version, asset, expected, dir string) ([]byte, error) {
	path := filepath.Join(dir, asset)
	if data, err := os.ReadFile(path); err == nil && artifact.SHA256Hex(data) == expected {
		return data, nil
	}

	if err := download(ctx, version, asset, dir); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	if got := artifact.SHA256Hex(data); got != expected {
		return nil, fmt.Errorf("%w: %s expected=%s actual=%s", errChecksum, asset, expected, got)
	}

	return data, nil
}

func download(ctx context.Context, version, asset, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}

	return retry(ctx, func() error {
		return ghRun(ctx, "release", "download", version,
			"--repo", artifact.UpstreamRepository, "--pattern", asset, "--dir", dir, "--clobber")
	})
}

func ghRun(ctx context.Context, args ...string) error {
	_, stderr, err := gh.ExecContext(ctx, args...)
	if err != nil {
		return fmt.Errorf(
			"gh %s: %w: %s",
			strings.Join(args, " "),
			err,
			strings.TrimSpace(stderr.String()),
		)
	}

	return nil
}

func retry(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 1; attempt <= retryAttempts; attempt++ {
		if err = fn(); err == nil {
			return nil
		}
		if attempt == retryAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("interrupted: %w", ctx.Err())
		case <-time.After(retryDelay):
		}
	}

	return err
}

func readLock(path string) (artifact.Lock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return artifact.Lock{}, fmt.Errorf("failed to read %s: %w", path, err)
	}

	return artifact.ParseLock(data)
}

// readChecksums parses "<sha256>  <name>" lines.
func readChecksums(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	defer file.Close()

	return parseChecksums(file)
}

func parseChecksums(r io.Reader) (map[string]string, error) {
	checksums := map[string]string{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == checksumFields {
			checksums[fields[1]] = fields[0]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read checksums: %w", err)
	}

	return checksums, nil
}

func goosOf(platform string) string {
	goos, _, _ := strings.Cut(platform, "/")

	return goos
}
