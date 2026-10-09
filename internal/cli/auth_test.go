package cli_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fleveque/quantic-cli/internal/auth/authtest"
	"github.com/fleveque/quantic-cli/internal/cli"
)

// Login checks the token with Quantic, keeps it in the keyring, and the
// next commands use it; logout forgets it.
func TestLoginStatusLogout(t *testing.T) {
	fakeQuantic(t)
	keyring := &authtest.Keyring{}

	code, stdout, stderr := runWith(t, keyring, testToken+"\n", "auth", "login", "--with-token")
	if code != cli.ExitOK || stderr != "" {
		t.Fatalf("login: exit %d, stderr %q", code, stderr)
	}
	if !strings.HasPrefix(stdout, "Signed in to 127.0.0.1:") || !strings.HasSuffix(stdout, " as Demo Investor <demo-u4hNjybg@demo.quantic.invalid>.\n") {
		t.Errorf("login: stdout %q", stdout)
	}
	host := strings.TrimPrefix(os.Getenv("QUANTIC_URL"), "http://")
	if got, _ := keyring.Secret("quantic-cli", host); got != testToken {
		t.Errorf("keyring holds %q for %s, want the token", got, host)
	}

	code, stdout, _ = runWith(t, keyring, "", "auth", "status")
	if code != cli.ExitOK || !strings.Contains(stdout, "Token         in the system keyring") {
		t.Errorf("status: exit %d, stdout %q", code, stdout)
	}
	if code, _, stderr := runWith(t, keyring, "", "portfolios"); code != cli.ExitOK {
		t.Errorf("portfolios with the stored token: exit %d, %s", code, stderr)
	}

	code, stdout, stderr = runWith(t, keyring, "", "auth", "logout")
	if code != cli.ExitOK || stdout != "Signed out of "+host+".\n" ||
		stderr != "quantic: the token still works until you revoke it at http://"+host+"/settings#api-tokens-settings\n" {
		t.Errorf("logout: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if _, ok := keyring.Secret("quantic-cli", host); ok {
		t.Error("the keyring still holds the token")
	}
	if code, _, _ := runWith(t, keyring, "", "auth", "status"); code != cli.ExitNotSignedIn {
		t.Errorf("status after logout: exit %d, want %d", code, cli.ExitNotSignedIn)
	}
	code, stdout, _ = runWith(t, keyring, "", "auth", "logout")
	if code != cli.ExitOK || stdout != "No token was stored for "+host+".\n" {
		t.Errorf("logout again: exit %d, stdout %q", code, stdout)
	}
}

// A token Quantic turns down is never stored: the next command would only
// fail with it.
func TestLoginRejectsATokenQuanticDoesnt(t *testing.T) {
	requests := fakeQuantic(t)
	keyring := &authtest.Keyring{}

	code, stdout, stderr := runWith(t, keyring, "qtc_wrong", "auth", "login", "--with-token")
	if code != cli.ExitNotSignedIn || stdout != "" {
		t.Errorf("exit %d, stdout %q; want %d and nothing", code, stdout, cli.ExitNotSignedIn)
	}
	if !strings.Contains(stderr, "doesn't accept that token, so it wasn't stored. Make one at http://") {
		t.Errorf("stderr = %q", stderr)
	}
	host := strings.TrimPrefix(os.Getenv("QUANTIC_URL"), "http://")
	if _, ok := keyring.Secret("quantic-cli", host); ok {
		t.Error("the rejected token was stored")
	}
	if len(*requests) != 1 || (*requests)[0] != "GET /api/v1/me" {
		t.Errorf("requests = %q, want one GET /api/v1/me", *requests)
	}
}

// With no keyring, the token goes to a file only its owner can read, and
// login says so.
func TestLoginWithoutAKeyring(t *testing.T) {
	fakeQuantic(t)
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	keyring := &authtest.Keyring{Err: authtest.ErrNoSecretService}

	code, _, stderr := runWith(t, keyring, testToken, "auth", "login", "--with-token")
	file := filepath.Join(config, "quantic", "tokens.json")
	want := "quantic: no system keyring (" + authtest.ErrNoSecretService.Error() + ");\n" +
		"quantic: the token is in " + file + " instead, readable only by you.\n"
	if code != cli.ExitOK || stderr != want {
		t.Errorf("login: exit %d, stderr %q\nwant %q", code, stderr, want)
	}
	if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("%s: %v, %v; want mode 0600", file, info, err)
	}

	code, stdout, _ := runWith(t, keyring, "", "auth", "status")
	if code != cli.ExitOK || !strings.Contains(stdout, "Token         in a file (no system keyring)") {
		t.Errorf("status: exit %d, stdout %q", code, stdout)
	}
}

func TestLoginUsage(t *testing.T) {
	fakeQuantic(t)
	tests := []struct {
		stdin   string
		args    []string
		message string
	}{
		// A test's stdin is never a terminal, so there's no one to ask.
		{testToken, []string{"auth", "login"}, "standard input isn't a terminal; to read the token from it, add --with-token"},
		{"", []string{"auth", "login", "--with-token"}, "the token is empty"},
		{"qtc_one two", []string{"auth", "login", "--with-token"}, "a token is one word"},
	}
	for _, tt := range tests {
		code, stdout, stderr := runWith(t, &authtest.Keyring{}, tt.stdin, tt.args...)
		if code != cli.ExitUsage || stdout != "" || !strings.Contains(stderr, tt.message) {
			t.Errorf("%q with stdin %q: exit %d, stdout %q, stderr %q; want exit %d and %q",
				tt.args, tt.stdin, code, stdout, stderr, cli.ExitUsage, tt.message)
		}
	}
}

