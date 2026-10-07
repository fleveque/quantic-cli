# quantic-cli — notes for Claude Code

A read-only command-line client for Quantic Finance. Its `--json` output is a contract that other
programs build on, the first being an Omarchy bar widget in its own repository (design §9). It is also the author's way of learning Go: someone coming from Elixir and Ruby.
Start with [README.md](README.md) and [docs/design.md](docs/design.md).

## Status — 2026-10-07

- Milestone 0: the design. Nothing built. Open question 1 is decided: an `/api/v1` in Quantic
  ([ADR 0001](docs/decisions/0001-json-api-not-mcp.md)). Next is milestone S1, in `../quantic`.

## Conventions

These are quantic-agent's, on purpose: the same way of working, on a second Go project.

- **Every milestone ships code and a lesson**: `docs/lessons/NN-*.md` (NN = milestone number), written
  in the author's first person — someone coming to Go from Elixir and Ruby — plus a formatted page
  published as an artifact and linked from the lesson and `docs/lessons/README.md`. Every page uses
  the stylesheet in `docs/lessons/pages/` (see its README); reuse it, don't redesign it.
- **…and a code walkthrough** (every milestone with Go code, from milestone 1): a second artifact
  that goes through every file the milestone's PR added or changed, in reading order, as a tech lead
  mentoring someone new to Go: what each part does, why it's written that way, what the standard
  library and each dependency are doing, and why each pointer is a pointer. Excerpts are copied
  verbatim from a named commit with `docs/lessons/pages/build.py`; "try it" experiments show real
  outputs, usually by breaking the code on purpose and showing the test that catches it. Compare with
  Ruby or Elixir only where it genuinely helps. Linked from the lesson and the lessons README.
- **Milestones are worked one at a time**, each on its own branch and PR. The author says when to
  start the next one.
- **Decisions are ADRs** in `docs/decisions/NNNN-*.md`.
- **`main` is protected.** Branch → PR → CI green → the author merges. Never merge. Before pushing to
  an existing branch, check its PR isn't already merged.
- **Verify CI on the exact commit SHA** (`gh run list --json headSha`), not the PR's check list,
  which can still show the previous commit's run.
- **Gate before every commit**: `gofmt -l .` prints nothing, `go vet ./...`, `go test -race ./...`,
  and the relative-link check in `.github/workflows/ci.yml`. Until `go.mod` exists, the link check.
- **Claims are verified by running them.** Lesson outputs are real outputs. Every guarantee a test
  claims is checked by breaking the code and watching the test fail.
- **No real portfolio data** in fixtures, tests, logs or commits. Recorded responses come from a
  Quantic demo user or are written by hand.
- **The token** comes from the keyring or `QUANTIC_TOKEN`, never a flag, and never appears in output,
  logs, cache files or errors.
- **Nothing machine-specific in the binary**: `QUANTIC_URL` and flags, never a hard-coded path.
- **Questions are not decisions.** When the author asks something, answer it; ask before turning it
  into a design change.
- **Server milestones (S1, S2) are Elixir, in `../quantic`**, through that repo's own process. Its
  code is on `main`; fetch first, the local checkout can lag.
