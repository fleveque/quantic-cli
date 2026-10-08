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
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// TestMain lets the scripts run `quantic` as a real command: testscript
// re-executes this test binary with main as the program, so a script sees
// exactly what a shell would, exit status included, without a `go build`.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"quantic": main,
	})
}

// TestScripts runs every testdata/script/*.txtar: the commands, their exit
// codes, and what they print to stdout and stderr (design §11).
func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir:   "testdata/script",
		Setup: fakeQuantic,
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"exits": exits,
		},
	})
}

// fakeQuantic starts a Quantic for each script, at $QUANTIC_URL, that answers
// from the script's own files: a request for /api/v1/stocks/KO gets the raw
// HTTP response in quantic/api/v1/stocks/KO, status line and headers
// included, so a script says exactly what the server sends. A path with no
// file is a 404. Every request is appended to requests.log as "GET <url>",
// for the script to check what was asked.
func fakeQuantic(env *testscript.Env) error {
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if err := appendLine(filepath.Join(env.WorkDir, "requests.log"), r.Method+" "+r.URL.String()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		f, err := os.Open(filepath.Join(env.WorkDir, "quantic", filepath.FromSlash(r.URL.Path)))
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
