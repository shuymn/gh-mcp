package main

import (
	"errors"
	"testing"
)

func mustVersion(t *testing.T, raw string, prefixed bool) version {
	t.Helper()

	v, err := parseVersion(raw, prefixed)
	if err != nil {
		t.Fatal(err)
	}

	return v
}

func TestParseVersion(t *testing.T) {
	for _, raw := range []string{"1.2", "1.2.3.4", "01.2.3", "1..3", "v1.2.3", "1.2.x", "4294967296.0.0"} {
		if _, err := parseVersion(raw, false); !errors.Is(err, errInvalidVersion) {
			t.Errorf("parseVersion(%q) error = %v, want errInvalidVersion", raw, err)
		}
	}
	if _, err := parseVersion("1.2.3", true); !errors.Is(err, errInvalidVersion) {
		t.Errorf("missing v prefix accepted")
	}
}

func TestNextRelease(t *testing.T) {
	tests := []struct{ from, to, want string }{
		{"v1.14.0", "v1.14.1", "3.14.1"},
		{"v1.14.0", "v1.15.0", "3.15.0"},
		{"v1.14.0", "v1.16.2", "3.15.0"},
		{"v1.14.0", "v2.0.0", "4.0.0"},
		{"v1.14.0", "v1.14.0", "3.14.0"},
	}
	for _, tt := range tests {
		got, err := nextRelease(
			mustVersion(t, "3.14.0", false),
			mustVersion(t, tt.from, true),
			mustVersion(t, tt.to, true),
		)
		if err != nil || got.String() != tt.want {
			t.Errorf(
				"nextRelease(3.14.0, %s -> %s) = %s, %v; want %s",
				tt.from,
				tt.to,
				got,
				err,
				tt.want,
			)
		}
	}

	if _, err := nextRelease(
		mustVersion(t, "3.14.0", false),
		mustVersion(t, "v1.14.0", true),
		mustVersion(t, "v1.13.9", true),
	); !errors.Is(
		err,
		errUpstreamBack,
	) {
		t.Errorf("downgrade error = %v, want errUpstreamBack", err)
	}
}

func TestNextReleaseComponentLimit(t *testing.T) {
	for _, tt := range []struct {
		release, to, want string
	}{
		{"4294967295.0.0", "v2.0.0", ""},
		{"3.4294967295.0", "v1.15.0", ""},
		{"3.14.4294967295", "v1.14.1", ""},
		{"4294967294.0.0", "v2.0.0", "4294967295.0.0"},
		{"3.4294967294.0", "v1.15.0", "3.4294967295.0"},
		{"3.14.4294967294", "v1.14.1", "3.14.4294967295"},
		{"3.14.4294967295", "v1.15.0", "3.15.0"},
		{"3.4294967295.0", "v2.0.0", "4.0.0"},
	} {
		t.Run(tt.release+"_"+tt.to, func(t *testing.T) {
			got, err := nextRelease(
				mustVersion(t, tt.release, false),
				mustVersion(t, "v1.14.0", true),
				mustVersion(t, tt.to, true),
			)
			if tt.want == "" {
				if !errors.Is(err, errInvalidVersion) {
					t.Fatalf("nextRelease error = %v, want errInvalidVersion", err)
				}
				return
			}
			if err != nil || got.String() != tt.want {
				t.Errorf("nextRelease = %s, %v; want %s", got, err, tt.want)
			}
		})
	}
}

func TestValidateTransition(t *testing.T) {
	up := func(raw string) *version { v := mustVersion(t, raw, true); return &v }
	rel := func(raw string) version { return mustVersion(t, raw, false) }

	tests := []struct {
		name             string
		baseRel, headRel string
		baseUp           *version
		headUp           string
		wantErr          error
	}{
		{"upstream patch bumps patch", "3.14.0", "3.14.1", up("v1.14.0"), "v1.14.1", nil},
		{
			"upstream update without bump",
			"3.14.0",
			"3.14.0",
			up("v1.14.0"),
			"v1.15.0",
			errUnexpectedBump,
		},
		{
			"upstream update with wrong bump",
			"3.14.0",
			"4.0.0",
			up("v1.14.0"),
			"v1.15.0",
			errUnexpectedBump,
		},
		{"upstream downgrade", "3.14.0", "3.14.0", up("v1.14.0"), "v1.13.0", errUpstreamBack},
		{"project-only release", "3.14.0", "4.0.0", up("v1.14.0"), "v1.14.0", nil},
		{"unchanged", "3.14.0", "3.14.0", up("v1.14.0"), "v1.14.0", nil},
		{"release regressed", "3.14.0", "3.13.0", up("v1.14.0"), "v1.14.0", errReleaseRegressed},
		{"base without lock", "3.14.0", "4.0.0", nil, "v1.14.0", nil},
		{"base without lock regressed", "3.14.0", "3.0.0", nil, "v1.14.0", errReleaseRegressed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTransition(
				rel(tt.baseRel),
				rel(tt.headRel),
				tt.baseUp,
				mustVersion(t, tt.headUp, true),
			)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateNoSkip(t *testing.T) {
	upstream := []string{"v1.15.0", "v1.14.1", "v1.14.0", "v1.13.0", "nightly"}
	v := func(raw string) version { return mustVersion(t, raw, true) }

	tests := []struct {
		base, head string
		wantErr    error
	}{
		{"v1.14.0", "v1.14.1", nil},
		{"v1.14.1", "v1.15.0", nil},
		{"v1.14.0", "v1.15.0", errUpstreamSkipped},
		{"v1.13.0", "v1.15.0", errUpstreamSkipped},
		{"v1.15.0", "v1.16.0", errUpstreamMissing},
	}
	for _, tt := range tests {
		if err := validateNoSkip(v(tt.base), v(tt.head), upstream); !errors.Is(err, tt.wantErr) {
			t.Errorf(
				"validateNoSkip(%s -> %s) error = %v, want %v",
				tt.base,
				tt.head,
				err,
				tt.wantErr,
			)
		}
	}
}

func TestDecidePublish(t *testing.T) {
	published := []string{"v3.13.0", "v3.14.0", "not-a-version"}

	tests := []struct {
		release                 string
		wantPublish, wantLatest bool
	}{
		{"3.15.0", true, true},
		{"3.14.0", false, false},
		{"3.13.5", true, false},
	}
	for _, tt := range tests {
		d := decidePublish(mustVersion(t, tt.release, false), published)
		if d.publish != tt.wantPublish || d.latest != tt.wantLatest {
			t.Errorf("decidePublish(%s) = %t, %t (%s), want %t, %t",
				tt.release, d.publish, d.latest, d.reason, tt.wantPublish, tt.wantLatest)
		}
	}
}
