# 0002 — The API client is generated from a checked-in copy of the OpenAPI document

**Status:** accepted · **Date:** 2026-10-08 · **Milestone:** 2

## Context

[ADR 0001](0001-json-api-not-mcp.md) decided the CLI's client is generated from Quantic's OpenAPI
document, with `oapi-codegen` (design §10). Generating it raised three choices: where the document
comes from at build time, how to fix what doesn't suit Go in it, and whether the generated code is
committed.

Two things in the document as Quantic serves it don't suit Go:

- **Operation names** follow the server's controllers (`QuanticWeb.API.V1.CalendarController.index`),
  which become `QuanticWebAPIV1CalendarControllerIndexWithResponse`.
- **Numbers have no format**, and `oapi-codegen` makes those `float32`, which keeps about 7
  significant digits. A growth rate of `0.044617420086995985` comes back as `0.044617422`.

## Decision

- **`api/openapi.json` is an exact copy** of `https://quantic.finance/api/v1/openapi.json`, updated
  by hand when the CLI needs something new (the commands are in `internal/api/doc.go`). A build never
  fetches anything.
- **`internal/api/overlay.yaml`**, an [OpenAPI Overlay](https://spec.openapis.org/overlay/v1.0.0.html),
  renames the operations and makes every number a `double` before generation. The copy stays exactly
  what the server publishes, and every change to it is in one reviewed file. *Removed 2026-10-09:
  see the update below.*
- **`internal/api/api.gen.go` is committed**, and `oapi-codegen` is a `tool` in `go.mod`
  (`go tool oapi-codegen`), pinned like any dependency. CI runs `go generate ./...` and fails if
  anything changes, so the committed code is always what the document produces.
- The generated client is unexported (`genClient`). The commands use `internal/api`'s `Client`:
  one method per endpoint, typed errors (`StatusError`, `UnreachableError`), and the 429 retry
  (design §7) in an `http.RoundTripper` the generated code never sees.

## Consequences

**Good.**
- `go install` and `go build` need no network beyond modules, and no generator: the generated code
  is in the repository.
- A change in Quantic's API reaches the CLI only when someone copies the new document and reviews the
  diff, in `api/openapi.json` and in `api.gen.go`.
- `oapi-codegen` is in `go.mod` but not in the binary: `go version -m` lists only the runtime
  packages (`oapi-codegen/runtime`, `google/uuid`, `go-jsonmerge`).

**Costs.**
- The copy can fall behind the server. That's on purpose: a new endpoint is used when a milestone
  needs it.
- `go.mod` lists the generator's own dependencies (about 20 indirect lines), because a `tool` is a
  dependency of the module.
- The overlay was CLI-side: it fixed the names and numbers for this client only. See the update.

**Update, 2026-10-09: the fix moved to the server.** Quantic now names every operation
(`operation_id:`) and gives every number `format: :double`, and its `spec_test.exs` fails if a new
operation or number leaves them out ([quantic#488](https://github.com/fleveque/quantic/pull/488)).
Generating from the new document without the overlay produced the same `api.gen.go`, byte for byte,
so the overlay is gone and `api/openapi.json` is again all `oapi-codegen` reads.

**Rejected alternative — generate at build time from the live document.** Builds would depend on the
network and on whatever the server published that minute, and a breaking change would show up as a
failed build instead of a reviewed diff.

**Rejected alternative — a hand-written client.** Three endpoints now would be shorter by hand, but
the client covers all of them, and design §11 relies on the CLI and server sharing one document.
