package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/fleveque/quantic-cli/internal/auth/authtest"
	"github.com/fleveque/quantic-cli/internal/cli"
)

// TestMain lets the scripts run `quantic` as a real command: testscript
// re-executes this test binary with mainForTests as the program, so a script
// sees exactly what a shell would, exit status included, without a `go build`.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"quantic": mainForTests,
	})
}

// mainForTests is main with a keyring in $WORK/keyring.json instead of the
// system's: a script's `auth login` must never reach the keyring of the
// machine running it, and the next command in the script must find what it
// stored. With TEST_KEYRING=none, the keyring fails as it does with no
// Secret Service running.
func mainForTests() {
	keyring := authtest.FileKeyring{Path: filepath.Join(os.Getenv("WORK"), "keyring.json")}
	if os.Getenv("TEST_KEYRING") == "none" {
		keyring.Err = authtest.ErrNoSecretService
	}
	build := cli.Build{Version: version, Commit: commit, Date: date}
	os.Exit(cli.RunWith(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, build, keyring))
}

// TestScripts runs every testdata/script/*.txtar: the commands, their exit
// codes, and what they print to stdout and stderr (design §11).
func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir:   "testdata/script",
		Setup: fakeQuantic,
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"exits": exits,
			"mode":  mode,
		},
	})
}

// fakeQuantic starts a Quantic for each script, at $QUANTIC_URL, that answers
// from the script's own files: a request for /api/v1/stocks/KO gets the raw
// HTTP response in quantic/api/v1/stocks/KO, status line and headers
// included, so a script says exactly what the server sends. A path with no
// file is a 404. A file named with the query as well
// (quantic/api/v1/holdings?portfolio=nope) wins over the path alone. When
// the script has a quantic/tokens file, a request with any other bearer token
// gets Quantic's 401. Every request is appended to requests.log as "GET <url>",
// followed by its Authorization header when it has one, for the script to
// check what was asked, and with which token.
func fakeQuantic(env *testscript.Env) error {
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		line := r.Method + " " + r.URL.String()
		if a := r.Header.Get("Authorization"); a != "" {
			line += " " + a
		}
		if err := appendLine(filepath.Join(env.WorkDir, "requests.log"), line); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if tokens, err := os.ReadFile(filepath.Join(env.WorkDir, "quantic", "tokens")); err == nil {
			a := r.Header.Get("Authorization")
			if a != "" && !slices.Contains(strings.Fields(string(tokens)), strings.TrimPrefix(a, "Bearer ")) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				io.WriteString(w, `{"error":{"code":"unauthorized","message":"Missing or invalid bearer token."}}`)
				return
			}
		}

		path := filepath.Join(env.WorkDir, "quantic", filepath.FromSlash(r.URL.Path))
		f, err := os.Open(path + "?" + r.URL.RawQuery)
		if err != nil {
			f, err = os.Open(path)
		}
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":{"code":"not_found","message":"Not found."}}`)
			return
		}
		defer f.Close()
		// http.ReadResponse parses what a server sends on the wire: the same
		// code an HTTP client uses, here reading from a file.
		resp, err := http.ReadResponse(bufio.NewReader(f), r)
		if err != nil {
			http.Error(w, fmt.Sprintf("bad response file for %s: %v", r.URL.Path, err), http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	env.Defer(srv.Close)
	env.Setenv("QUANTIC_URL", srv.URL)
	// os.UserConfigDir reads it first; without it, $HOME, which testscript
	// sets to a directory that doesn't exist.
	env.Setenv("XDG_CONFIG_HOME", filepath.Join(env.WorkDir, "config"))
	return nil
}

func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.WriteString(f, strings.TrimSpace(line)+"\n")
	return err
}

// exits N cmd [args...] runs cmd and fails unless it exits with status N.
// `! exec` only knows "not zero", and the exit codes are a contract (design
// §6): a 2 that turns into a 1 is a break that `! exec` can't see. Like exec,
// it keeps stdout and stderr for the stdout and stderr commands after it.
func exits(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("usage: exits N cmd [args...] (no !)")
	}
	if len(args) < 2 {
		ts.Fatalf("usage: exits N cmd [args...]")
	}
	want, err := strconv.Atoi(args[0])
	if err != nil {
		ts.Fatalf("exits: %q isn't an exit status", args[0])
	}

	got := 0
	if err := ts.Exec(args[1], args[2:]...); err != nil {
		exitErr, ok := errors.AsType[*exec.ExitError](err)
		if !ok {
			ts.Fatalf("exits: %v", err)
		}
		got = exitErr.ExitCode()
	}
	if got != want {
		ts.Fatalf("exit status %d, want %d", got, want)
	}
}

// mode FILE PERM fails unless FILE's permissions are PERM, in octal: the
// token file must be 0600 (design §4), and testscript has no stat.
func mode(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 2 {
		ts.Fatalf("usage: mode file perm")
	}
	want, err := strconv.ParseUint(args[1], 8, 32)
	if err != nil {
		ts.Fatalf("mode: %q isn't an octal permission", args[1])
	}
	info, err := os.Stat(ts.MkAbs(args[0]))
	ts.Check(err)
	if got := info.Mode().Perm(); got != os.FileMode(want) {
		ts.Fatalf("%s has mode %04o, want %04o", args[0], got, want)
	}
}
