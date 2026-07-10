package main

import "testing"

func TestNextReleaseVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		currentRelease  string
		currentUpstream string
		nextUpstream    string
		want            string
	}{
		{
			name:            "patch",
			currentRelease:  "3.5.0",
			currentUpstream: "v1.5.0",
			nextUpstream:    "v1.5.1",
			want:            "3.5.1",
		},
		{
			name:            "minor resets release patch",
			currentRelease:  "3.5.4",
			currentUpstream: "v1.5.9",
			nextUpstream:    "v1.6.0",
			want:            "3.6.0",
		},
		{
			name:            "major resets release minor and patch",
			currentRelease:  "3.5.4",
			currentUpstream: "v1.5.9",
			nextUpstream:    "v2.0.0",
			want:            "4.0.0",
		},
		{
			name:            "skipped upstream versions use highest changed component",
			currentRelease:  "3.5.4",
			currentUpstream: "v1.5.9",
			nextUpstream:    "v3.2.1",
			want:            "4.0.0",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := nextReleaseVersion(
				test.currentRelease,
				test.currentUpstream,
				test.nextUpstream,
			)
			if err != nil {
				t.Fatalf("nextReleaseVersion returned error: %v", err)
			}
			if got != test.want {
				t.Fatalf("nextReleaseVersion = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNextReleaseVersionRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		currentRelease  string
		currentUpstream string
		nextUpstream    string
	}{
		{
			name:            "release prefix",
			currentRelease:  "v3.5.0",
			currentUpstream: "v1.5.0",
			nextUpstream:    "v1.5.1",
		},
		{
			name:            "missing upstream prefix",
			currentRelease:  "3.5.0",
			currentUpstream: "1.5.0",
			nextUpstream:    "v1.5.1",
		},
		{
			name:            "prerelease",
			currentRelease:  "3.5.0",
			currentUpstream: "v1.5.0",
			nextUpstream:    "v1.6.0-rc.1",
		},
		{
			name:            "leading zero",
			currentRelease:  "3.5.0",
			currentUpstream: "v1.05.0",
			nextUpstream:    "v1.6.0",
		},
		{
			name:            "same upstream version",
			currentRelease:  "3.5.0",
			currentUpstream: "v1.5.0",
			nextUpstream:    "v1.5.0",
		},
		{
			name:            "upstream downgrade",
			currentRelease:  "3.5.0",
			currentUpstream: "v1.5.0",
			nextUpstream:    "v1.4.9",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if _, err := nextReleaseVersion(
				test.currentRelease,
				test.currentUpstream,
				test.nextUpstream,
			); err == nil {
				t.Fatal("nextReleaseVersion succeeded, want error")
			}
		})
	}
}

func TestValidateReleaseTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		currentRelease  string
		nextRelease     string
		currentUpstream string
		nextUpstream    string
	}{
		{
			name:            "ordinary change keeps release version",
			currentRelease:  "3.5.0",
			nextRelease:     "3.5.0",
			currentUpstream: "v1.5.0",
			nextUpstream:    "v1.5.0",
		},
		{
			name:            "project-only release may advance",
			currentRelease:  "3.5.0",
			nextRelease:     "3.6.0",
			currentUpstream: "v1.5.0",
			nextUpstream:    "v1.5.0",
		},
		{
			name:            "upstream patch matches computed release",
			currentRelease:  "3.5.0",
			nextRelease:     "3.5.1",
			currentUpstream: "v1.5.0",
			nextUpstream:    "v1.5.1",
		},
		{
			name:            "upstream minor matches computed release",
			currentRelease:  "3.5.4",
			nextRelease:     "3.6.0",
			currentUpstream: "v1.5.9",
			nextUpstream:    "v1.6.0",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if err := validateReleaseTransition(
				test.currentRelease,
				test.nextRelease,
				test.currentUpstream,
				test.nextUpstream,
			); err != nil {
				t.Fatalf("validateReleaseTransition returned error: %v", err)
			}
		})
	}
}

func TestValidateReleaseTransitionRejectsInvalidTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		currentRelease  string
		nextRelease     string
		currentUpstream string
		nextUpstream    string
	}{
		{
			name:            "project-only release downgrade",
			currentRelease:  "3.5.0",
			nextRelease:     "3.4.9",
			currentUpstream: "v1.5.0",
			nextUpstream:    "v1.5.0",
		},
		{
			name:            "upstream update has wrong release bump",
			currentRelease:  "3.5.0",
			nextRelease:     "3.6.0",
			currentUpstream: "v1.5.0",
			nextUpstream:    "v1.5.1",
		},
		{
			name:            "upstream downgrade",
			currentRelease:  "3.5.0",
			nextRelease:     "3.5.0",
			currentUpstream: "v1.5.0",
			nextUpstream:    "v1.4.9",
		},
		{
			name:            "malformed project release",
			currentRelease:  "3.5.0",
			nextRelease:     "03.5.1",
			currentUpstream: "v1.5.0",
			nextUpstream:    "v1.5.0",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if err := validateReleaseTransition(
				test.currentRelease,
				test.nextRelease,
				test.currentUpstream,
				test.nextUpstream,
			); err == nil {
				t.Fatal("validateReleaseTransition succeeded, want error")
			}
		})
	}
}
