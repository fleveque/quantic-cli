package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fleveque/quantic-cli/internal/auth/authtest"
	"github.com/fleveque/quantic-cli/internal/cli"
)

// Run twice in a row, each command asks Quantic once, except `auth status`,
// which always asks: it's how a script checks a token still works.
func TestWhatIsCached(t *testing.T) {
	tests := []struct {
		args []string
		asks int
	}{
		{[]string{"calendar"}, 1},
		{[]string{"stock", "KO"}, 1},
		{[]string{"search", "coca"}, 1},
		{[]string{"portfolios"}, 1},
		{[]string{"holdings"}, 1},
		{[]string{"dividends"}, 1},
		{[]string{"income"}, 1},
		{[]string{"auth", "status"}, 2},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			requests := fakeQuantic(t)
			t.Setenv("QUANTIC_TOKEN", testToken)
			for range 2 {
				if code, _, stderr := run(t, tt.args...); code != cli.ExitOK {
					t.Fatalf("exit %d: %s", code, stderr)
				}
			}
			if len(*requests) != tt.asks {
				t.Errorf("asked Quantic %d times (%q), want %d", len(*requests), *requests, tt.asks)
			}
		})
	}
}

func TestNoCache(t *testing.T) {
	requests := fakeQuantic(t)
	run(t, "calendar", "--no-cache")
	run(t, "calendar", "--no-cache")
	if len(*requests) != 2 {
		t.Errorf("asked %d times, want 2", len(*requests))
	}
	if entries, _ := os.ReadDir(filepath.Join(os.Getenv("XDG_CACHE_HOME"), "quantic")); len(entries) > 0 {
		t.Errorf("--no-cache wrote %d files", len(entries))
	}
}

// Offline, the last answer: exit 0, stale in the JSON, a note on stderr.
func TestStaleAnswer(t *testing.T) {
	srv := fakeQuanticServer(t)
	t.Setenv("QUANTIC_TOKEN", testToken)
	if code, _, _ := run(t, "auth", "status"); code != cli.ExitOK {
		t.Fatal("online: not OK")
	}

	srv.Close()
	code, stdout, stderr := run(t, "auth", "status", "--json")
	if code != cli.ExitOK {
		t.Errorf("offline: exit %d, want 0", code)
	}
	if !strings.Contains(stdout, `"stale": true`) || !strings.Contains(stdout, `"email": "demo-u4hNjybg@demo.quantic.invalid"`) {
		t.Errorf("stdout = %s", stdout)
	}
	if !strings.HasPrefix(stderr, "quantic: can't reach 127.0.0.1:") || !strings.HasSuffix(stderr, " UTC, moments ago\n") {
		t.Errorf("stderr = %q", stderr)
	}
}

// login checks the new token with Quantic itself, never with the cache: a
// cached "yes" could be for a token since revoked.
func TestLoginNeverUsesTheCache(t *testing.T) {
	srv := fakeQuanticServer(t)
	keyring := &authtest.Keyring{}
	if code, _, stderr := runWith(t, keyring, testToken, "auth", "login", "--with-token"); code != cli.ExitOK {
		t.Fatalf("login: %s", stderr)
	}
	runWith(t, keyring, "", "auth", "status") // caches /me for this token

	srv.Close()
	code, stdout, _ := runWith(t, keyring, testToken, "auth", "login", "--with-token")
	if code != cli.ExitUnreachable || stdout != "" {
		t.Errorf("login offline: exit %d, stdout %q; want %d and nothing", code, stdout, cli.ExitUnreachable)
	}
}

func TestLogoutClearsTheCache(t *testing.T) {
	fakeQuantic(t)
	keyring := &authtest.Keyring{}
	runWith(t, keyring, testToken, "auth", "login", "--with-token")
	runWith(t, keyring, "", "holdings")
	dir := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "quantic")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("nothing cached: %v", err)
	}
	runWith(t, keyring, "", "auth", "logout")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the cache is still there (%v)", err)
	}
}

func TestCacheClear(t *testing.T) {
	fakeQuantic(t)
	run(t, "calendar")
	dir := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "quantic")
	code, stdout, _ := run(t, "cache", "clear")
	if code != cli.ExitOK || stdout != "Cleared "+dir+".\n" {
		t.Errorf("exit %d, stdout %q", code, stdout)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the cache is still there (%v)", err)
	}
}
