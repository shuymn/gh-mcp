package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	minimumArgumentCount  = 2
	nextArgumentCount     = 5
	validateArgumentCount = 6
	versionComponentCount = 3
	nextCommand           = "next"
	validateCommand       = "validate"
)

var (
	errUsage = errors.New(
		"usage: release-version next <current-release> <current-upstream> <next-upstream> | " +
			"release-version validate <current-release> <next-release> " +
			"<current-upstream> <next-upstream>",
	)
	errMissingVPrefix      = errors.New("missing v prefix")
	errUnexpectedVPrefix   = errors.New("unexpected v prefix")
	errInvalidShape        = errors.New("expected major.minor.patch")
	errInvalidComponent    = errors.New("version components must be canonical decimal integers")
	errUpstreamNotNewer    = errors.New("next upstream version must be newer")
	errComponentOverflow   = errors.New("version component overflow")
	errReleaseRegressed    = errors.New("release version must not decrease")
	errUnexpectedRelease   = errors.New("release version does not match the upstream update")
	errUpstreamVersionBack = errors.New("upstream version must not decrease")
)

type version struct {
	major uint64
	minor uint64
	patch uint64
}

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < minimumArgumentCount {
		return errUsage
	}

	switch args[1] {
	case nextCommand:
		if len(args) != nextArgumentCount {
			return errUsage
		}
		next, err := nextReleaseVersion(args[2], args[3], args[4])
		if err != nil {
			return err
		}
		fmt.Println(next)
		return nil
	case validateCommand:
		if len(args) != validateArgumentCount {
			return errUsage
		}
		return validateReleaseTransition(args[2], args[3], args[4], args[5])
	default:
		return errUsage
	}
}

func nextReleaseVersion(
	currentRelease string,
	currentUpstream string,
	nextUpstream string,
) (string, error) {
	release, err := parseVersion(currentRelease, false)
	if err != nil {
		return "", fmt.Errorf("invalid current release version %q: %w", currentRelease, err)
	}
	current, err := parseVersion(currentUpstream, true)
	if err != nil {
		return "", fmt.Errorf("invalid current upstream version %q: %w", currentUpstream, err)
	}
	next, err := parseVersion(nextUpstream, true)
	if err != nil {
		return "", fmt.Errorf("invalid next upstream version %q: %w", nextUpstream, err)
	}
	if compareVersions(next, current) <= 0 {
		return "", fmt.Errorf(
			"%w: %q is not newer than %q",
			errUpstreamNotNewer,
			nextUpstream,
			currentUpstream,
		)
	}

	switch {
	case next.major != current.major:
		release.major, err = increment(release.major)
		release.minor = 0
		release.patch = 0
	case next.minor != current.minor:
		release.minor, err = increment(release.minor)
		release.patch = 0
	default:
		release.patch, err = increment(release.patch)
	}
	if err != nil {
		return "", fmt.Errorf("cannot bump release version %q: %w", currentRelease, err)
	}

	return release.String(), nil
}

func validateReleaseTransition(
	currentRelease string,
	nextRelease string,
	currentUpstream string,
	nextUpstream string,
) error {
	current, err := parseVersion(currentRelease, false)
	if err != nil {
		return fmt.Errorf("invalid current release version %q: %w", currentRelease, err)
	}
	next, err := parseVersion(nextRelease, false)
	if err != nil {
		return fmt.Errorf("invalid next release version %q: %w", nextRelease, err)
	}
	currentMCP, err := parseVersion(currentUpstream, true)
	if err != nil {
		return fmt.Errorf("invalid current upstream version %q: %w", currentUpstream, err)
	}
	nextMCP, err := parseVersion(nextUpstream, true)
	if err != nil {
		return fmt.Errorf("invalid next upstream version %q: %w", nextUpstream, err)
	}

	upstreamComparison := compareVersions(nextMCP, currentMCP)
	if upstreamComparison < 0 {
		return fmt.Errorf(
			"%w: %q is older than %q",
			errUpstreamVersionBack,
			nextUpstream,
			currentUpstream,
		)
	}
	if upstreamComparison == 0 {
		if compareVersions(next, current) < 0 {
			return fmt.Errorf(
				"%w: %q is older than %q",
				errReleaseRegressed,
				nextRelease,
				currentRelease,
			)
		}
		return nil
	}

	expected, err := nextReleaseVersion(currentRelease, currentUpstream, nextUpstream)
	if err != nil {
		return err
	}
	if nextRelease != expected {
		return fmt.Errorf(
			"%w: got %q, expected %q",
			errUnexpectedRelease,
			nextRelease,
			expected,
		)
	}

	return nil
}

func parseVersion(raw string, requireV bool) (version, error) {
	value := raw
	if requireV {
		var found bool
		value, found = strings.CutPrefix(raw, "v")
		if !found {
			return version{}, errMissingVPrefix
		}
	} else if strings.HasPrefix(raw, "v") {
		return version{}, errUnexpectedVPrefix
	}

	parts := strings.Split(value, ".")
	if len(parts) != versionComponentCount {
		return version{}, errInvalidShape
	}

	values := make([]uint64, len(parts))
	for index, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return version{}, errInvalidComponent
		}
		parsed, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return version{}, errInvalidComponent
		}
		values[index] = parsed
	}

	return version{major: values[0], minor: values[1], patch: values[2]}, nil
}

func compareVersions(left, right version) int {
	for _, pair := range [][2]uint64{
		{left.major, right.major},
		{left.minor, right.minor},
		{left.patch, right.patch},
	} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}

	return 0
}

func increment(value uint64) (uint64, error) {
	if value == ^uint64(0) {
		return 0, errComponentOverflow
	}

	return value + 1, nil
}

func (value version) String() string {
	return fmt.Sprintf("%d.%d.%d", value.major, value.minor, value.patch)
}