// Your own data needs a token: with none, the command says how to sign in,
// exits 4, and doesn't ask Quantic at all.
func TestNotSignedIn(t *testing.T) {
	for _, args := range [][]string{{"auth", "status"}, {"portfolios"}, {"holdings"}, {"dividends"}, {"income"}} {
		requests := fakeQuantic(t)
		code, stdout, stderr := run(t, args...)
		if code != cli.ExitNotSignedIn || stdout != "" {
			t.Errorf("%s: exit %d, stdout %q; want %d and nothing", args, code, stdout, cli.ExitNotSignedIn)
		}
		if !strings.HasSuffix(stderr, "; run: quantic auth login (or set QUANTIC_TOKEN)\n") {
			t.Errorf("%s: stderr %q", args, stderr)
		}
		if len(*requests) > 0 {
			t.Errorf("%s: asked Quantic %q with no token to send", args, *requests)
		}
	}
}

// A token Quantic rejects exits 4, and the message says which token and
// what to do: a stored one, sign in again; QUANTIC_TOKEN, make a new one.
func TestTokenRejected(t *testing.T) {
	fakeQuantic(t)
	t.Setenv("QUANTIC_TOKEN", "qtc_revoked")
	code, _, stderr := run(t, "holdings")
	if code != cli.ExitNotSignedIn || !strings.Contains(stderr, "rejected the token in QUANTIC_TOKEN; it may have been revoked. Make a new one at http://") {
		t.Errorf("from QUANTIC_TOKEN: exit %d, stderr %q", code, stderr)
	}

	t.Setenv("QUANTIC_TOKEN", "")
	keyring := &authtest.Keyring{}
	keyring.Set("quantic-cli", strings.TrimPrefix(os.Getenv("QUANTIC_URL"), "http://"), "qtc_revoked")
	code, _, stderr = runWith(t, keyring, "", "holdings")
	if code != cli.ExitNotSignedIn || !strings.Contains(stderr, "rejected the stored token; it may have been revoked. Sign in again: quantic auth login") {
		t.Errorf("from the keyring: exit %d, stderr %q", code, stderr)
	}
}

// Public commands send the token when there is one, and work without.
func TestPublicCommandsSendTheTokenIfAny(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"from":"2026-10-08","days":45,"stocks":[]}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("QUANTIC_URL", srv.URL)

	run(t, "calendar")
	t.Setenv("QUANTIC_TOKEN", testToken)
	run(t, "calendar")
	if len(got) != 2 || got[0] != "" || got[1] != "Bearer "+testToken {
		t.Errorf("Authorization = %q, want none, then the token", got)
	}
}

// The token is never printed (CLAUDE.md, design §4): whatever happens, on
// stdout or stderr, as the token or in an error about it.
func TestTheTokenIsNeverPrinted(t *testing.T) {
	const secret = "qtc_SECRET_never_print_me"
	check := func(t *testing.T, what, stdout, stderr string) {
		t.Helper()
		if strings.Contains(stdout+stderr, "SECRET_never_print_me") {
			t.Errorf("%s printed the token:\nstdout: %s\nstderr: %s", what, stdout, stderr)
		}
	}
	commands := [][]string{
		{"auth", "status"}, {"auth", "status", "--json"}, {"portfolios", "--json"}, {"holdings"},
		{"dividends", "--json"}, {"income"}, {"calendar"}, {"stock", "KO", "--json"},
	}

	t.Run("rejected", func(t *testing.T) {
		fakeQuantic(t) // accepts only qtc_test
		t.Setenv("QUANTIC_TOKEN", secret)
		for _, args := range commands {
			_, stdout, stderr := run(t, args...)
			check(t, strings.Join(args, " "), stdout, stderr)
		}
		_, stdout, stderr := runWith(t, &authtest.Keyring{}, secret, "auth", "login", "--with-token")
		check(t, "login", stdout, stderr)
	})

	t.Run("malformed", func(t *testing.T) {
		fakeQuantic(t)
		t.Setenv("QUANTIC_TOKEN", secret+" with spaces")
		_, stdout, stderr := run(t, "holdings")
		check(t, "holdings", stdout, stderr)
		t.Setenv("QUANTIC_TOKEN", "")
		_, stdout, stderr = runWith(t, &authtest.Keyring{}, secret+"\x00", "auth", "login", "--with-token")
		check(t, "login", stdout, stderr)
	})

	t.Run("failures", func(t *testing.T) {
		t.Setenv("QUANTIC_TOKEN", secret)
		for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusForbidden} {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(status)
			}))
			t.Setenv("QUANTIC_URL", srv.URL)
			for _, args := range commands {
				_, stdout, stderr := run(t, args...)
				check(t, strings.Join(args, " "), stdout, stderr)
			}
			srv.Close()
		}
		// Closed: nothing listening at all.
		for _, args := range commands {
			_, stdout, stderr := run(t, args...)
			check(t, strings.Join(args, " "), stdout, stderr)
		}
	})

	t.Run("a file others can read", func(t *testing.T) {
		fakeQuantic(t)
		config := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", config)
		os.MkdirAll(filepath.Join(config, "quantic"), 0o700)
		os.WriteFile(filepath.Join(config, "quantic", "tokens.json"), []byte(`{"x": "`+secret+`"}`), 0o644)
		keyring := &authtest.Keyring{Err: authtest.ErrNoSecretService}
		code, stdout, stderr := runWith(t, keyring, "", "holdings")
		check(t, "holdings", stdout, stderr)
		if code != cli.ExitNotSignedIn || !strings.Contains(stderr, "can be read by other users (mode 0644)") {
			t.Errorf("exit %d, stderr %q; want a refusal", code, stderr)
		}
	})
}
