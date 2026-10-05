// Command release derives and checks gh-mcp release versions.
//
//	release next     -base-ref REF [-dir DIR] [-write]  print (and write) the VERSION for the lock change
//	release validate -base-ref REF [-dir DIR]           check VERSION and the lock against REF
//	release plan     -target-sha SHA                    decide whether the Release job publishes
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cli/go-gh/v2"
	"github.com/shuymn/gh-mcp/internal/artifact"
)

var (
	errUsage        = errors.New("usage: release {next|validate|plan} [flags]")
	errTagMismatch  = errors.New("release tag points at a different commit")
	errMissingEnv   = errors.New("required environment variable is not set")
	zeroSHA         = regexp.MustCompile(`^0+$`)
	errUnknownField = errors.New("unexpected field in gh output")
	errNoBaseLock   = errors.New("base commit has no server.lock.json to compare against")
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}

	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	baseRef := flags.String("base-ref", "", "git revision of the base commit")
	dir := flags.String("dir", ".", "repository checkout to read")
	write := flags.Bool("write", false, "write the computed VERSION")
	targetSHA := flags.String("target-sha", "", "commit the Release job builds")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}

	switch args[0] {
	case "next":
		if *baseRef == "" {
			return errUsage
		}
		return next(ctx, *dir, *baseRef, *write, stdout)
	case "validate":
		if *baseRef == "" {
			return errUsage
		}
		return validate(ctx, *dir, *baseRef, stdout)
	case "plan":
		if *targetSHA == "" {
			return errUsage
		}
		return plan(ctx, *targetSHA, stdout)
	default:
		return errUsage
	}
}

func next(ctx context.Context, dir, baseRef string, write bool, stdout io.Writer) error {
	base, err := readBase(ctx, dir, baseRef)
	if err != nil {
		return err
	}
	headUpstream, err := readHeadUpstream(dir)
	if err != nil {
		return err
	}
	if base.upstream == nil {
		return fmt.Errorf("%w: %s", errNoBaseLock, baseRef)
	}

	release, err := nextRelease(base.release, *base.upstream, headUpstream)
	if err != nil {
		return err
	}
	if write {
		if err := os.WriteFile(
			filepath.Join(dir, "VERSION"),
			[]byte(release.String()+"\n"),
			0o644,
		); err != nil {
			return fmt.Errorf("failed to write VERSION: %w", err)
		}
	}
	fmt.Fprintln(stdout, release)

	return nil
}

func validate(ctx context.Context, dir, baseRef string, stdout io.Writer) error {
	if zeroSHA.MatchString(baseRef) {
		fmt.Fprintln(stdout, "No base commit; skipping release transition check.")
		return nil
	}

	base, err := readBase(ctx, dir, baseRef)
	if err != nil {
		return err
	}
	headRelease, err := readRelease(filepath.Join(dir, "VERSION"))
	if err != nil {
		return err
	}
	headUpstream, err := readHeadUpstream(dir)
	if err != nil {
		return err
	}

	if err := validateTransition(
		base.release,
		headRelease,
		base.upstream,
		headUpstream,
	); err != nil {
		return err
	}
	if base.upstream != nil && headUpstream != *base.upstream {
		tags, err := ghLines(ctx, "api", "--paginate",
			"repos/"+artifact.UpstreamRepository+"/releases?per_page=100",
			"--jq", `.[] | select(.draft == false and .prerelease == false) | .tag_name`)
		if err != nil {
			return err
		}
		if err := validateNoSkip(*base.upstream, headUpstream, tags); err != nil {
			return err
		}
	}
	fmt.Fprintf(
		stdout,
		"Release %s with upstream v%s is a valid successor of %s.\n",
		headRelease,
		headUpstream,
		baseRef,
	)

	return nil
}

type baseState struct {
	release  version
	upstream *version
}

func readBase(ctx context.Context, dir, ref string) (baseState, error) {
	raw, err := gitShow(ctx, dir, ref, "VERSION")
	if err != nil {
		return baseState{}, err
	}
	release, err := parseVersion(strings.TrimSpace(raw), false)
	if err != nil {
		return baseState{}, err
	}

	// Commits before the lock was introduced carry no comparable upstream version.
	if !gitHas(ctx, dir, ref, "server.lock.json") {
		return baseState{release: release}, nil
	}
	lockData, err := gitShow(ctx, dir, ref, "server.lock.json")
	if err != nil {
		return baseState{}, err
	}
	upstream, err := lockUpstream([]byte(lockData))
	if err != nil {
		return baseState{}, err
	}

	return baseState{release: release, upstream: &upstream}, nil
}

