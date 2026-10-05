package launch_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/shuymn/gh-mcp/internal/launch"
)

func TestArgs(t *testing.T) {
	got := launch.Args([]string{"--read-only", "--toolsets=repos,issues"})
	want := []string{"stdio", "--read-only", "--toolsets=repos,issues"}
	if !slices.Equal(got, want) {
		t.Errorf("Args() = %v, want %v", got, want)
	}
}

func TestEnv(t *testing.T) {
	parent := []string{
		"PATH=/bin",
		"HTTPS_PROXY=http://proxy",
		"GITHUB_TOOLSETS=repos",
		"GITHUB_EXCLUDE_TOOLS=delete_file",
		"GITHUB_TOKEN=leak",
		"GITHUB_ENTERPRISE_TOKEN=leak",
		"GITHUB_PERSONAL_ACCESS_TOKEN=stale",
		"GITHUB_HOST=https://stale",
		"GITHUB_=empty-suffix",
		"GH_TOKEN=leak",
		"AWS_SECRET_ACCESS_KEY=leak",
		"malformed",
	}

	got, err := launch.Env(parent, "linux", "https://github.com", "gho_x")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"PATH=/bin",
		"HTTPS_PROXY=http://proxy",
		"GITHUB_TOOLSETS=repos",
		"GITHUB_EXCLUDE_TOOLS=delete_file",
		"GITHUB_HOST=https://github.com",
		"GITHUB_PERSONAL_ACCESS_TOKEN=gho_x",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Env() =\n%v\nwant\n%v", got, want)
	}
}

func TestEnvWindowsIgnoresCase(t *testing.T) {
	parent := []string{"Path=C:\\bin", "github_read_only=1", "Github_Token=leak"}

	got, err := launch.Env(parent, "windows", "https://github.com", "t")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Path=C:\\bin",
		"github_read_only=1",
		"GITHUB_HOST=https://github.com",
		"GITHUB_PERSONAL_ACCESS_TOKEN=t",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Env() = %v, want %v", got, want)
	}

	got, err = launch.Env(parent, "linux", "https://github.com", "t")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{
		"GITHUB_HOST=https://github.com",
		"GITHUB_PERSONAL_ACCESS_TOKEN=t",
	}; !slices.Equal(
		got,
		want,
	) {
		t.Errorf("Env() on linux = %v, want %v", got, want)
	}
}

func TestEnvRejectsUnsafeCredentials(t *testing.T) {
	for _, value := range []string{"a\nb", "a\rb", "a\x00b"} {
		if _, err := launch.Env(
			nil,
			"linux",
			"https://github.com",
			value,
		); !errors.Is(
			err,
			launch.ErrInvalidEnvValue,
		) {
			t.Errorf("token %q error = %v, want ErrInvalidEnvValue", value, err)
		}
		if _, err := launch.Env(
			nil,
			"linux",
			value,
			"t",
		); !errors.Is(
			err,
			launch.ErrInvalidEnvValue,
		) {
			t.Errorf("host %q error = %v, want ErrInvalidEnvValue", value, err)
		}
	}
}
