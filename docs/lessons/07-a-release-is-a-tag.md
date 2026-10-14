# Lesson 07 — A release is a tag

**Milestone 7**: releases. Until now the only way to install quantic was `go install`, which
needs Go, reports a pseudo-version, and puts the binary somewhere Omarchy's shell doesn't look.
Now a tag makes a release: six platforms, archives, checksums, an AUR package and a Homebrew cask,
all built by GoReleaser from one file. Very little of this is new Go code. It's about what a Go
binary already is (one static file, cross-compiled anywhere), what the linker can write into it,
and what it takes for two builds of the same commit to come out byte for byte the same.

*Also readable as a [formatted page](https://claude.ai/artifact/LESSON07). The code, file by file:
[walkthrough](https://claude.ai/artifact/WALKTHROUGH07).*

---

## What landed

```
.goreleaser.yaml                  the whole release: builds, archives, checksums, AUR, Homebrew
.github/workflows/release.yml     a v* tag runs it
.github/workflows/ci.yml          every PR builds the release without publishing it
internal/cli/root.go              Cobra's completion command, switched on
docs/decisions/0005-…             why it's built this way
```

And a new repository, [fleveque/homebrew-tap](https://github.com/fleveque/homebrew-tap), where
each release writes its cask.

## Cross-compiling is a variable

In Ruby or Elixir, "build for Windows" means getting Ruby or Erlang onto Windows. In Go it's two
environment variables:

```
$ for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do printf "%s: " $t; CGO_ENABLED=0 GOOS=${t%/*} GOARCH=${t#*/} go build -o /dev/null ./cmd/quantic && echo ok; done
linux/amd64: ok
linux/arm64: ok
darwin/amd64: ok
darwin/arm64: ok
windows/amd64: ok
windows/arm64: ok
```

That worked on the first try because of choices made long before this milestone. `CGO_ENABLED=0`
has been in CI since milestone 1, so no dependency could quietly start needing a C compiler. The
keyring library talks to macOS through `/usr/bin/security` and to Windows through system calls,
not C. Milestone 4's file lock has one file per platform, picked by `//go:build`. Cross-compiling
is easy in Go only if nothing has pulled in C, and here nothing had.

## Stamping the version

`cmd/quantic/main.go` has had these since milestone 1, empty:

```go
var (
	version string
	commit  string
	date    string
)
```

The linker can set a package-level string variable from the command line:
`-ldflags "-X main.version=0.1.0"`. GoReleaser passes one `-X` for each, from the tag. Without
them, milestone 1's fallback reads what the Go toolchain recorded in the binary
(`debug.ReadBuildInfo`), which is a pseudo-version. To see the difference, I took the `-X` for
`version` out of the release config and built:

```
binary: v0.0.0-20261014172258-b761cb87c530+dirty, release: 0.0.0-SNAPSHOT-b761cb8
```

The binary said what the toolchain knew: a commit, its time, and `+dirty` because my working tree
had changes. The release said something else. CI now runs exactly that comparison on every PR:
the release binary must report the release's version.

In Elixir the version lives in `mix.exs` and is compiled in. In Ruby it's a constant in
`version.rb`. Go has neither convention. The version is whatever the linker is told, which is why
the tag can be the only place it's written.

## The same bytes twice

A release is checked by its checksums: the AUR package and the Homebrew cask both pin a SHA-256
of an archive. If building the same commit again gives different bytes, nobody can check that
the published archive came from the published source. Go is close to reproducible already, but
three things leak in:

- **Paths.** A Go binary records where its source files were, for stack traces:
  `/home/fleveque/Projects/…`. `-trimpath` records module paths instead.
- **Times.** GoReleaser sets each binary's file time to the commit's (`mod_timestamp`), and the
  stamped `date` is the commit's date, not the build's. Milestone 1 already chose that, for this.
- **The archive itself.** A `.tar.gz` records every file's owner and modification time. My first
  build had `README.md` at the time I built it, owned by `fleveque`. On CI it would be `runner`.

The archive's files now get the commit's time and `root` as owner. Building twice, two seconds
apart:

```
$ diff sums1.txt sums2.txt && echo "two builds, identical checksums"
two builds, identical checksums
$ tar tvzf dist/quantic_0.0.0-SNAPSHOT-b761cb8_linux_amd64.tar.gz | head -3
-rw-r--r-- root/root      1073 2026-10-14 19:22 LICENSE
-rw-r--r-- root/root      9312 2026-10-14 19:22 README.md
-rw-r--r-- root/root     16213 2026-10-14 19:22 completions/quantic.bash
```

19:22 is when the commit was made, not when I ran the build.

## Packages that install where things are looked for

The AUR package is `quantic-cli-bin`. By AUR convention, `-bin` means it downloads the prebuilt
binary rather than compiling. It `provides` and `conflicts` with `quantic-cli`, so a source package
could be added later without a clash. I built it for real with `makepkg`, feeding it the snapshot
archive instead of a download, and listed what it installs:

```
$ pacman -Qlp quantic-cli-bin-0.0.0_SNAPSHOT_b761cb8-1-x86_64.pkg.tar.zst | grep -v "/$"
quantic-cli-bin /usr/bin/quantic
quantic-cli-bin /usr/share/bash-completion/completions/quantic
quantic-cli-bin /usr/share/fish/vendor_completions.d/quantic.fish
quantic-cli-bin /usr/share/licenses/quantic-cli-bin/LICENSE
quantic-cli-bin /usr/share/zsh/site-functions/_quantic
```

`/usr/bin` is on the `PATH` Omarchy's shell has, so lesson 06's `command` setting stops being
needed. Each completion file goes where its shell looks for one, so tab completion works with
nothing to configure.

Homebrew gets a **cask**, not a formula. Casks used to be for macOS apps, but GoReleaser has
deprecated formulas for prebuilt binaries and writes casks instead. One detail mattered: the
binary isn't signed by Apple, so macOS quarantines it when Homebrew downloads it, and refuses to
run it. The cask removes the quarantine flag after installing. That's in the config with a comment,
because otherwise it looks like a hack.

## Completion: Cobra already wrote it

Milestone 1 switched off Cobra's `completion` command, with a note: not until there's somewhere to
install what it writes. Now there is. The release runs it as a hook, before building, and puts the
three scripts in every archive:

```
sh -c 'mkdir -p completions && for s in bash zsh fish; do go run ./cmd/quantic completion "$s" > "completions/quantic.$s"; done'
```

The scripts come from the binary itself, so they always match the flags it has. They're also
dynamic: they call back into `quantic` (a hidden `__complete` command) to ask what to suggest. So
completion improves as the commands do, without regenerating anything.

## Secrets, and who presses the button

A release needs two credentials I don't have: an SSH key for the AUR account, and a token that can
push to the tap. They're repository secrets that only the release job sees. That job's GoReleaser
action is pinned to a full commit, not to `v7.2.3`: a tag can be moved to other code, and this
step holds both keys.

And I don't push the tag. Everything up to it is a PR that CI checks, including a full snapshot
build of every platform and package. The tag is what publishes, under the author's name, to two
package managers. So the author decides when.

## Testing it

- **CI, every PR:** the whole release as a snapshot (`goreleaser release --snapshot --clean`):
  six builds, six archives, checksums, the PKGBUILD and the cask, published nowhere. Then the
  release binary's version must equal the release's.
- **`help.txtar`:** `completion` is listed, and its bash, zsh and fish scripts start the way each
  shell expects.
- **By hand:** two builds, identical checksums. `makepkg` on the generated PKGBUILD, and the
  package's file list. `ruby -c` on the cask (`Syntax OK`).

## Breaking it on purpose

| Broken | Caught by |
|---|---|
| no `-X main.version` | CI's "Release binary knows its version" (`v0.0.0-…+dirty` against `0.0.0-SNAPSHOT-…`) |
| `completion` switched off again | `help.txtar` |
| the help check reading the wrong command's output | `help.txtar`'s first version passed whatever happened; it now runs `quantic --help` first, and fails when that's swapped for `quantic version` |
| archive files at build time and owner | two snapshot builds give different checksums (checked by hand, not in CI: it would double the build) |

## What I'm taking away

- Cross-compiling Go is two environment variables, if nothing has pulled in C. Keeping cgo off in
  CI from the first milestone is what made it boring.
- The linker can write into a variable (`-X`). A Go program's version is whatever the build says,
  so the tag can be the only place it's written.
- Reproducible means checking everything that leaks the build machine into the output: paths
  (`-trimpath`), times, owners. A Go binary is nearly there; the archive around it needs help.
- Install where things are looked for: `/usr/bin`, and each shell's completion directory.
- The last step publishes under someone's name, so it belongs to them. Everything before it can be
  checked on every PR.

Next: milestone 8, `quantic dash`, a terminal dashboard with Bubble Tea.