func readRelease(path string) (version, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return version{}, fmt.Errorf("failed to read VERSION: %w", err)
	}

	return parseVersion(strings.TrimSpace(string(data)), false)
}

func readHeadUpstream(dir string) (version, error) {
	data, err := os.ReadFile(filepath.Join(dir, "server.lock.json"))
	if err != nil {
		return version{}, fmt.Errorf("failed to read server.lock.json: %w", err)
	}

	return lockUpstream(data)
}

func lockUpstream(data []byte) (version, error) {
	lock, err := artifact.ParseLock(data)
	if err != nil {
		return version{}, err
	}

	return parseVersion(lock.Version, true)
}

func gitHas(ctx context.Context, dir, ref, path string) bool {
	return exec.CommandContext(ctx, "git", "-C", dir, "cat-file", "-e", ref+":"+path).Run() == nil
}

func gitShow(ctx context.Context, dir, ref, path string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "show", ref+":"+path).Output()
	if err != nil {
		return "", fmt.Errorf("git show %s:%s: %w", ref, path, err)
	}

	return string(out), nil
}

// plan writes tag, create_tag, publish, and latest to $GITHUB_OUTPUT. It skips
// a release that already exists and keeps "latest" on the newest version.
func plan(ctx context.Context, targetSHA string, stdout io.Writer) error {
	repo, outputPath := os.Getenv("GITHUB_REPOSITORY"), os.Getenv("GITHUB_OUTPUT")
	if repo == "" || outputPath == "" {
		return fmt.Errorf("%w: GITHUB_REPOSITORY and GITHUB_OUTPUT", errMissingEnv)
	}

	release, err := readRelease("VERSION")
	if err != nil {
		return err
	}
	tag := "v" + release.String()

	releases, err := ghLines(ctx, "api", "--paginate", "repos/"+repo+"/releases?per_page=100",
		"--jq", `.[] | select(.draft == false) | .tag_name`)
	if err != nil {
		return err
	}
	decision := decidePublish(release, releases)

	createTag := false
	if decision.publish {
		target, err := tagTarget(ctx, repo, tag)
		if err != nil {
			return err
		}
		switch target {
		case "":
			createTag = true
		case targetSHA:
		default:
			return fmt.Errorf("%w: %s -> %s, expected %s", errTagMismatch, tag, target, targetSHA)
		}
	}

	fmt.Fprintf(stdout, "%s: %s\n", tag, decision.reason)
	out, err := os.OpenFile(outputPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open GITHUB_OUTPUT: %w", err)
	}
	defer out.Close()
	if _, err := fmt.Fprintf(
		out,
		"tag=%s\ncreate_tag=%t\npublish=%t\nlatest=%t\n",
		tag,
		createTag,
		decision.publish,
		decision.latest,
	); err != nil {
		return fmt.Errorf("failed to write GITHUB_OUTPUT: %w", err)
	}

	return nil
}

// tagTarget returns the commit tag points at, or "" when the tag does not exist.
func tagTarget(ctx context.Context, repo, tag string) (string, error) {
	lines, err := ghLines(
		ctx,
		"api",
		"repos/"+repo+"/git/matching-refs/tags/"+tag,
		"--jq",
		fmt.Sprintf(`.[] | select(.ref == "refs/tags/%s") | .object.type + " " + .object.sha`, tag),
	)
	if err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return "", nil
	}

	kind, sha, _ := strings.Cut(lines[0], " ")
	switch kind {
	case "commit":
		return sha, nil
	case "tag":
		peeled, err := ghLines(ctx, "api", "repos/"+repo+"/git/tags/"+sha, "--jq", ".object.sha")
		if err != nil {
			return "", err
		}
		if len(peeled) == 0 {
			return "", fmt.Errorf("%w: tag %s has no target", errUnknownField, tag)
		}
		return peeled[0], nil
	default:
		return "", fmt.Errorf("%w: tag object type %q", errUnknownField, kind)
	}
}

func ghLines(ctx context.Context, args ...string) ([]string, error) {
	stdout, stderr, err := gh.ExecContext(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf(
			"gh %s: %w: %s",
			strings.Join(args, " "),
			err,
			strings.TrimSpace(stderr.String()),
		)
	}

	var lines []string
	scanner := bufio.NewScanner(&stdout)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			lines = append(lines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read gh output: %w", err)
	}

	return lines, nil
}
