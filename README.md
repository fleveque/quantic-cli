# quantic-cli

A command-line client for [Quantic Finance](https://quantic.finance): your portfolio, dividends and
income in the terminal, and in scripts and status bars through `--json`.

> **Status: milestone 7 of 9.** Sign in with a personal API token, then read your own
> `portfolios`, `holdings`, `dividends`, `income` and `upcoming` dividends, or all at a glance with
> `status`. The public commands (`calendar`, `stock`, `search`) work signed out too. Answers are
> cached, and when Quantic can't be reached the last one is shown, marked stale. On
> [Omarchy](https://omarchy.org), [omarchy-quantic](https://github.com/fleveque/omarchy-quantic)
> puts your next dividend in the bar, from `quantic status --json`. Releases come with packages
> for Arch (and Omarchy) and Homebrew. The API it reads is
> documented at [quantic.finance/developers](https://quantic.finance/developers). Start with the
> [design](docs/design.md).

What it does:

```
quantic upcoming            # your holdings' next ex-dividend and payment dates, and amounts
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

## Install

| Where | How |
|---|---|
| Arch, Omarchy | `yay -S quantic-cli-bin` (the AUR) |
| macOS, Linux with Homebrew | `brew install fleveque/tap/quantic` |
| Anywhere else | an archive for Linux, macOS or Windows (amd64, arm64) from [Releases](https://github.com/fleveque/quantic-cli/releases), checked against its `checksums.txt` |
| With Go 1.27 or later | `go install github.com/fleveque/quantic-cli/cmd/quantic@latest` |

The packages exist from the first release, v0.1.0; before it, `go install` is the way. They
install the binary, and the tab completion for bash, zsh and fish (`quantic completion <shell>`
prints them for anything else). `go install` puts `quantic` in `~/go/bin`, which the
[Omarchy widget](https://github.com/fleveque/omarchy-quantic) doesn't look in: it needs its
`command` setting.

## Try it

```
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
quantic upcoming --days 30   # your next ex-dates and payments, and what each should pay
quantic status               # your next dividend, this month's, and the next 14 days
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

```
$ quantic status                # Quantic's demo portfolio
Next     O goes ex-dividend in 3 days (2026-10-17), 65.66 USD
October  nothing received yet; 138.60 EUR in a typical October, after withholding

The next 14 days
EX-DATE     PAY DATE    SYMBOL  NAME               SHARES   PER SHARE    EXPECTED         NET  PORTFOLIO
2026-10-17  -           O       Realty Income         200  0.2736 USD   54.72 USD   46.51 USD  Main
2026-10-17  -           O       Realty Income          40  0.2736 USD   10.94 USD    9.30 USD  Pension
2026-10-19  2026-12-10  MSFT    Microsoft           radar  0.9652 USD           -           -  -
...
```

**In the Omarchy bar:** [omarchy-quantic](https://github.com/fleveque/omarchy-quantic) shows
your next ex-date or payment as a pill, with the next 14 days in a panel. It runs `quantic status
--json`, so install and sign in to the CLI first.

`upcoming` lists stocks on your radar too (`radar`, with no amounts); `--portfolio` leaves them
out. `status` makes its three calls to Quantic at once. If one fails, it shows the rest, lists
what's missing in `"errors"`, and exits 0; only when all three fail does it exit with their
error. Money received in different currencies is summed per currency, since Quantic doesn't
convert it.

Every `--json` output names its shape and version (`"schema": "quantic.cli/income/v1"`), with
`fetched_at` and `stale`; a breaking change is a new version. Signed out, Quantic allows 60 calls a
minute per IP address; a rate-limited call is retried, then exits 5. Signed in, every command sends
the token, public ones included.

**The cache.** Answers are kept in `$XDG_CACHE_HOME/quantic` (`~/.cache/quantic`), readable only by
you, and used without asking Quantic while they're fresh: the calendar for 6 hours, holdings,
dividends, income and a stock for 15 minutes, portfolios, searches and upcoming dividends for an
hour. After that, the
CLI asks again with the answer's `ETag`, and an unchanged answer costs a `304` with no body.
`auth status` always asks. When Quantic can't be reached, the last answer is used, whatever its age:

```
$ quantic stock KO          # with the network down
quantic: can't reach quantic.finance; this is what it said at 2026-10-13 12:00 UTC, an hour ago
KO  Coca-Cola
...
```

with `"stale": true` and the original `fetched_at` in `--json`, and exit 0, so a status bar keeps
showing the last known state. `--no-cache` asks Quantic and leaves the cache alone;
`quantic cache clear` and `quantic auth logout` empty it. Two commands asking for the same thing at
once (the bar, and you in a terminal) make one request: the second waits for the first's answer.

`quantic version` says which release you have, its commit and the commit's date. A build from a
commit between releases, with `go install`, reports a Go pseudo-version naming the commit instead,
such as `v0.0.0-20261008113214-5820da20436c`.

**Global flags:** `--json` (machine-readable output, a versioned contract), `--timeout` (default
`10s`, for the whole call, retries included), `--portfolio <name>` (for `holdings`, `dividends`,
`income`, `upcoming` and `status`: one portfolio, by name or slug, in any case) and `--no-cache` (ask Quantic; don't read
or write the cache).

**Environment:** `QUANTIC_URL` points the CLI at another Quantic, such as `http://localhost:4000`
for a local server (default `https://quantic.finance`); it has its own stored token.
`QUANTIC_TOKEN` is a token to use instead of the stored one.

**Exit codes**, so scripts can react without parsing messages:

| Code | Meaning |
|---|---|
| 0 | OK, possibly from the cache (`"stale": true` says Quantic couldn't be reached) |
| 1 | Failed, e.g. a symbol Quantic doesn't track |
| 2 | Wrong usage: unknown command or flag, bad value, extra arguments, a portfolio you don't have |
| 3 | Quantic unreachable, and nothing cached |
| 4 | Not signed in, or the token was rejected (revoked, say); `status --json` still prints `"signed_in": false` |
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

**Releases** ([ADR 0005](docs/decisions/0005-releases-with-goreleaser.md)) are made by pushing a
tag (`git tag v0.1.0 && git push origin v0.1.0`) from `main`. `.github/workflows/release.yml` runs
GoReleaser, which builds every platform, publishes the GitHub release, and updates the AUR package
and the Homebrew tap. It needs two repository secrets: `AUR_KEY` and `HOMEBREW_TAP_TOKEN`.
`goreleaser release --snapshot --clean` builds the whole release locally, into `dist/`, without
publishing anything, and CI does the same on every PR.

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
