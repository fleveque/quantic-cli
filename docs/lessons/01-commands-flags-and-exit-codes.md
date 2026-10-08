# Lesson 01 — Commands, flags, and exit codes

**Milestone 1** — the first Go code in this repository. A `quantic` binary with one command,
`version`, the global flags every later command will share, and the exit codes from
[design §6](../design.md#6-exit-codes). Nothing talks to Quantic yet. What this milestone settles is
how a command reports what happened, to a person and to a script, and how to test that from the
outside.

*Also readable as a [formatted page](https://claude.ai/artifact/XWB84F2VbghTJFbE7GaB7S). The code, file by file:
[walkthrough](https://claude.ai/artifact/PpMKhtpWX1LLqWtWpuXZDG).*

---

## What landed

```
cmd/quantic/
  main.go                 package main   wiring only: args, streams, build stamp → cli.Run
  main_test.go            package main   testscript: runs the real command from scripts
  testdata/script/*.txtar                version, usage errors, help
internal/cli/
  root.go                 package cli    the root command, global flags, Run
  exit.go                 package cli    exit codes, ExitError, ExitCode
  version.go              package cli    `quantic version`, text and --json
  *_test.go
internal/render/
  json.go                 package render JSON for programs
```

```
$ quantic version
quantic v0.0.0-20261008103030-6ff36a315023+dirty (6ff36a315023, 2026-10-08T10:30:30Z) go1.27.1 linux/amd64
$ quantic verison
quantic: unknown command "verison"; did you mean version?
Run 'quantic --help' for usage.
$ echo $?
2
```

## A command is a value

In Ruby I'd reach for Thor, where a command is a method on a class. In Elixir, `OptionParser` gives
me a keyword list and I pattern-match my way to a function. Cobra is neither. A command is a struct,
built with a literal like any other value:

```go
&cobra.Command{
	Use:   "version",
	Short: "Print the version of quantic",
	Args:  usageArgs(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, _ []string) error { … },
}
```

There's no registration DSL, no reflection over method names, and no macro. `RunE` is a field whose
value is a function. The tree is built by calling `root.AddCommand(child)`. The `E` in `RunE` means
the function returns an error, and that matters more than it looks: Cobra also has `Run`, with no
error, and a command written with `Run` has to decide for itself how to fail. With `RunE` the error
travels back up to one place that decides what failing means.

The global flags work the same way. `PersistentFlags()` on the root are inherited by every
subcommand, and `BoolVar(&opts.JSON, …)` binds a flag to a field by pointer: after parsing, the
field holds the value. Every command's constructor receives the same `*Options`, so they all read
one set of flags without any global variables.

## `Run` returns a number, and only `main` exits

```go
func main() {
	build := cli.Build{Version: version, Commit: commit, Date: date}
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, build))
}
```

That's the whole of `main`. Two reasons for keeping it so small.

**`os.Exit` doesn't unwind.** Its documentation says "deferred functions are not run". A function
that calls `os.Exit` halfway through skips every `defer` above it, and once there's a cache lock to
release (milestone 4), that's a bug. So nothing below `main` exits. Errors are returned, and `main`
turns the last one into a status.

**`os.Exit` can't be tested.** It ends the test binary too. `Run` takes the arguments, the three
streams and the build stamp as parameters and returns an `int`, so a test can call it as often as it
likes, with `bytes.Buffer`s where the terminal would be.

## Exit codes are part of the output

The widget (design §9) won't parse error messages. It reads the exit status: 4 means "log in
again", 3 means "offline", 2 means the widget itself is wrong. Those meanings are a contract like the
JSON is, so they're constants with a comment each, and an error carries its code with it:

```go
type ExitError struct {
	Code int
	Err  error
}

func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	if exitErr, ok := errors.AsType[*ExitError](err); ok {
		return exitErr.Code
	}
	return ExitFailed
}
```

`errors.AsType` is new in Go 1.26: the generic form of `errors.As`, which returns the match instead
of filling in a variable you pass by pointer. It walks the chain of wrapped errors. That's the part
that matters, because errors get wrapped on their way up: a command will say
`fmt.Errorf("holdings: %w", err)` to add context, and the `ExitError` is then one level down.

My first instinct was a type assertion, `err.(*ExitError)`, which only looks at the outermost
error. I broke `ExitCode` that way on purpose, and the test caught it:

```
--- FAIL: TestExitCode (0.00s)
    --- FAIL: TestExitCode/wrapped_exit_error (0.00s)
        exit_test.go:33: ExitCode(holdings: dial tcp) = 1, want 3
```

An offline widget would have told me "failed" instead of "offline". `ExitError` also has an
`Unwrap` method, so the label doesn't hide what's underneath: `errors.Is(err, fs.ErrNotExist)` still
finds a missing file inside an `ExitError`.

## Cobra's defaults are for people at a terminal

Out of the box, Cobra prints an error itself, prints the full usage text after it, and returns the
error to me as well. That's friendly in a terminal and wrong for a script. Turning off only the
first of those (`SilenceErrors: false`) shows the problem:

```
Error: unknown command "verison"; did you mean version?
quantic: unknown command "verison"; did you mean version?
Run 'quantic --help' for usage.
```

Cobra's copy and mine. With `SilenceErrors` and `SilenceUsage` both on, `Run` prints one line and a
pointer to `--help`, on stderr, and stdout stays empty. A script piping stdout into `jq` must never
get an error message to parse. A test now counts the lines on stderr: two, exactly.

Then there's getting usage mistakes to exit 2 rather than 1. Cobra has three separate kinds, and each
needs its own hook:

| Mistake | Where Cobra handles it | How it becomes exit 2 |
|---|---|---|
| Unknown flag, bad value (`--timeout soon`) | the flag parser | `SetFlagErrorFunc`, on the root, inherited by every command |
| Wrong number of arguments (`version now`) | the command's `Args` check | `usageArgs` wraps the check in an `ExitError` |
| Unknown command (`verison`) | Cobra's own lookup, unless the root has `Args` | the root takes `ArbitraryArgs`, and its `RunE` reports the unknown command |

The third one surprised me. Cobra only produces its "unknown command" error when the root command
has no `Args` set. Give the root `cobra.ArbitraryArgs` and the unknown word arrives in the root's
`RunE` as an ordinary argument, where I can return a usage error. With no arguments, the same `RunE`
prints the help, and exits 0: asking for help isn't a mistake.

I lost Cobra's "Did you mean this?" in the process and added it back with `cmd.SuggestionsFor`. It
returned nothing. Cobra applies its default edit distance of 2 inside its own error path, but
`SuggestionsFor` reads `SuggestionsMinimumDistance` as it is, and it starts at 0. One line,
`root.SuggestionsMinimumDistance = 2`, and a test that would fail without it.

## Writers, not `os.Stdout`

Every command writes to `cmd.OutOrStdout()`, never to `os.Stdout`. `Run` calls `root.SetOut(stdout)`,
so in the binary that's the real terminal and in a test it's a `bytes.Buffer`. The parameter type is
`io.Writer`, an interface with one method:

```go
type Writer interface {
	Write(p []byte) (n int, err error)
}
```

Anything with that method is a writer: a file, a buffer, a network connection, a gzip stream. Nothing
declares that it implements `io.Writer`; it just has the method. Coming from Elixir, it's the closest
thing to a behaviour that nobody has to adopt. Coming from Ruby, it's duck typing that the compiler
checks.

## What the toolchain writes into the binary

`quantic version` has three sources, and I didn't expect the second one:

```
$ go build -ldflags "-X main.version=v0.1.0 -X main.commit=abc1234 -X main.date=2026-10-08T10:00:00Z"
quantic v0.1.0 (abc1234, 2026-10-08T10:00:00Z) go1.27.1 linux/amd64

$ go build                      # in a git checkout with uncommitted changes
quantic v0.0.0-20261008103030-6ff36a315023+dirty (6ff36a315023, 2026-10-08T10:30:30Z) go1.27.1 linux/amd64

$ go build -buildvcs=false
quantic dev go1.27.1 linux/amd64
```

**`-X` sets a string variable at link time.** The release (milestone 7) will pass the version, commit
and date this way. The linker only does it for a package-level `string` variable that's uninitialised
or set to a constant string, which is why `main.go` has `var version string` and not a function
call.

**A plain `go build` stamps more than I thought.** I'd read that a local build reports its version
as `(devel)`. Not any more: inside a git checkout the toolchain records a pseudo-version made of the
commit's time and hash, `+dirty` included when there are uncommitted changes, plus the commit and its
time as separate `vcs.*` settings. `runtime/debug.ReadBuildInfo` reads them back. `(devel)` is now
what you get when there's no version control to read, such as `-buildvcs=false` or a source tarball.

**`date` is the commit's date.** `vcs.time` is when the commit was made, not when the binary was
built. My first draft printed "built 2026-10-08…", which would have been wrong for every build but
the first. Now `date` means the commit date everywhere, and the release will stamp GoReleaser's
`{{.CommitDate}}` rather than the build time, so building the same commit twice gives the same binary.

## JSON that's polite in a terminal

`version --json` is the first shape of the output contract, `quantic.cli/version/v1`. `render.JSON`
writes it indented, with `json.Encoder`, which adds the trailing newline every Unix tool expects. It
also escapes HTML by default, which I only found by testing a name with an ampersand in it:

```
--- FAIL: TestJSON (0.00s)
    json_test.go:23: JSON() wrote
        {
          "name": "AT&T <Inc>",
```

That's valid JSON, and right if the output were going inside an HTML page. In a terminal it's just
unreadable. `enc.SetEscapeHTML(false)` turns it off.

## testscript: testing the command a person actually types

Unit tests call `cli.Run` with buffers. Design §11 also asks for end-to-end runs of the real binary,
and `testscript` does that without a `go build` step. `TestMain` hands it a map from command names
to functions:

```go
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"quantic": main,
	})
}
```

When a script says `exec quantic version`, testscript runs the test binary itself again, and that
second process runs `main` instead of the tests. So the script sees a real process with a real exit
status, stdout and stderr, exactly what a shell would see. The scripts are `.txtar` files, readable
on their own:

```
exits 2 quantic verison
! stdout .
stderr '^quantic: unknown command "verison"; did you mean version\?$'
```

`exits` isn't built in. The Go project's own scripts have a `status` command, but go-internal's
testscript doesn't, and `! exec` only checks that a command failed, not how. A usage error that
quietly became exit 1 would pass every `! exec`. So `main_test.go` adds `exits N cmd…`, about twenty
lines on top of `ts.Exec`, and when I changed `ExitUsage` to 1 it said so:

```
> exits 2 quantic verison
FAIL: testdata/script/usage.txtar:4: exit status 1, want 2
```

## Breaking it on purpose

Every guarantee was checked by breaking the code and watching a test fail. Eleven breaks, eleven
failures. One of them didn't count at first: replacing `errors.AsType` with a type assertion left
the `errors` import unused, and Go refuses to compile with an unused import, so the "failure" was a
build error, not the test. Once I removed the import as well, the test failed the way it should.

| Broken | Caught by |
|---|---|
| `ExitCode` uses a type assertion | `TestExitCode/wrapped_exit_error` |
| HTML escaping left on | `TestJSON` |
| `SilenceErrors` or `SilenceUsage` off | `TestUsageErrors` (line count), `usage.txtar` |
| no `SetFlagErrorFunc` | `TestUsageErrors/unknown_flag`, `usage.txtar` |
| root without `Args` | `TestUsageErrors/unknown_command`, `usage.txtar` |
| `version` without `usageArgs` | `TestUsageErrors/extra_argument`, `usage.txtar` |
| `--timeout 0` accepted | `TestUsageErrors/zero_timeout`, `usage.txtar` |
| toolchain stamp overrides the linker's | `TestBuildWithFallback`, `TestVersionJSON` |
| no `SuggestionsMinimumDistance` | `TestUsageErrors/unknown_command`, `usage.txtar` |
| `ExitUsage` = 1 | `usage.txtar`, through `exits` |

## What I'm taking away

- A Cobra command is a struct with function-valued fields. `RunE` over `Run`, always: errors go up
  to one place.
- Only `main` calls `os.Exit`. Everything below returns errors, so `defer` works and tests can run it.
- An exit code is an error with a label. `errors.AsType` finds the label through any wrapping; a
  type assertion only looks at the outside.
- Cobra's defaults are for interactive use. A tool whose output is a contract has to turn some of
  them off, and say which mistakes are usage.
- A plain `go build` already knows the commit. `-X` is for what the release wants to say instead.
- testscript runs the real command in-process, and `exits` makes the exit code part of every script.

Next, milestone 2: a client generated from Quantic's OpenAPI document, and the first commands that
talk to it.
