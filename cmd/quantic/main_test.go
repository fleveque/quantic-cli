package main

import (
	"errors"
	"os/exec"
	"strconv"
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
		Dir: "testdata/script",
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"exits": exits,
		},
	})
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
