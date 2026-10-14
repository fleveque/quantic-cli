# 0005 — Releases: a tag, GoReleaser, the AUR and a Homebrew tap

**Status:** accepted · **Date:** 2026-10-14 · **Milestone:** 7

## Context

Until now quantic-cli could only be installed with `go install`. That needs Go, gives a
pseudo-version (`v0.0.0-2026…-5820da20436c`), and puts the binary in `~/go/bin`, which Omarchy's
shell doesn't have on its `PATH`. So the widget can't find it without its `command` setting
(milestone 6). Design §12 planned GoReleaser, an AUR package and Homebrew. Doing it raised these
questions:

- What starts a release, and who does it.
- Which AUR package: built from source (`quantic-cli`) or prebuilt (`quantic-cli-bin`).
- A Homebrew formula or a cask.
- Where the secrets live, and what can touch them.
- Whether a release can be built again and give the same bytes.

The name `quantic` is free: no AUR package, no Arch package and no Homebrew formula or cask is
called `quantic` or `quantic-cli` (checked 2026-10-14). That closes design open question 4.

## Decision

- **A tag starts a release.** The author pushes `vX.Y.Z` from `main`; `release.yml` runs the tests
  and then GoReleaser (`.goreleaser.yaml`). `main` is protected and every commit on it passed CI,
  so the tag only names one. Nothing is released from a branch, and nothing is released
  automatically.
- **Six targets**, built with cgo off: Linux, macOS and Windows, each on amd64 and arm64. Archives
  are `.tar.gz`, `.zip` on Windows, each with the README, the license and the shell completions. A
  `checksums.txt` (SHA-256) covers them all.
- **The version is stamped** with `-ldflags -X main.version/commit/date`, the variables
  `cmd/quantic/main.go` has had since milestone 1. The date is the commit's, not the build's.
- **Reproducible:** `-trimpath`, the commit's time on the binary (`mod_timestamp`), and the
  commit's time and root ownership on every archived file. Two builds of the same commit give the
  same checksums, so a checksum pinned by the AUR or Homebrew can be checked by rebuilding.
- **AUR: `quantic-cli-bin`**, the prebuilt binary, as `-bin` packages are. It installs
  `/usr/bin/quantic`, the completions where bash, zsh and fish look, and the license. It
  `provides` and `conflicts` with `quantic-cli`, so a source package can be added later without a
  clash. GoReleaser pushes it with the author's AUR SSH key.
- **Homebrew: a cask** in a new tap, `fleveque/homebrew-tap`: `brew install fleveque/tap/quantic`.
  A cask, not a formula, because the package is a prebuilt binary, and GoReleaser now writes casks
  for that (its formula support is deprecated). The binary isn't signed by Apple, so the cask
  removes the macOS quarantine flag after installing. Without that, macOS refuses to run it.
- **Completions come from the binary**: a GoReleaser hook runs `quantic completion bash|zsh|fish`,
  Cobra's own command, enabled now that the packages install what it writes.
- **Secrets:** `AUR_KEY` (the AUR account's private SSH key) and `HOMEBREW_TAP_TOKEN` (a
  fine-grained token that can write only to `fleveque/homebrew-tap`), as repository secrets. Only
  the release job sees them. Its GoReleaser action is pinned to a full commit, because a tag can be
  moved. CI builds a snapshot of the whole release on every PR, without publishing anything, with
  placeholder values for both.
- **GoReleaser v2.18.2 and goreleaser-action v7.2.3** are pinned. Each was the newest release at
  least two weeks old, so a broken fresh release can't break ours.

## Consequences

- Installing on Arch and Omarchy is `yay -S quantic-cli-bin`, and the binary is on the `PATH`
  Omarchy's shell has. The widget's `command` setting is then only for unusual installs.
- The author holds two credentials that a release needs. Without them, the tag's run fails at the
  AUR or Homebrew step, after the GitHub release is already published. Re-running the job finishes
  it.
- The tap's `main` isn't protected: each release pushes its cask there directly.
- `go install …@latest` keeps working, and now reports the tag's version instead of a
  pseudo-version.
- A change that breaks a platform, the archives or a package fails CI on the PR, not on the next
  tag.
