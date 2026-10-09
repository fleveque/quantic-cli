# quantic-cli

A command-line client for [Quantic Finance](https://quantic.finance): your portfolio, dividends and
income in the terminal, and in scripts and status bars through `--json`.

> **Status: milestone 3 of 9.** Sign in with a personal API token, then read your own
> `portfolios`, `holdings`, `dividends` and `income`. The public commands (`calendar`, `stock`,
> `search`) work signed out too. Caching and offline use come in milestone 4. The API it reads is
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

Your own data needs a personal API token. Make one in Quantic under Settings → AI and API access
(the [settings page](https://quantic.finance/settings#api-tokens-settings)), then:

```
quantic auth login           # paste the token; it's checked, then kept in the system keyring
quantic auth status          # who you're signed in as, and where the token is kept
quantic portfolios
quantic holdings --portfolio Main
quantic dividends --from 2026-01-01 --symbol KO
quantic income --years 10    # a year's income, by month, by holding, and projected
quantic auth logout          # forget it here; revoke it in Quantic's settings
```

For scripts, `quantic auth login --with-token < token.txt` reads it from standard input, and
`QUANTIC_TOKEN` is used instead of any stored token. The token is kept in the Secret Service on
Linux (GNOME Keyring, KWallet), the Keychain on macOS, the Credential Manager on Windows, one per
Quantic host. Without a keyring it goes to `$XDG_CONFIG_HOME/quantic/tokens.json`, readable only by
you, with a warning; the CLI refuses that file if others can read it. The token never appears in
output, logs or error messages.

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

```
$ quantic income --years 5      # Quantic's demo portfolio
Income      2723.10 EUR a year, 2465.54 EUR after withholding
Yield       2.99%
Growth      0.0% a year, weighted by income
In 5 years  2723.10 EUR a year, 2465.54 EUR after withholding (at today's growth)

By month, after withholding; 205.46 EUR on average
Jan  355.42 EUR  █████████████████████
Feb  177.24 EUR  ██████████
Mar   85.99 EUR  █████
...
```

Every `--json` output names its shape and version (`"schema": "quantic.cli/income/v1"`), with
`fetched_at` and `stale`; a breaking change is a new version. Signed out, Quantic allows 60 calls a
minute per IP address; a rate-limited call is retried, then exits 5. Signed in, every command sends
the token, public ones included.

There are no releases yet (milestone 7), so the version is a Go pseudo-version naming the commit,
such as `v0.0.0-20261008113214-5820da20436c`.

**Global flags:** `--json` (machine-readable output, a versioned contract), `--timeout` (default
`10s`, for the whole call, retries included), `--portfolio <name>` (for `holdings`, `dividends` and
`income`: one portfolio, by name or slug, in any case) and `--no-cache` (accepted now, used from
milestone 4).

**Environment:** `QUANTIC_URL` points the CLI at another Quantic, such as `http://localhost:4000`
for a local server (default `https://quantic.finance`); it has its own stored token.
`QUANTIC_TOKEN` is a token to use instead of the stored one.

**Exit codes**, so scripts can react without parsing messages:

| Code | Meaning |
|---|---|
| 0 | OK |
| 1 | Failed, e.g. a symbol Quantic doesn't track |
| 2 | Wrong usage: unknown command or flag, bad value, extra arguments, a portfolio you don't have |
| 3 | Quantic unreachable, and nothing cached |
| 4 | Not signed in, or the token was rejected (revoked, say) |
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
The document is used as published, with no local patches: operation names and number formats are
set on the server.

Tests use responses recorded from Quantic's public endpoints and from a demo user on a local
Quantic, or hand-written ones, never real portfolio data. No test touches the keyring of the machine
it runs on: they all pass their own to `cli.RunWith` (`internal/auth/authtest`).

## License

[MIT](LICENSE)
