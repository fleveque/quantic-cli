// Command quantic is a read-only command-line client for Quantic Finance.
//
// This file is wiring only (design §10): it hands the arguments, the three
// standard streams and the build stamp to internal/cli, and turns the result
// into the process's exit status. Everything that can be tested lives there.
package main

import (
	"os"

	"github.com/fleveque/quantic-cli/internal/cli"
)

// Set at build time by the release tooling (milestone 7), with
// -ldflags "-X main.version=… -X main.commit=… -X main.date=…": the names
// GoReleaser fills by default. date is the commit's date ({{.CommitDate}}),
// not the build's, so building the same commit twice gives the same binary,
// and it means the same as the vcs.time fallback. Left empty, internal/cli
// falls back to what the Go toolchain recorded in the binary.
var (
	version string
	commit  string
	date    string
)

func main() {
	build := cli.Build{Version: version, Commit: commit, Date: date}
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, build))
}
