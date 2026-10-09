# quantic-cli

A command-line client for [Quantic Finance](https://quantic.finance): your portfolio, dividends and
income in the terminal, and in scripts and status bars through `--json`.

> **Status: milestone 2 of 9.** The public commands work, signed out: `calendar`, `stock` and
> `search`, plus `version`. Signing in and your own portfolio come in milestone 3. The API it reads is
> documented at [quantic.finance/developers](https://quantic.finance/developers). Start with the
> [design](docs/design.md).

Where it's going:

```
quantic upcoming            # your holdings' next ex-dividend and payment dates
quantic income              # projected income, month by month
quantic calendar --days 30  # the public ex-dividend calendar, no account needed
quantic status --json       # one document for status bars and scripts
```

It only reads. Trades, portfolios and settings stay in the app.

## Why Go

Command-line tools are what Go is most often chosen for (`gh`, `kubectl`, `docker`, `tailscale`):
one static binary per platform, cross-compiled from one machine, with mature libraries for commands,
keyrings, releases and terminal interfaces. It's also how I'm learning Go, milestone by milestone,
with a [lesson](docs/lessons/README.md) and a line-by-line code walkthrough for each.

## Try it

With Go 1.27 or later:

```
go install github.com/fleveque/quantic-cli/cmd/quantic@latest
quantic calendar --days 14   # stocks going ex-dividend in the next two weeks
quantic stock KO             # price, dividend, safety and scores
quantic search coca          # find a symbol by ticker or name
quantic stock KO --json      # the same, for a script
```

```
$ quantic stock KO
KO  Coca-Cola
Consumer Defensive · Beverages - Non-Alcoholic

Price     87.005 USD, as of 2026-10-08 15:00 UTC
Yield     2.44%, 5-year average 2.99%
Dividend  quarterly, paid in Apr Jul Oct Dec, next ex-date 2026-10-17
Growth    4.5% a year over 5 years, raised 28 years in a row
Safety    safe
Scores    rating 5.0 · value 0.0 · momentum 9.0 (out of 10)
```

Every `--json` output names its shape and version (`"schema": "quantic.cli/stock/v1"`), with
`fetched_at` and `stale`; a breaking change is a new version. Signed out, Quantic allows 60 calls a
minute per IP address; a rate-limited call is retried, then exits 5.

There are no releases yet (milestone 7), so the version is a Go pseudo-version naming the commit,
such as `v0.0.0-20261008113214-5820da20436c`.

**Global flags:** `--json` (machine-readable output, a versioned contract), `--timeout` (default
`10s`, for the whole call, retries included), `--portfolio <name>` and `--no-cache`. The last two
are accepted now and used from milestones 3 and 4.

**Environment:** `QUANTIC_URL` points the CLI at another Quantic, such as `http://localhost:4000`
for a local server (default `https://quantic.finance`).

**Exit codes**, so scripts can react without parsing messages:

| Code | Meaning |
|---|---|
| 0 | OK |
| 1 | Failed, e.g. a symbol Quantic doesn't track |
| 2 | Wrong usage: unknown command or flag, bad value, extra arguments |
| 3 | Quantic unreachable, and nothing cached |
| 4 | Not signed in, or the token was rejected |
| 5 | Rate limited, after retrying |

## Working on this

`main` is protected: every change goes through a pull request, and CI must pass before merge.
Before each commit:

```
gofmt -l .            # prints nothing
go vet ./...
go test -race ./...   # unit tests, plus testscript runs of the real binary
```

`go test ./internal/cli -update` rewrites the golden files after an intended change to the output;
review their diff like code.

The API client in `internal/api/api.gen.go` is generated from `api/openapi.json`, a copy of
Quantic's OpenAPI document ([ADR 0002](docs/decisions/0002-generated-client-from-a-checked-in-spec.md)).
After updating the copy, run `go generate ./...` and commit both; CI fails if they don't match.

Tests use recorded public responses or hand-written ones, never real portfolio data.

## License

[MIT](LICENSE)
