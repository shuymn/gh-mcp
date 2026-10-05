package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	errInvalidVersion   = errors.New("version must be canonical MAJOR.MINOR.PATCH")
	errUpstreamBack     = errors.New("upstream version must not decrease")
	errReleaseRegressed = errors.New("release version must not decrease")
	errUnexpectedBump   = errors.New("release version does not match the upstream update")
	errUpstreamSkipped  = errors.New("an earlier upstream release must be applied first")
	errUpstreamMissing  = errors.New("upstream version is not a published stable release")
)

// versionParts is the number of dot-separated components in a version.
const versionParts = 3

type version struct{ major, minor, patch uint64 }

// parseVersion accepts "1.2.3", or "v1.2.3" when prefixed is true.
func parseVersion(raw string, prefixed bool) (version, error) {
	value := raw
	if prefixed {
		var ok bool
		if value, ok = strings.CutPrefix(raw, "v"); !ok {
			return version{}, fmt.Errorf("%w: %q", errInvalidVersion, raw)
		}
	}

	parts := strings.Split(value, ".")
	if len(parts) != versionParts {
		return version{}, fmt.Errorf("%w: %q", errInvalidVersion, raw)
	}
	var nums [3]uint64
	for i, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return version{}, fmt.Errorf("%w: %q", errInvalidVersion, raw)
		}
		n, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return version{}, fmt.Errorf("%w: %q", errInvalidVersion, raw)
		}
		nums[i] = n
	}

	return version{nums[0], nums[1], nums[2]}, nil
}

func (v version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

func (v version) compare(o version) int {
	for _, pair := range [][2]uint64{{v.major, o.major}, {v.minor, o.minor}, {v.patch, o.patch}} {
		switch {
		case pair[0] < pair[1]:
			return -1
		case pair[0] > pair[1]:
			return 1
		}
	}

	return 0
}

// nextRelease bumps release by the component that changed between the
// upstream versions: a major upstream update bumps the major, and so on.
func nextRelease(release, fromUpstream, toUpstream version) (version, error) {
	var next version
	switch c := toUpstream.compare(fromUpstream); {
	case c < 0:
		return version{}, fmt.Errorf("%w: %s -> %s", errUpstreamBack, fromUpstream, toUpstream)
	case c == 0:
		return release, nil
	case toUpstream.major != fromUpstream.major:
		next = version{release.major + 1, 0, 0}
	case toUpstream.minor != fromUpstream.minor:
		next = version{release.major, release.minor + 1, 0}
	default:
		next = version{release.major, release.minor, release.patch + 1}
	}

	return parseVersion(next.String(), false)
}

// validateTransition checks a change from base to head. An upstream update must
// bump the release exactly as nextRelease does; otherwise the release may stay
// or increase (a gh-mcp-only release). baseUpstream is nil when the base has no
// lock, in which case only the release order is checked.
func validateTransition(
	baseRelease, headRelease version,
	baseUpstream *version,
	headUpstream version,
) error {
	if baseUpstream != nil && headUpstream != *baseUpstream {
		want, err := nextRelease(baseRelease, *baseUpstream, headUpstream)
		if err != nil {
			return err
		}
		if headRelease != want {
			return fmt.Errorf("%w: got %s, expected %s", errUnexpectedBump, headRelease, want)
		}

		return nil
	}
	if headRelease.compare(baseRelease) < 0 {
		return fmt.Errorf("%w: %s -> %s", errReleaseRegressed, baseRelease, headRelease)
	}

	return nil
}

// validateNoSkip checks that head is the published stable upstream release
// directly after base, so every upstream release gets its own gh-mcp release.
func validateNoSkip(base, head version, upstreamTags []string) error {
	found := false
	for _, tag := range upstreamTags {
		v, err := parseVersion(tag, true)
		if err != nil {
			continue
		}
		if v == head {
			found = true
		}
		if v.compare(base) > 0 && v.compare(head) < 0 {
			return fmt.Errorf("%w: %s comes before v%s", errUpstreamSkipped, tag, head)
		}
	}
	if !found {
		return fmt.Errorf("%w: v%s", errUpstreamMissing, head)
	}

	return nil
}

type publishDecision struct {
	publish, latest bool
	reason          string
}

// decidePublish decides whether release is published given the tags of
// published releases, and whether it becomes the latest release. A release that
// lands after a newer one (CI finished out of order) is still published so every
// version exists, but it must not take "latest" from the newer one.
func decidePublish(release version, publishedTags []string) publishDecision {
	d := publishDecision{publish: true, latest: true, reason: "ready to publish"}
	for _, tag := range publishedTags {
		published, err := parseVersion(tag, true)
		if err != nil {
			continue
		}
		switch published.compare(release) {
		case 0:
			return publishDecision{reason: "already published"}
		case 1:
			d.latest, d.reason = false, "publishing behind newer "+tag
		}
	}

	return d
}
