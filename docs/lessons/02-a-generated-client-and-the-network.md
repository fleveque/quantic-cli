# Lesson 02 — A generated client, and the network

**Milestone 2** — the first commands that talk to Quantic: `calendar`, `stock` and `search`, all
public, all signed out. The HTTP client isn't written by hand. It's generated from the OpenAPI
document Quantic publishes, and wrapped in a few dozen lines that decide what a failure means. This
milestone is about what Go gives you for talking to a server: `net/http`, `context`, and error values
that say where they came from.

*Also readable as a [formatted page](https://claude.ai/artifact/7kkCVCorLUBF6cjJFduzb5). The code, file by file:
[walkthrough](https://claude.ai/artifact/RZgW6D3BNvvxrqLVpW6jCZ).*

---

## What landed

```
api/openapi.json                         a copy of Quantic's OpenAPI document
internal/api/
  doc.go                  package api    the package doc and the go:generate line
  oapi-codegen.yaml, overlay.yaml        how the client is generated
  api.gen.go                             generated: types and a client for every endpoint
  client.go                              the wrapper the commands use; typed errors
  retry.go                               retrying 429s, as an http.RoundTripper
internal/cli/
  client.go               package cli    fetch: one call under --timeout, errors → exit codes
  calendar.go, stock.go, search.go       the commands, text and --json
  testdata/api/, testdata/golden/        recorded responses, expected outputs
internal/render/
  table.go                package render tables for people
cmd/quantic/testdata/script/             calendar, stock, search, network
```

```
$ quantic calendar --days 7
EX-DATE     SYMBOL  NAME               SECTOR      FREQUENCY
2026-10-08  MSFT    Microsoft          Technology  quarterly
2026-10-08  JNJ     Johnson & Johnson  Healthcare  quarterly
2026-10-09  AAPL    Apple Inc.         Technology  quarterly
2026-10-10  IBE.MC  Iberdrola          Utilities   semi_annual
$ quantic --timeout 1ms calendar
quantic: Quantic didn't answer within 1ms (--timeout)
$ echo $?
3
```

## Generating the client

In Elixir I'd write the client by hand: a `Req.new(base_url: …)` and a function per endpoint, ten
lines each. Here the server already describes every endpoint in an OpenAPI document, so
`oapi-codegen` turns it into Go: a struct per schema, a function per operation, and the parsing. Two
thousand lines I didn't write and don't edit.

Three things about how it's wired surprised me.

**The generator is a dependency, but not of the binary.** Since Go 1.24, `go.mod` can list tools:

```
go get -tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen
```

That adds a `tool` line and pins the version like any other module, so everyone runs the same
generator with `go tool oapi-codegen`. No global install, no Makefile. The cost is about twenty
`// indirect` lines in `go.mod`: the generator's own dependencies. None of them reach the binary:

```
$ go version -m quantic | grep dep
	dep	github.com/apapsch/go-jsonmerge/v2	v2.0.0
	dep	github.com/google/uuid	v1.6.0
	dep	github.com/oapi-codegen/runtime	v1.7.0
	dep	github.com/spf13/cobra	v1.10.2
	dep	github.com/spf13/pflag	v1.0.9
```

Only packages the compiled code imports are linked. (`google/uuid` comes along because the
runtime packages the generated code imports, for dates and for writing parameters into URLs, handle
UUIDs too.)

**`go:generate` is a comment.** `//go:generate go tool oapi-codegen -config …` in `doc.go` does
nothing during `go build`. `go generate ./...` scans for those comments and runs them, from the
file's directory. That's all it is: a convention for "this is how these files were made". The
generated file is committed, so building never needs the generator, and CI runs `go generate` and
fails if anything changes. The committed code is always what the document makes.

**The document is a copy.** `api/openapi.json` is exactly what `quantic.finance` publishes, copied
by hand when a milestone needs something new ([ADR 0002](../decisions/0002-generated-client-from-a-checked-in-spec.md)).
A build that fetched it live would break the day the server changed, instead of showing me a diff.

## What I changed before generating

The server's operation names come from its controllers, so the first generated function was
`QuanticWebAPIV1CalendarControllerIndexWithResponse`. I didn't want to edit the copy, so the changes
go in an [OpenAPI Overlay](https://spec.openapis.org/overlay/v1.0.0.html): a small YAML file of
"at this JSONPath, merge this", which `oapi-codegen` applies before generating.

```yaml
- target: $.paths['/api/v1/calendar'].get
  update: {operationId: GetCalendar}
```

The second change is the one I'd never have guessed. **A JSON number with no `format` becomes a
`float32`.** Quantic's growth rates have seventeen significant digits; a `float32` keeps about seven:

```
$ go run f32.go      # decode 0.044617420086995985 into a *float32, encode it again
{"cagr_5y":0.044617422} 0.0446174219250679
```

So the overlay sets `format: double` on every number, and they become `float64`. Removing that line
and regenerating was a good test of the types: the build failed before any test ran, because the
CLI's own output types say `float64`:

```
internal/cli/stock.go:110:19: cannot use s.Dividend.Cagr5y (variable of type *float32) as *float64 value in struct literal
```

In Elixir, a float is a float, and the precision question never comes up. In Go the size is part of
the type, and a generator has to pick one.

**Afterwards, the overlay went away.** Both problems were really the server's document being vague,
and I own the server. So Quantic now names each operation and says `format: double` on each number,
with a test that a new endpoint can't forget either
([quantic#488](https://github.com/fleveque/quantic/pull/488)). I regenerated from the new document
with no overlay and got the same `api.gen.go`, byte for byte, then deleted `overlay.yaml`. An overlay
is the tool for a document you can't change; for your own, fix the source.

## A pointer means "maybe"

A field the document marks `nullable` comes out as a pointer: `Name *string`. That's how Go spells
"maybe", because every type has a zero value and the zero value is a real value. An empty `string`
is `""`, a zero `float64` is `0`, and neither can mean "Quantic doesn't know". A `*string` can be
`nil`.

Elixir has `nil` for any value, and Ruby too, so I never had to ask for it. In Go I ask for it
field by field, and the type checker makes every reader deal with it. `render.Or(s *string)` is the
one place the tables turn `nil` into `-`.

The CLI's JSON keeps the `null`s: a field Quantic doesn't have is `"name": null`, never missing. A
status bar written in QML shouldn't have to tell "absent" from "null". One tag would quietly break
that:

```go
Name *string `json:"name,omitempty"`   // a nil Name disappears from the output
```

```
FAIL: testdata/script/calendar.txtar:15: no match for `"name": null` found in stdout
```

## A client is a transport

`http.Client` turned out to be smaller than I expected. It handles redirects and cookies; sending is
done by its `Transport`, an `http.RoundTripper`. That interface has one method:

```go
RoundTrip(*http.Request) (*http.Response, error)
```

and anything that implements it can wrap another one. That's middleware, the same idea as a Plug
pipeline or a Rack stack, but around the client instead of the server. The retry for 429 Too Many
Requests lives there:

```go
type retryTransport struct {
	next     http.RoundTripper
	attempts int
	base     time.Duration
}
```

It calls `next.RoundTrip`, and when the answer is a 429 it waits and calls it again, up to three
times. The generated client never knows. The wait is the server's `Retry-After` when it sends one;
otherwise it's a doubling backoff with jitter (a random amount between half and one and a half
times), so clients that were limited together don't all retry at the same moment. The test checks
that 100 waits aren't all the same, because "between 0.5s and 1.5s" alone would pass without any
jitter:

```
retry_test.go:113: 100 waits took 1 distinct values: no jitter?
```

The testscript checks the count from the outside: the fake server logs every request, and a 429 on
every try must log exactly three.

```
FAIL: testdata/script/network.txtar:8: have 1 matches for `^GET /api/v1/calendar`, want 3
```

## context: one deadline for the whole call

`--timeout 10s` means the whole command, not each request. In Go that's a `context.Context`:

```go
ctx, cancel := context.WithTimeout(cmd.Context(), opts.Timeout)
defer cancel()
```

The context goes into the call, the generated client attaches it to the request, and everything
below it sees the same deadline: the dial, the TLS handshake, reading the body, and the retry's
waits. There's no `Timeout` on the `http.Client` at all. In Elixir I'd set `receive_timeout` on
each request, and a retry loop would quietly multiply it.

Because the retry can see the deadline (`req.Context().Deadline()`), it doesn't start a wait it
can't finish. If Quantic says "retry in 30 seconds" and 10 are left, the 429 goes back straight
away, and the command exits 5, "rate limited", instead of waiting 10 seconds to exit 3,
"timed out". Taking that check out is caught:

```
retry_test.go:86: context deadline exceeded
```

`defer cancel()` looked like ceremony until I read why: a context with a deadline holds a timer
until its deadline or until it's cancelled. Cancelling when the function returns frees it at once.
`go vet` warns when the `cancel` is dropped.

## Errors that say where they came from

A command needs to know which failure it got, because each one is a different exit code. Three kinds
can come out of `internal/api`:

- **`*UnreachableError`**: no answer at all. The standard library reports any failure to send as a
  `*url.Error`, so `requestError` checks for that with `errors.AsType` and wraps it. Its message
  drops the `Get "http://…/api/v1/calendar?days=45":` prefix `url.Error` adds, because it repeats
  the command line:

  ```
  quantic: can't reach Quantic at 127.0.0.1:1: dial tcp 127.0.0.1:1: connect: connection refused
  ```

- **`*StatusError`**: Quantic answered, but not with a 200. It carries the status, the API's stable
  `code`, and its message. A 502 from a proxy, which is HTML and not JSON, gets
  `Quantic answered 502 Bad Gateway, not JSON` instead of a JSON decoding error.
- **A deadline**, which is neither: `errors.Is(err, context.DeadlineExceeded)` finds it through
  every layer of wrapping, `url.Error` included.

The exit codes aren't decided in `internal/api`. `internal/cli` maps each kind to one in
`exitErrorFor`, because the API package shouldn't know it lives inside a CLI. It's the same split as
in milestone 1: the code underneath returns what happened, and one place decides what it means.

## Generics, twice, small

I used type parameters twice, and both times they removed a copy-paste rather than adding cleverness.
The generated client puts each status in its own field (`JSON200`, `JSON404`, …), one response type
per endpoint. One function turns any of them into "the value or the error":

```go
func result[T any](v *T, resp *http.Response, body []byte) (*T, error)
```

In the CLI, `fetch[T]` builds the client, applies `--timeout`, makes the call and maps the error,
whatever the call returns. In Ruby either would be duck typing and never come up. In Go before 1.18,
it would have been three copies, or an `interface{}` and a type assertion.

## An embedded struct in JSON

Every data command's `--json` starts with the same three keys: `schema`, `fetched_at`, `stale`
(design §5). They're one struct, embedded without a field name:

```go
type calendarOutput struct {
	envelope
	From  string         `json:"from"`
	…
}
```

`encoding/json` writes an embedded struct's fields as if they were the outer struct's own, in the
place it's embedded, so the shared keys come first in every output. It isn't inheritance: there's
no `calendarOutput is an envelope`. There's a field whose name is its type, and the JSON encoder
flattens it. `stale` is always `false` until the cache arrives in milestone 4. It's in the shape now
so no program ever has to check whether it's there.

## Testing against a fake Quantic

Three levels, each with its own fake server, all `net/http/httptest`, which starts a real HTTP
server on a loopback port in a few lines:

- **`internal/api`**: a server per test, answering one status and body. 404 with JSON, 429 every time,
  502 as HTML, a closed port, a server that never answers. This is where the error kinds are pinned.
- **`internal/cli`**: a server that serves recorded responses from `testdata/api/`, and **golden
  files** in `testdata/golden/`, one per output shape and table. `go test ./internal/cli -update`
  rewrites them, and the diff is reviewed like code. `fetched_at` is the only part that changes
  between runs, so the test replaces it before comparing. The recordings are public market data,
  fetched signed out; the one with every field missing is written by hand.
- **`cmd/quantic`**: testscript's `Setup` starts a server for each script, at `$QUANTIC_URL`, that
  answers with **raw HTTP responses written in the script itself**:

  ```
  -- quantic/api/v1/calendar --
  HTTP/1.1 429 Too Many Requests
  Content-Type: application/json
  Retry-After: 0

  {"error": {"code": "rate_limited", "message": "…"}}
  ```

  The server reads the file with `http.ReadResponse`, the same parser an HTTP client uses on the
  wire. Each script says exactly what Quantic sends, status line and headers included, in a form
  anyone who has used `curl -i` can read.

A side note that cost me ten minutes: every test package took about a second longer under `-race`,
even with nothing in it. The race detector sleeps for a second when the program exits
(`atexit_sleep_ms`, to let racing goroutines report):

```
$ go test -race -count=1 ./internal/render
ok  	github.com/fleveque/quantic-cli/internal/render	1.009s
$ GORACE=atexit_sleep_ms=0 go test -race -count=1 ./internal/render
ok  	github.com/fleveque/quantic-cli/internal/render	0.005s
```

## Breaking it on purpose

Each guarantee, broken once, and what caught it. All of them were run against this milestone's code
(the three that first failed to compile were redone so they compiled).

| Broken | Caught by |
|---|---|
| never retry a 429 | `TestRetryTransport`, `network.txtar` |
| retry a fourth time | `network.txtar` (`grep -count=3`) |
| wait past the deadline | `TestRetryTransportStopsBeforeTheDeadline` |
| no jitter | `TestRetryAfter` |
| ignore `Retry-After` | `TestRetryAfter`, `TestRetryTransportStopsBeforeTheDeadline` |
| unreachable as a plain error | `TestUnreachable` (api) |
| the full URL in the unreachable message | `TestUnreachable` (api) |
| no `User-Agent` | `TestCalendar` |
| any URL scheme accepted | `TestNewRejectsWhatIsntAnHTTPURL` |
| a timeout reported as unreachable | `TestAPIFailures/no_answer_in_time` |
| 429 exits 1 | `TestAPIFailures/rate_limited`, `network.txtar` |
| `--timeout` not applied | `TestAPIFailures/no_answer_in_time` (hangs until `go test -timeout`) |
| `--days 0` accepted | `TestCommandUsage` |
| 404 keeps the API's message | `TestAPIFailures/unknown_symbol`, `stock.txtar` |
| `stale` dropped from the envelope | `TestGolden`, `calendar.txtar` |
| `fetched_at` not in UTC | `TestFetchedAtIsNowInUTC` |
| `omitempty` on a nullable field | `calendar.txtar` |
| search words not joined | `TestGolden/search-nothing.txt`, `search.txtar` |
| padded names not collapsed | `TestGolden` |
| numbers as `float32` | the compiler |
| `api.gen.go` edited by hand | CI's `go generate` check |

## What I'm taking away

- Generate the client from the server's own document, commit the result, and let CI check it's
  current. `go tool` pins the generator without putting it in the binary.
- Read what the generator chose. `number` became `float32`, and only the types caught it. When
  the document is yours, fix it there rather than patching it in the client.
- In Go, "maybe" is a pointer, asked for field by field. `omitempty` decides whether `nil` is
  `null` or missing, and a contract should say which.
- An `http.Client` is a `RoundTripper` with extras, and a `RoundTripper` wrapping another is
  middleware. Retrying lives there.
- One `context` deadline covers the whole call, waits included. Code that can see the deadline can
  refuse to start what it can't finish.
- Errors carry their kind (`*url.Error`, my `StatusError`), and `errors.Is` / `errors.AsType` find
  it through any wrapping. The package that knows it's a CLI picks the exit code.

Next, milestone 3: signing in, the keyring, and your own portfolio.
