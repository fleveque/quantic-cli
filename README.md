# quantic-cli

A command-line client for [Quantic Finance](https://quantic.finance): your portfolio, dividends and
income in the terminal, and in scripts and status bars through `--json`.

> **Status: design.** The CLI isn't built yet; the API it reads is live, documented at
> [quantic.finance/developers](https://quantic.finance/developers). Start with the [design](docs/design.md).

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

## Working on this

`main` is protected: every change goes through a pull request, and CI must pass before merge.

## License

[MIT](LICENSE)
