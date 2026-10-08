# 0001 — A versioned JSON API in Quantic, not the CLI on MCP

**Status:** accepted · **Date:** 2026-10-07 · **Answers:** open question 1 in the
[design](../design.md#13-open-questions)

## Context

The CLI needs Quantic's data: portfolios, holdings, dividends, income, and public stock data. Two ways
to get it:

- **MCP**, which works today. Quantic already serves an MCP server, quantic-agent talks to it, and
  [quantic-agent-go](https://github.com/fleveque/quantic-agent-go) has a Go MCP client that could be
  copied.
- **A new read-only JSON API**, `/api/v1`, in Quantic. Server work in Elixir before the CLI can
  sign in.

The CLI's `--json` output is a contract other programs build on, starting with the Omarchy widget
(design §9), which refreshes every few minutes. Whatever the CLI reads from has to be stable enough to
carry that contract.

## Decision

**Quantic gets `/api/v1`, and the CLI talks only to it** (design §2). The API and MCP are siblings:
both call the same contexts and share the existing serializers. Neither calls the other over HTTP.

The v1 surface is the one in design §2.1, and grows only by additive changes within v1. An OpenAPI 3.1
document is served at `/api/v1/openapi.json`, checked in CI, and the CLI's client is generated from it.

## Consequences

**Good.**
- A version is a promise to programs. MCP tool descriptions and result shapes are tuned for language
  models and may change for their sake; the API's don't.
- Plain HTTP: status codes instead of `isError` results, JSON decoded once instead of JSON inside a
  text field, `ETag` and `304 Not Modified` for the widget's polling, standard caching.
- No new auth: the personal API tokens are already Bearer credentials for "the read-only API / MCP".
- `/api/v1/upcoming` (milestone S2) can return pay dates and expected amounts. The public calendar has
  neither, so on MCP `upcoming` would stay approximate: ex-dates only.
- Other clients, such as the native app, can use it instead of a cookie session.

**Costs.**
- Server work comes first: milestone S1, in Elixir in `../quantic`, before the CLI's generated
  client (milestone 2) and its authenticated commands (milestone 3).
- `/api/v1` is a compatibility commitment Quantic has to keep. Mitigated by a small surface, additive
  changes only within v1, and the OpenAPI document checked in CI.
- Two interfaces to the same data to keep consistent. Shared serializers keep the drift small.

**Rejected alternative — the CLI on MCP.** No server work, and the quantic-agent-go client could be
copied. But the CLI's JSON contract would rest on output written for language models, `upcoming`
could never show pay dates or amounts, and the widget would poll without conditional requests.
Cheaper now, paid for on every later milestone.

**Update, 2026-10-08.** Built in milestone S1. The document is OpenAPI 3.0, not 3.1, because 3.0
is what `open_api_spex` generates; `oapi-codegen` supports both. The decision is otherwise unchanged.

**Rejected alternative — MCP now, the API later.** Builds milestones 2–3 twice and makes the lessons
about a client that gets thrown away.
