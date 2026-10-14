package cli_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fleveque/quantic-cli/internal/auth"
	"github.com/fleveque/quantic-cli/internal/auth/authtest"
	"github.com/fleveque/quantic-cli/internal/cli"
)

// TestMain keeps the tests away from the sign-in and cache of whoever runs
// them: no QUANTIC_TOKEN or QUANTIC_URL from their shell, and config and
// cache directories of the tests' own.
func TestMain(m *testing.M) {
	os.Unsetenv("QUANTIC_TOKEN")
	os.Unsetenv("QUANTIC_URL")
	dir, err := os.MkdirTemp("", "quantic-cli-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", dir)
	sharedCache = dir + "/cache"
	os.Setenv("XDG_CACHE_HOME", sharedCache)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

var release = cli.Build{Version: "v0.1.0", Commit: "abc1234", Date: "2026-10-08T10:00:00Z"}

// run runs the CLI the way main does, with buffers for the streams and an
// empty keyring of its own: never the machine's.
func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	return runWith(t, &authtest.Keyring{}, "", args...)
}

// sharedCache is XDG_CACHE_HOME until a test sets its own.
var sharedCache string

// runWith is run with a keyring and what standard input holds. Each test
// gets a cache of its own, kept across its runs: a fake Quantic's port can
// come round again in a later test, and its answers must not.
func runWith(t *testing.T, keyring auth.Keyring, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	return runDeps(t, cli.Deps{Keyring: keyring}, stdin, args...)
}

// runAt is run on a day of the test's choosing: the clock is at.
func runAt(t *testing.T, at time.Time, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	return runDeps(t, cli.Deps{Keyring: &authtest.Keyring{}, Now: func() time.Time { return at }}, "", args...)
}

func runDeps(t *testing.T, deps cli.Deps, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	if os.Getenv("XDG_CACHE_HOME") == sharedCache {
		t.Setenv("XDG_CACHE_HOME", t.TempDir())
	}
	var out, errOut bytes.Buffer
	code = cli.RunWith(args, strings.NewReader(stdin), &out, &errOut, release, deps)
	return code, out.String(), errOut.String()
}

// `version --json` is the first shape of the output contract: a program reads
// exactly these keys. A key renamed, added or dropped fails here first.
func TestVersionJSON(t *testing.T) {
	code, stdout, stderr := run(t, "version", "--json")
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, cli.ExitOK, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}

	var got map[string]string
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout isn't a JSON object of strings: %v\n%s", err, stdout)
	}
	want := map[string]string{
		"schema":   "quantic.cli/version/v1",
		"version":  "v0.1.0",
		"commit":   "abc1234",
		"date":     "2026-10-08T10:00:00Z",
		"go":       runtime.Version(),
		"platform": runtime.GOOS + "/" + runtime.GOARCH,
	}
	if !maps.Equal(got, want) {
		t.Errorf("version --json keys = %v\nwant %v", slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

// Wrong usage exits 2, says what was wrong on stderr, and leaves stdout
// empty: a script piping stdout into jq must never receive an error message.
func TestUsageErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		message string
	}{
		{name: "unknown command", args: []string{"verison"}, message: `unknown command "verison"; did you mean version?`},
		{name: "unknown flag", args: []string{"--bogus"}, message: "unknown flag: --bogus"},
		{name: "unknown flag on a command", args: []string{"version", "--bogus"}, message: "unknown flag: --bogus"},
		{name: "extra argument", args: []string{"version", "now"}, message: `unknown command "now" for "quantic version"`},
		{name: "bad duration", args: []string{"--timeout", "soon", "version"}, message: `invalid argument "soon"`},
		{name: "zero timeout", args: []string{"--timeout", "0s", "version"}, message: "--timeout must be more than zero, got 0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, tt.args...)
			if code != cli.ExitUsage {
				t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing", stdout)
			}
			if !strings.Contains(stderr, tt.message) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tt.message)
			}
			if !strings.Contains(stderr, "Run 'quantic --help' for usage.") {
				t.Errorf("stderr = %q, want the pointer to --help", stderr)
			}
			// The reason and the pointer, once each: not Cobra's own copy of
			// the error, and not the whole usage text.
			if lines := strings.Count(stderr, "\n"); lines != 2 {
				t.Errorf("stderr has %d lines, want 2:\n%s", lines, stderr)
			}
		})
	}
}

// Asking for help isn't an error: no arguments, --help and `help` all print
// the usage to stdout and exit 0.
func TestHelp(t *testing.T) {
	for _, args := range [][]string{{}, {"--help"}, {"help"}} {
		t.Run(strings.Join(append([]string{"quantic"}, args...), " "), func(t *testing.T) {
			code, stdout, stderr := run(t, args...)
			if code != cli.ExitOK {
				t.Errorf("exit code = %d, want %d; stderr: %s", code, cli.ExitOK, stderr)
			}
			if !strings.Contains(stdout, "Usage:") {
				t.Errorf("stdout = %q, want the usage", stdout)
			}
		})
	}
}
