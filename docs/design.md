# quantic-cli — design

**Status:** draft, 2026-10-07. Nothing built yet.

A command-line client for [Quantic Finance](https://quantic.finance): your portfolio, dividends and
income in the terminal and in scripts, plus an Omarchy bar widget built on it. It is also where I
learn Go. Unlike quantic-agent, this is a kind of program Go is the usual choice for.

---

## 1. Why this exists, and why Go

**Who it's for:** Quantic users who live in a terminal, and scripts and status bars that want
Quantic's data without a browser. The first consumer is the [Omarchy widget](#9-the-omarchy-widget).

**Why Go.** Command-line tools are Go's home ground: `gh`, `kubectl`, `docker`, `terraform`,
`hugo`, `tailscale`. Each release is a single static binary per platform, cross-compiled from one
machine, and the ecosystem for this kind of tool is mature: Cobra, GoReleaser, keyring access, and
Charm's terminal UI libraries. Elixir needs its runtime installed to run a CLI; Python CLIs are
awkward to distribute. This is the honest reason, and it doesn't rely on "I want to learn Go"
(that's a bonus).

**Non-goals.**
- **No writes.** It reads; it never records a trade, edits a portfolio or changes settings. Writes
  stay in the app, where they're validated and reversible.
- **No AI.** It shows Quantic's data. Asking questions about it is what the MCP server is for.
- **Not a replacement for the app.** It shows the few things worth a glance or a script.

---

## 2. Data source: a JSON API in Quantic

**Proposed:** Quantic gets a read-only, versioned JSON API, `/api/v1`, and the CLI talks only to it.
This is a server change, in Elixir, before the CLI's authenticated commands (milestone S1).

```
             ┌──────────────── Quantic (Phoenix) ────────────────┐
quantic CLI ─┼─▶ /api/v1/*  (JSON controllers) ─┐                 │
Omarchy      │                                  ├─▶ contexts      │
widget ──CLI─┤   /mcp       (MCP tools) ────────┘   Portfolio,    │
AI clients ──┼─▶                                    MarketData,   │
             │                                      Export …      │
             └───────────────────────────────────────────────────┘
```

**The API and MCP are siblings, not layers.** Both call the same context functions, and share the
serializers that already exist for MCP (`Export.holding_map`, `Common.money`, …). MCP does not call
the API over HTTP: that would add a network hop and a second way to fail, and change nothing.

**Why not have the CLI call MCP**, which works today (quantic-agent does)?
- **MCP's output is written for language models.** Tool descriptions and result shapes are tuned
  for an LLM and may change for its sake. The payload is JSON *inside* a text field (decoded twice).
  An API version is a promise to programs; a tool description isn't.
- **HTTP does the rest.** Status codes instead of `isError` results; `ETag` and `304 Not Modified`
  for a widget that refreshes every few minutes; standard caching and proxies.
- **The auth is already there.** Quantic's personal API tokens are documented as "Bearer
  credentials for the read-only API / MCP". The same `Authorization: Bearer qtc_…` works for both.
- **Other clients benefit.** The native app could use it instead of a cookie session.

**The cost:** server work comes first, and `/api/v1` is a compatibility commitment. Mitigation: a
small surface (below), only additive changes within v1, and an OpenAPI document checked in CI.

### 2.1 The v1 surface

Only what the CLI and widget need. Each endpoint mirrors an existing MCP tool, so the work is mostly
a controller and a serializer.

| Endpoint | Auth | Mirrors MCP tool | Notes |
|---|---|---|---|
| `GET /api/v1/me` | token | — | user, preferred currency; lets `auth status` verify a token |
| `GET /api/v1/portfolios` | token | `list_portfolios` | |
| `GET /api/v1/holdings?portfolio=` | token | `get_holdings` | cost basis |
| `GET /api/v1/dividends?symbol=&from=&to=&portfolio=` | token | `get_dividends` | received |
| `GET /api/v1/income?portfolio=&years=` | token | `get_income_outlook` | includes `income_year` by month |
| `GET /api/v1/upcoming?days=&portfolio=` | token | **new** | your holdings' next ex-dates, pay dates, expected amounts |
| `GET /api/v1/calendar?days=` | none | `dividend_calendar` | public, same 60/min anonymous limit |
| `GET /api/v1/stocks/:symbol` | none or token | `get_stock` | richer when signed in, as in MCP |
| `GET /api/v1/stocks?q=` | none | `search_stocks` | |

**`/upcoming` is the one genuinely new query**, and the reason the widget is worth having: "what is
*my* next dividend". Today it can only be approximated by intersecting the public calendar with your
holdings, which gives ex-dates but no pay dates and no amounts (the calendar has neither). Quantic
already projects per-holding income for `income_outlook`, so the data is in the app. The query
isn't, yet.

**Conventions:**
- Money as `{"amount": "12.34", "currency": "EUR"}`: decimal strings, never floats.
- Dates as ISO 8601 (`2026-10-08`); timestamps in UTC.
- Errors as `{"error": {"code": "unauthorized", "message": "…"}}` with the matching status: `401`
  for a bad token, `403` for a valid token without access, `404`, `422`, `429`.
- Every response carries `ETag`; clients send `If-None-Match`.
- An OpenAPI 3.1 document at `/api/v1/openapi.json`, generated from the controllers or written by
  hand and tested against them. The CLI's client code is generated from it.

---

## 3. Commands

```
quantic auth login [--with-token]     # store a token (section 4)
quantic auth logout
quantic auth status                   # who you are; exit 4 if not signed in

quantic portfolios
quantic holdings      [--portfolio NAME]
quantic dividends     [--symbol S] [--from D] [--to D] [--portfolio NAME]
quantic income        [--portfolio NAME] [--years N]
quantic upcoming      [--days N] [--portfolio NAME]

quantic calendar      [--days N]      # public: works signed out
quantic stock SYMBOL                  # public, richer when signed in
quantic search QUERY

quantic status                        # the widget's one call (section 8)
quantic cache clear
quantic version
```

Global flags: `--json`, `--portfolio`, `--no-cache`, `--timeout` (default 10s). Environment:
`QUANTIC_TOKEN` (overrides the keyring, for CI and scripts), `QUANTIC_URL` (defaults to
`https://quantic.finance`; local Phoenix for development).

**Output:** tables for people, chosen when stdout is a terminal; `--json` for programs. Color only on
a terminal, and never with `NO_COLOR` set.

---

## 4. Authentication

**v1: a personal API token.** You create one in Quantic's settings ("quantic-cli on desktop";
up to 10 per user, revocable, shown once), then:

```
quantic auth login --with-token < token.txt     # or paste at the prompt
```

The same pattern as `gh auth login --with-token`. The token is stored in the system keyring (Secret
Service on Linux, Keychain on macOS) via `zalando/go-keyring`. If no keyring is available, it falls
back to a `0600` file under `$XDG_CONFIG_HOME/quantic/`, with a warning. `QUANTIC_TOKEN` overrides
both. The token never appears in flags, logs, cache files or error messages.

**Later: browser login.** OAuth 2.1 with PKCE and a loopback redirect (`http://127.0.0.1:<port>`,
RFC 8252), the usual way for a desktop CLI. Quantic already runs an OAuth server with dynamic client
registration for MCP clients. Open question: whether it accepts loopback redirect URIs (section 13).

**Unlike quantic-agent:** decision 0006 there forbids giving the agent a personal token, because the
agent must never be able to read a user's portfolio. The CLI is the user acting as themself, on
their own machine. A personal token is exactly what it's for.

---

## 5. Output contract (`--json`)

The JSON belongs to the CLI, not to the API: the CLI may add fields (such as `stale`) or reshape
for convenience, and it versions its own output.

```json
{
  "schema": "quantic.cli/upcoming/v1",
  "fetched_at": "2026-10-07T09:12:03Z",
  "stale": false,
  "items": [
    {"symbol": "MSFT", "name": "Microsoft", "ex_date": "2026-10-08", "pay_date": "2026-12-11",
     "expected": {"amount": "6.64", "currency": "USD"}, "portfolio": "Main"}
  ]
}
```

- `schema` names the shape and its version; a breaking change is `/v2`, and both are produced for a
  release with a deprecation notice.
- Golden files in the tests pin every shape (section 11).

---

## 6. Exit codes

| Code | Meaning |
|---|---|
| 0 | OK, possibly from cache (`stale` says so in `--json`) |
| 1 | Failed |
| 2 | Wrong usage |
| 3 | Quantic unreachable, and nothing cached |
| 4 | Not signed in, or the token was rejected (`401`) |
| 5 | Rate limited, after retrying |

A script or the widget can tell "log in again" (4) from "offline" (3) without parsing messages.

---

## 7. Cache, offline, and being polite

- **Where:** `$XDG_CACHE_HOME/quantic/` (`~/.cache/quantic`), directory `0700`, since it holds
  portfolio data. `--no-cache` skips it; `quantic cache clear` and `auth logout` empty it.
- **Freshness:** per endpoint. The public calendar for 6h, holdings and income for 15 minutes,
  `upcoming` for 1h. With an `ETag`, a refresh that changed nothing is a `304` with no body.
- **Offline:** if Quantic can't be reached, serve the cached copy with `stale: true` and its
  `fetched_at`, and exit 0. The bar keeps showing the last known state, marked stale.
- **One fetch at a time:** a lock file per cache entry, so the widget and a terminal command don't
  fetch the same thing at once (the `flock` pattern from quantic-agent-go's store).
- **Rate limits:** anonymous calls share Quantic's 60/min per IP. A `429` is retried with backoff
  and jitter, as in quantic-agent-go's `internal/mcp`, then exit 5. Authenticated calls aren't
  rate limited today, but the widget still refreshes no more than every few minutes.

---

## 8. `quantic status`: one call for the widget

The widget shouldn't run four commands and merge them in QML. `quantic status --json` fetches
what a glance needs, concurrently (`errgroup`), and returns one document:

```json
{
  "schema": "quantic.cli/status/v1",
  "fetched_at": "2026-10-07T09:12:03Z",
  "stale": false,
  "signed_in": true,
  "next": {"symbol": "MSFT", "event": "ex_date", "date": "2026-10-08", "in_days": 1},
  "this_month": {"received": {"amount": "42.10", "currency": "EUR"},
                 "projected": {"amount": "118.00", "currency": "EUR"}},
  "upcoming": [ /* next 14 days, as in section 5 */ ]
}
```

A partial failure, such as the income call failing while upcoming succeeds, returns what it has,
with an `errors` list, and still exits 0. The widget shows what it can.

---

## 9. The Omarchy widget

An Omarchy shell plugin: a bar pill with a popup panel. It follows the pattern of the built-in
Tailscale widget, which drives the `tailscale` CLI (`tailscale status --json`) from QML.

**It lives in its own repository**, not this one. `omarchy plugin add <git-url>` clones a repository
and reads `manifest.json` at its root, so a plugin *is* a repository. The widget is also a different
program: QML and JavaScript running inside Omarchy's shell, with no Go in it. What ties the two
together is a contract, not shared code: the widget runs `quantic status --json` and reads
`quantic.cli/status/v1` (section 8). The CLI can be used without the widget; the widget needs the
CLI installed.

```
omarchy-quantic/                 # its own repo; `omarchy plugin add <its git url>`
  manifest.json                  # kind: bar-widget (shape of omarchy.weather's)
  BarWidget.qml                  # the pill
  Panel.qml                      # the popup
  Model.js                       # parsing and formatting, kept out of the QML
```

**How it works.** A Quickshell `Process` runs `quantic status --json` on a `Timer` (every 10
minutes by default, never under 5), and once when the panel opens. The QML parses the JSON and
renders. All logic stays in the Go CLI; the widget stays thin.

**The pill** shows one thing:
- the next event: `MSFT ex-div tomorrow`;
- or, with "show amounts" on, this month's income: `€42.10 / €118`;
- dimmed when the data is stale, with a tooltip saying since when.

**The panel:** the next 14 days of ex-dates and payments, this month received against projected,
the last refresh time, and two actions: Refresh, and Open Quantic (`xdg-open`).

**States the widget handles**, each from the CLI's exit code or `which quantic`, as the Tailscale
widget does:

| State | Pill shows |
|---|---|
| CLI not installed | Quantic icon, dimmed; the panel says how to install it |
| Not signed in (exit 4) | icon with a key mark; the panel says `quantic auth login` |
| Offline, cached (stale) | the cached text, dimmed |
| Offline, nothing cached (exit 3) | icon only |
| OK | the next event, or the month's income |

**Privacy.** A bar is visible when sharing your screen. Amounts are hidden in the pill by default;
a setting ("show amounts in the bar") turns them on. The panel shows them, since opening it is
deliberate.

**Settings** (the plugin's settings form, like the weather widget's): refresh interval, portfolio,
show amounts in the bar.

---

## 10. Code layout

```
cmd/quantic/            main: wiring only
internal/cli/           Cobra commands, flags, exit codes
internal/api/           client generated from Quantic's OpenAPI document, plus a thin wrapper
internal/auth/          keyring, QUANTIC_TOKEN, the file fallback
internal/cache/         files, freshness, ETags, per-entry locks
internal/render/        tables (text/tabwriter) and JSON output types
internal/status/        the concurrent composition behind `quantic status`
```

The widget is in its own repository (section 9).

**Libraries:** `spf13/cobra` (commands), `zalando/go-keyring`, `oapi-codegen` (client from
OpenAPI), `golang.org/x/sync/errgroup`, `rogpeppe/go-internal/testscript` (tests). The terminal
dashboard adds `charmbracelet/bubbletea` and `lipgloss`. Everything else from the standard library.

---

## 11. Testing

- **Unit tests** against an `httptest` server replaying recorded API responses.
- **Recorded responses never contain real portfolio data.** They come from a Quantic demo user
  (`Quantic.Demo` creates an ephemeral user with curated data), or are written by hand.
- **Golden files** for every `--json` shape and every table, updated with `-update` and reviewed in
  the diff.
- **`testscript`** for end-to-end runs of the real binary: commands, exit codes, stdout and stderr in
  readable `.txtar` scripts. The Go tool itself is tested this way.
- **Contract test** on the server side: the OpenAPI document is checked against the controllers in
  Quantic's CI, so the API can't drift from what the CLI generated against.
- The widget, in its repository: `Model.js` parsing tested against this repo's golden `status` JSON;
  the QML checked by hand. When `status/v1` changes, the golden file is the signal for both.

---

## 12. Milestones

Each ships code and, as in quantic-agent, a lesson and a walkthrough. The Go each one covers:

| # | Milestone | Go ground covered |
|---|---|---|
| 0 | Repo, this design | — |
| S1 | **Quantic (Elixir):** `/api/v1` for `me`, `portfolios`, `holdings`, `calendar`, OpenAPI doc | — (server work) |
| 1 | Cobra skeleton: `version`, flags, `--json`, exit codes; `testscript` | Cobra, `io.Writer` design, testscript |
| 2 | Generated client; `calendar`, `stock`, `search` signed out | OpenAPI codegen, `net/http`, `context` |
| 3 | `auth` with keyring; `holdings`, `portfolios`, `dividends`, `income` | interfaces for secrets, OS integration |
| 4 | Cache: freshness, ETags, offline, locks | files, `encoding/json`, `flock`, time |
| S2 | **Quantic (Elixir):** `/api/v1/upcoming` with pay dates and amounts | — |
| 5 | `upcoming` and `status`, concurrently | `errgroup`, partial failure |
| 6 | The Omarchy widget, in its own repo | (QML, not Go; consuming your own JSON contract) |
| 7 | Release: GoReleaser, an AUR package (Omarchy is Arch), Homebrew | build flags, cross-compilation, checksums |
| 8 | `quantic dash`: terminal dashboard | Bubble Tea (Elm architecture) |
| 9 | Browser login: OAuth 2.1, PKCE, loopback redirect | `net/http` server, `crypto/rand`, OAuth |

---

## 13. Open questions

1. **API in Quantic, or the CLI on MCP?** This design proposes the API (section 2). Confirm before
   S1; if the answer is MCP, milestones 2–3 change to reuse quantic-agent-go's MCP client, and
   `upcoming` stays approximate (ex-dates only).
2. **Does Quantic's OAuth server accept loopback redirect URIs** from dynamically registered
   clients? Needed for milestone 9; until then, tokens.
3. ~~Where the widget lives.~~ Its own repository: `omarchy plugin add` reads `manifest.json` at a
   repository's root (section 9). Its name is still open (`omarchy-quantic`?).
4. **The binary's name:** `quantic` is short; check for clashes in the AUR and Homebrew.
5. **Sharing code with quantic-agent-go** (the MCP client, backoff): copy for now; a shared module only
   if both keep using it.
