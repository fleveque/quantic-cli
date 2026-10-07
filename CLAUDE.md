# quantic-cli — notes for Claude Code

A read-only command-line client for Quantic Finance, plus an Omarchy bar widget built on its
`--json` output. It is also the author's way of learning Go: someone coming from Elixir and Ruby.
Start with [README.md](README.md) and [docs/design.md](docs/design.md).

## Status — 2026-10-07

- Milestone 0: the design. Nothing built. Open question 1 (an `/api/v1` in Quantic, or the CLI on
  MCP) must be answered by the author before milestone S1.

## Conventions

- **Every milestone ships code and a lesson**, as in `../quantic-agent`: a first-person lesson in
  `docs/lessons/NN-*.md`, a formatted page, and a line-by-line walkthrough as a tech lead mentoring
  someone new to Go. Compare with Ruby or Elixir only where it helps.
- **Decisions are ADRs** in `docs/decisions/NNNN-*.md`.
- **`main` is protected.** Branch → PR → CI green → the author merges. Never merge. Before pushing to
  an existing branch, check its PR isn't already merged.
- **Verify CI on the exact commit SHA** (`gh run list --json headSha`).
- **Gate before every commit** once Go code exists: `gofmt -l .` prints nothing, `go vet ./...`,
  `go test -race ./...`, and the relative-link check in `.github/workflows/ci.yml`.
- **No real portfolio data** in fixtures, tests, logs or commits. Recorded responses come from a
  Quantic demo user or are written by hand.
- **The token** comes from the keyring or `QUANTIC_TOKEN`, never a flag, and never appears in output,
  logs, cache files or errors.
- **Questions are not decisions.** When the author asks something, answer it; ask before turning it
  into a design change.
- Quantic's server code is in `../quantic` (`main`); fetch first, the local checkout can lag.
