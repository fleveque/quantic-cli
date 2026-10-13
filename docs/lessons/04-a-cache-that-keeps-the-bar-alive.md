# Lesson 04 — A cache that keeps the bar alive

**Milestone 4** — the cache. Answers from Quantic are kept on disk and used while they're fresh.
After that they're checked with an `ETag`, which costs a `304` and no body when nothing changed.
When Quantic can't be reached, the last answer is shown, marked stale, and the command still
succeeds. That's what a status bar needs: it asks every few minutes, and on a train it should keep
showing yesterday's numbers rather than an error. This milestone is about `http.RoundTripper`
again, files and locks the operating system keeps, and time as something a test controls.

*Also readable as a [formatted page](https://claude.ai/artifact/8aYsD9oBMfykrCjLAb2FwZ). The code, file by file:
[walkthrough](https://claude.ai/artifact/VaoAQndrkGKbrypASjjvZX).*

---

## What landed

```
internal/cache/
  store.go                package cache  entries on disk, keys, a lock per entry
  transport.go                           the cache as an http.RoundTripper
  lock_unix.go, lock_windows.go          flock, and LockFileEx
internal/privfile/privfile.go            write a file only you can read, in one step
internal/cli/
  client.go                              freshness per endpoint; fetched_at and stale
  cache.go                               quantic cache clear
  root.go                                Deps: the keyring, and a clock
cmd/quantic/testdata/script/cache.txtar  the whole life of an entry, with TEST_NOW
docs/decisions/0004-the-cache-is-a-transport.md
```

No new dependencies. `golang.org/x/sys` moved from indirect to direct, for the Windows lock.

The second run of the same command skips the network:

```
$ time (quantic calendar --days 7 --json | head -4)
{
  "schema": "quantic.cli/calendar/v1",
  "fetched_at": "2026-10-13T13:53:09Z",
  "stale": false,
(0.147s)
$ time (quantic calendar --days 7 --json | head -4)
{
  "schema": "quantic.cli/calendar/v1",
  "fetched_at": "2026-10-13T13:53:09Z",
  "stale": false,
(0.003s)
```

Same `fetched_at`, three milliseconds: the answer came from `~/.cache/quantic`.

## Where a cache goes

My first thought was to cache in each command: read the file, check its age, call the API, write
the file. Eight commands, eight copies. The second was the API client's methods, which would each
take an ETag and return "not modified" as well as the answer. Both put an HTTP concern, ETags and
304s, in code that otherwise doesn't know about HTTP.

Lesson 02 already had the answer. An `http.RoundTripper` takes a request and returns a response,
and wrapping one in another is middleware. The retry for a `429` is one. The cache is another,
underneath it:

```
http.Client → retryTransport → cache.Transport → the network
```

When the cache can answer, it builds an `*http.Response` itself, a `200` with the stored body, and
the generated client above decodes it exactly as if it had come from Quantic. Nothing above the
transport knows the cache exists. In Ruby this is `faraday-http-cache`, a Faraday middleware; in
Elixir, a Tesla middleware or a Req step. It's the same idea in all three, and Go's version is one
interface with one method.

What the commands do need is the answer's time, and whether it's stale. Each call
builds its own `cache.Transport`, so after the call `callQuantic` asks it: `cached.Last()`
returns the `fetched_at` and the `stale` flag for the envelope.

## ETags, and what a 304 costs

Quantic sends a weak `ETag` on every answer, and checks `If-None-Match`:

```
$ curl -sS -D - -o /dev/null https://quantic.finance/api/v1/calendar?days=7 | grep -i etag
etag: W/"IaU6QiiKUMa8S4XQ3JOHgg"
$ curl -sS -D - -o /dev/null -H 'If-None-Match: W/"IaU6QiiKUMa8S4XQ3JOHgg"' https://quantic.finance/api/v1/calendar?days=7 | head -1
HTTP/2 304
```

So an entry older than its freshness isn't thrown away. It's sent back as a question, "still
this?", and a `304` means yes: the cache keeps the body, notes it as fetched now, and answers. The
`W/` means weak: equal in meaning, not necessarily byte for byte, which is all a client needs. The
server computes it by hashing the body it was about to send, so a `304` saves the transfer, not the
server's work.

Freshness is per endpoint, because the data changes at different speeds: the public calendar for
6 hours; holdings, dividends, income and a stock's price for 15 minutes; portfolios and searches
for an hour. `/me` is never fresh, because `auth status` is how a script checks whether a token
still works. Quantic's own header says `Cache-Control: private, no-cache` ("check before every
use"), and the CLI deliberately does less checking than that. A status bar that asks every two
minutes shouldn't make a request every two minutes.

## A key that holds no token

Answers depend on who's asking: your holdings aren't mine, and `stock KO` is richer signed in. So
the key has to include the token, and the token must never be written to a cache file. The key is
a hash:

```go
h := sha256.New()
h.Write([]byte(req.Method + " " + req.URL.String() + "\n" + req.Header.Get("Authorization")))
return hex.EncodeToString(h.Sum(nil))[:32]
```

SHA-256 can't be reversed, and a `qtc_` token is long and random, so it can't be guessed from its
hash either. The file is named by the hash (`472a9f2f7c73a796acc41322d65ec5d7.json`) and holds the
URL, the time, the ETag and the body. A test fills the cache with tokens marked `SECRET` and greps
every file for them.

`hash.Hash` is an `io.Writer`, which is why it's `h.Write`, and `h.Sum(nil)` appends the digest to
the slice it's given. Passing `nil` asks for a new one. Small, and very Go: hashing isn't special,
it's writing.

## When Quantic can't be reached

"Can't be reached" is narrower than "something went wrong":

- **No answer at all**: refused, no route, DNS, a timeout. The transport below returns an error.
- **A `502`, `503` or `504`**: that's a proxy or load balancer saying there's nothing behind it.
  It isn't Quantic's answer.

Everything else passes through. A `401` served from the cache would hide a revoked token, and a
`429` would hide rate limiting; both are things Quantic is telling you.

To see it for real, I aged an entry by hand and cut the network. `unshare -rn` runs a command in
a new network namespace with no interfaces but loopback, without root:

```
$ unshare -rn quantic stock KO
quantic: can't reach quantic.finance; this is what it said at 2026-10-13 12:00 UTC, an hour ago
KO  Coca-Cola
Consumer Defensive · Beverages - Non-Alcoholic

Price     89.79 USD, as of 2026-10-13 13:00 UTC
...
[exit 0]
```

Exit 0, the note on stderr so stdout stays the answer, and in `--json`, `"stale": true` with the
old `fetched_at`. The widget can grey itself out without parsing a word. With nothing cached, it's
still exit 3.

## A lock the operating system holds

The status bar and a command in a terminal can ask for the same thing at the same moment. Without
coordination both go to Quantic, and both write the entry. Each write is atomic (next section), so
nothing breaks. It's just wasteful, and against the anonymous rate limit, wasteful matters.

So each entry has a lock file, and fetching it takes an exclusive `flock`:

```go
err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
if errors.Is(err, syscall.EWOULDBLOCK) {
	return false, nil
}
```

Three things about `flock` I had to learn:

1. **It's advisory.** It stops nothing but other `flock` calls. Anyone can still read the file; the
   lock is a convention among processes that agree to ask.
2. **The kernel releases it** when the file is closed, which happens when the process exits, even
   by `kill -9`. A crashed `quantic` can't leave the cache locked. A lock implemented as "create a
   file, delete it when done" can.
3. **It belongs to the open file, not the process.** Two `os.OpenFile`s of the same path in one
   process exclude each other. That's what lets a Go test with two goroutines stand in for two
   processes.

`LOCK_NB` means "don't wait": `EWOULDBLOCK` if someone else has it. The code tries every 25 ms
until the request's context says stop. A blocking `flock` would be simpler, but it can't be
cancelled, and `--timeout` has to cover waiting for the lock too. Once the lock is held, the code
reads the entry again. If the other process just refreshed it, that answer is used and Quantic
isn't asked.

`flock` is Unix. On Windows it's `LockFileEx`, from `golang.org/x/sys/windows`. One file each, and
a build constraint at the top picks which one is compiled:

```
//go:build unix
```

```
$ go list -f {{.GoFiles}} ./internal/cache
[lock_unix.go store.go transport.go]
$ GOOS=windows go list -f {{.GoFiles}} ./internal/cache
[lock_windows.go store.go transport.go]
```

Both files define `tryLock` and `unlockFile`, with the same signatures. Elixir would reach for a
`:global` lock or a GenServer holding the state; but those coordinate processes inside one BEAM.
Two separate OS processes, the bar and a shell, need something the kernel holds.

## Files nobody else can read, again

Lesson 03 wrote the token file as a new `0600` temporary file renamed into place. The cache holds
portfolio data, and needs the same, so that code moved to `internal/privfile`, a package of one
function that both now call. A reader never sees half an entry either, because the name only
points at the new file once it's complete.

## A cache that can't be written is no cache

While writing the lock, I asked what happens if the cache directory can't be created: a read-only
home, or a container with no `$HOME`. `Lock` failed, and the first version returned that error, so
`quantic calendar` failed because of a cache. Now an entry that won't parse is ignored, a failed
write is ignored, and a lock that can't be taken for any reason other than running out of time
means asking Quantic without one. A test points the cache at a path under a regular file, which
can never be a directory, and checks both requests go through.

## json.RawMessage, and a check that did nothing

An entry keeps the body as `json.RawMessage`, a `[]byte` that `encoding/json` writes as JSON
rather than base64, so a cache file is readable:

```json
{"url":"/api/v1/stocks/KO","fetched_at":"2026-10-13T13:53:19.64596927Z","etag":"W/\"…\"","content_type":"application/json; charset=utf-8","body":{"symbol":"KO",…}}
```

I'd also written an `isJSON` check on the `Content-Type`, so that a hotel wifi's login page,
answered with `200` and HTML, wouldn't be cached. Breaking it on purpose (below) changed nothing,
because `json.Marshal` already refuses a `RawMessage` that isn't JSON:

```
json: error calling MarshalJSON for type *jsontext.Value: invalid character '<' looking for beginning of value
```

That `Put` fails, the failure is ignored as above, and the HTML is answered but never kept. The
check is gone, and a comment says what really keeps HTML out. (The `jsontext.Value` in the message
suggests that in this Go version `encoding/json` runs on the newer `encoding/json/v2` underneath.)

## Time is a dependency

"Fresh for 15 minutes" can't be tested by waiting 15 minutes. The transport takes a clock,
`Now func() time.Time`, and the unit tests pass one that only moves when told to:

```go
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }
```

Scripts run the real binary as separate processes, so the clock has to come from outside. Lesson
03's `RunWith(…, keyring)` became `RunWith(…, cli.Deps{Keyring, Now})`, and `mainForTests` reads
`TEST_NOW`:

```
env TEST_NOW=2026-10-13T09:10:00Z
exec quantic holdings --json
stdout '"fetched_at": "2026-10-13T09:00:00Z",\n  "stale": false'
grep -count=1 '^GET /api/v1/holdings ' requests.log
```

Ten minutes on, the same `fetched_at` and still one request. At twenty, the request log shows
`If-None-Match: W/"h1"`, and `fetched_at` moves. The script then creates `quantic/down`, which makes
the fake Quantic drop every connection unanswered, and checks for `"stale": true` and the note. In
Elixir I'd have reached for a mock of `DateTime.utc_now/0`. Here it's a field, and the binary
always leaves it `nil`.

The clock returns UTC, too. The first entries said `"fetched_at": "…+02:00"`: correct, but a
cache file from a laptop in Madrid read differently from one in CI.

## Testing it

- **`internal/cache`** on its own, with a fake Quantic that counts requests and answers
  `If-None-Match` as the real one does: fresh, revalidated, changed, offline (refused, 502, 503,
  504), passed through (401, 404, 429, 500), per-token entries and no token in any file, `0700`
  and `0600`, HTML and POST not kept, two goroutines and one request, a lock wait that ends with
  the context, a broken entry, an unwritable directory.
- **`internal/cli`**: each command run twice asks once, except `auth status`; `--no-cache`; a
  stale `auth status` offline; `login` never trusting the cache; `logout` and `cache clear`
  emptying it. Every test now gets a cache directory of its own, because a fake server's port can
  come round again in a later test, and its answers mustn't.
- **`cache.txtar`**: the life of one entry through the real binary, with `TEST_NOW` and the
  `down` switch.

## Breaking it on purpose

| Broken | Caught by |
|---|---|
| never fresh | `TestAFreshAnswerIsntAskedAgain`, `TestWhatIsCached`, `cache.txtar` |
| no `If-None-Match` | `TestAnOldAnswerIsRevalidated`, `TestMaxAgeZeroAlwaysAsks`, `cache.txtar` |
| a 304 doesn't count as fetched now | `TestAnOldAnswerIsRevalidated`, `cache.txtar` |
| offline: no fallback | `TestUnreachableServesTheLastAnswer`, `TestStaleAnswer`, `cache.txtar` |
| the offline answer not marked stale | the same three |
| 502–504 not "unreachable" | `TestUnreachableServesTheLastAnswer/502`, `/503`, `/504` |
| a 401 hidden by the cache | `TestOtherAnswersPassThrough` |
| one entry for every token | `TestEachTokenHasItsOwnEntry`, `TestPublicCommandsSendTheTokenIfAny` |
| cache files `0644` | `TestTheCacheIsPrivate`, `TestWrite`, `auth.txtar` |
| no lock | `TestOneFetchAtATime`, `TestLockWaitEndsWithTheContext` |
| the lock wait ignores the context | `TestLockWaitEndsWithTheContext`, by hanging until `go test -timeout` |
| no second look after the lock | `TestOneFetchAtATime` |
| a broken entry used | `TestABrokenEntryIsIgnored` |
| an unwritable cache fails the command | `TestAnUnwritableCacheIsNoCache` |
| the body as `[]byte`, not `RawMessage` (HTML kept) | `TestOnlyJSONAnswersToGETAreKept` |
| `isJSON` removed | nothing: `RawMessage` already refuses HTML; check deleted |
| POST cached | `TestOnlyJSONAnswersToGETAreKept` |
| `login` uses the cache | `TestLoginNeverUsesTheCache` |
| `logout` keeps the cache | `TestLogoutClearsTheCache`, `cache.txtar` |
| `--no-cache` ignored | `TestNoCache`, `cache.txtar` |
| `/me` cached for an hour | `TestWhatIsCached/auth_status`, `TestStaleAnswer` |
| no stale note | `TestStaleAnswer`, `cache.txtar` |
| `stale` not in the JSON | `TestStaleAnswer`, `cache.txtar` |
| `fetched_at` always now | `cache.txtar` |
| `cache clear` clears nothing | `TestCacheClear`, `cache.txtar` |

## What I'm taking away

- A cache is HTTP middleware. As an `http.RoundTripper` it answers with a response it built
  itself, and nothing above it changes.
- An `ETag` turns "throw it away" into "ask if it's still right", and a `304` is nearly free.
- What the cache must not hide matters as much as what it serves: only "Quantic isn't there"
  falls back, never Quantic saying no.
- `flock` is advisory, released by the kernel when the process dies, and per open file. Polling it
  with `LOCK_NB` lets a context end the wait.
- `//go:build` picks a file per platform; two files with the same functions are the whole
  abstraction.
- Time is a dependency like any other: a field with a default, set by tests and never by the binary.
- Breaking the code found dead code again, and thinking about failure found a cache that could
  fail a command. A cache that can't be used should just be no cache.

Next: S2, `/api/v1/upcoming` in Quantic, then milestone 5, `upcoming` and `status`, concurrently.
