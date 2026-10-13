# 0004 — The cache is an HTTP transport

**Status:** accepted · **Date:** 2026-10-13 · **Milestone:** 4

## Context

Design §7 asks for a cache in `$XDG_CACHE_HOME/quantic`: per-endpoint freshness, ETag
revalidation, the last answer served (marked stale) when Quantic can't be reached, and one fetch
at a time per entry, so the status bar and a terminal command don't both ask. Building it raised
choices the design leaves open:

- Where it sits: in each command, in the API client's methods, or underneath them.
- What an entry is keyed by, given that answers depend on the token and the token must never be
  written to a cache file.
- What "can't be reached" covers.
- How freshness is tested when it depends on the clock, across the separate processes testscript
  runs.

Quantic answers every `/api/v1` request with a weak `ETag` and `Cache-Control: private, no-cache`,
and answers a matching `If-None-Match` with `304` and no body (checked against production).

## Decision

- **The cache is an `http.RoundTripper`** (`cache.Transport`), passed as `api.Config.Transport`,
  so the retry layer from milestone 2 wraps it. The API client and the commands don't change shape:
  a method still returns the decoded answer. ETags and 304s are HTTP, and they're handled at the
  HTTP layer. Each call builds its own `Transport`, and `callQuantic` reads `Last()` afterwards
  for `fetched_at` and `stale`.
- **The key is a hash** (SHA-256, 128 bits as hex) of the method, the URL and the `Authorization`
  header. Each token gets its own entries, so two accounts on one machine never mix. Signed in and
  signed out are different entries (`stock` is richer signed in). A hash can't be turned back
  into the token. The entry file keeps the path and query for someone reading it, never the
  header.
- **Freshness is the CLI's policy** (`freshFor`), passed to the transport: the calendar for 6h;
  holdings, dividends, income and a stock for 15 minutes; portfolios and searches for 1h. `/me` is
  never fresh: `auth status` is how a script checks a token. The CLI serves a fresh entry without
  revalidating, which deliberately goes further than Quantic's `no-cache`: the design's freshness
  times are the CLI's own, and a status bar that asks every few minutes shouldn't.
- **"Can't be reached"** is a transport error (no connection, DNS, a timeout), or a `502`, `503` or
  `504`, which is what a proxy says when nothing is behind it. Any other answer, a `401` or `429`
  included, passes through and isn't stored: the cache never hides what Quantic said. Only a `200`
  with a JSON body is stored. An entry keeps its body as `json.RawMessage`, and `json.Marshal`
  refuses one that isn't JSON.
- **A stale answer** exits 0, with `"stale": true` and the entry's `fetched_at` in the JSON, and a
  line on stderr: `can't reach quantic.finance; this is what it said at … UTC, an hour ago`.
- **One fetch at a time:** an exclusive `flock` on a `.lock` file beside the entry (`LockFileEx` on
  Windows), taken only to fetch. The lock is tried without blocking and retried every 25 ms until
  the request's context ends, because a blocking `flock` can't be cancelled. After getting it, the
  entry is read again: if another process just refreshed it, that answer is used.
- **A cache that can't be used is no cache.** An entry that won't parse is ignored, and a failed
  write is ignored. A lock that can't be taken for any reason other than time running out means
  asking Quantic without it. None of these fails a command.
- **Files:** `privfile.Write` (new, shared with the token file): written to a `0600` temporary file
  and renamed into place, in a `0700` directory. A reader never sees half an entry.
- **`--no-cache`** neither reads nor writes. `auth login` never uses the cache, so a new token is
  always checked by Quantic. `quantic cache clear` and `auth logout` remove the directory.
- **The clock is a dependency**, like the keyring: `cli.Deps{Keyring, Now}`. Scripts set
  `TEST_NOW`, read only by the test binary's `mainForTests`.

## Consequences

**Good.**
- A status bar refreshing every few minutes asks Quantic for holdings at most every 15 minutes, and
  an unchanged answer costs a `304` with no body.
- With the network down, every command that has run before still answers, marked stale.
- The cache can be tested on its own (`internal/cache`), with a fake clock and an `httptest`
  server, without the CLI.

**Costs.**
- The cache can show data up to its freshness time old with no network trouble at all: holdings
  changed in the app show up in the CLI within 15 minutes. `--no-cache` is the way around it.
- Entries are never pruned. A cache of searches grows with every new query, a few hundred bytes
  each, until `cache clear`.
- `cli.RunWith` changed signature (`Deps` instead of a keyring). It has no users outside this
  repository's tests.

**Rejected alternative — caching in each command, or in the API client's methods.** Every method
would take an ETag and return "not modified", and every command would handle it: eight copies of
what one `RoundTripper` does once.

**Rejected alternative — serving stale on any error.** A `401` served from the cache would hide a
revoked token, and a `429` would hide rate limiting. Both are Quantic's answer, and the user should
see them.

**Rejected alternative — a blocking `flock` with a timer goroutine to give up.** The goroutine
can't cancel the system call; it can only stop waiting for it and leave a lock acquired later by
nobody. Polling with `LOCK_NB` ends cleanly when the context does.
