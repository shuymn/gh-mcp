package main

import "testing"

func TestNormalizeEngine(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "empty -> auto", in: "", want: engineAuto},
		{name: "auto", in: "auto", want: engineAuto},
		{name: "docker", in: "docker", want: engineDocker},
		{name: "podman", in: "podman", want: enginePodman},
		{name: "whitespace trimmed", in: "  PODMAN ", want: enginePodman},
		{name: "invalid", in: "nerdctl", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeEngine(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (value=%q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeEngine(%q)=%q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseOptionsPrecedence(t *testing.T) {
	t.Setenv("GH_MCP_ENGINE", "docker")
	opts, err := parseOptions([]string{"--engine=podman"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.engine != enginePodman {
		t.Fatalf("engine=%q, want %q", opts.engine, enginePodman)
	}
}

func TestParseOptions_DefaultFromEnv(t *testing.T) {
	t.Setenv("GH_MCP_ENGINE", "podman")
	opts, err := parseOptions(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.engine != enginePodman {
		t.Fatalf("engine=%q, want %q", opts.engine, enginePodman)
	}
}

func TestParseOptions_InvalidEngine(t *testing.T) {
	_, err := parseOptions([]string{"--engine=invalid"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
