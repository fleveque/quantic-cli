# quantic-cli

A command-line client for [Quantic Finance](https://quantic.finance): your portfolio, dividends and
income in the terminal, and in scripts and status bars through `--json`.

> **Status: milestone 1 of 9.** Only `quantic version` works so far, with the global flags and exit
> codes every command will share. The API it will read is live, documented at
> [quantic.finance/developers](https://quantic.finance/developers). Start with the [design](docs/design.md).

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
quantic version
quantic version --json
```

There are no releases yet (milestone 7), so the version is a Go pseudo-version naming the commit,
such as `v0.0.0-20261008113214-5820da20436c`.

**Global flags:** `--json` (machine-readable output, a versioned contract), `--portfolio <name>`,
`--no-cache` and `--timeout` (default `10s`). The last three are accepted now and used once there's
something to fetch.

**Exit codes**, so scripts can react without parsing messages:

| Code | Meaning |
|---|---|
| 0 | OK |
| 1 | Failed |
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

Tests use recorded or hand-written API responses, never real portfolio data.

## License

[MIT](LICENSE)
