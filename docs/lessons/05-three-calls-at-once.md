# Lesson 05 — Three calls at once, and what to do when one fails

**Milestone 5**: `upcoming` and `status`. `upcoming` is the endpoint S2 added to Quantic: your
holdings' next ex-dates and payments, with what each should pay. `status` is the call the bar
widget will make every ten minutes. It asks Quantic three things at the same time and turns the
answers into one document. If one of the three fails, it still shows the other two. This
milestone is about goroutines, about waiting for them, and about deciding what partial failure
means. It's also about two small Go details: money in a slice, and the difference between `nil`
and empty.

*Also readable as a [formatted page](https://claude.ai/artifact/LESSON05). The code, file by file:
[walkthrough](https://claude.ai/artifact/WALKTHROUGH05).*

---

## What landed

```
internal/cli/
  upcoming.go                            quantic upcoming
  status.go                              quantic status: three calls, one document
  client.go                              askQuantic: a call that doesn't warn; upcoming fresh for 1h
  status_internal_test.go                the first test inside package cli
api/openapi.json, internal/api/          regenerated: ListUpcoming
cmd/quantic/testdata/script/status.txtar signed out, the requests, the cache, offline
```

No new dependencies. Against a local Quantic and its demo user:

```
$ quantic upcoming --days 14
EX-DATE     PAY DATE    SYMBOL  NAME               SHARES   PER SHARE    EXPECTED         NET  PORTFOLIO
2026-10-17  -           O       Realty Income         200  0.2736 USD   54.72 USD   46.51 USD  Main
2026-10-17  -           O       Realty Income          40  0.2736 USD   10.94 USD    9.30 USD  Pension
2026-10-19  -           JNJ     Johnson & Johnson      36  1.3643 USD   49.11 USD   41.74 USD  Main
2026-10-19  2026-12-10  MSFT    Microsoft           radar  0.9652 USD           -           -  -
2026-10-20  -           AAPL    Apple Inc.             48   0.276 USD   13.25 USD   11.26 USD  Main
2026-10-21  -           IBE.MC  Iberdrola             900  0.3419 EUR  307.71 EUR  307.71 EUR  Pension
2026-10-27  -           PG      Procter & Gamble    radar  1.1043 USD           -           -  -
2026-10-28  -           KO      Coca-Cola             130  0.5398 USD   70.17 USD   59.64 USD  Main

$ quantic status
Next     O goes ex-dividend in 3 days (2026-10-17), 65.66 USD
October  nothing received yet; 138.60 EUR in a typical October, after withholding

The next 14 days
EX-DATE     PAY DATE    SYMBOL  NAME               SHARES   PER SHARE    EXPECTED         NET  PORTFOLIO
...
```

`radar` is a stock you're watching but don't hold: it has a date, but nothing to pay you. Realty
Income is held in two portfolios, so it appears twice in the table. As "next", it's one event:
54.72 + 10.94 = 65.66.

## `upcoming` was the easy half

Quantic does the work for `upcoming`, and the CLI only had to catch up with it. I fetched the
OpenAPI document again from production, which now has `/api/v1/upcoming`, and ran `go generate`.
That gave `ListUpcomingWithResponse` and an `Upcoming` type. From there it was the same three
steps as every other command: a method on `api.Client`, an output type with its own
`quantic.cli/upcoming/v1` schema, and a table.

`upcoming` also got one line in `freshFor`: an hour. Dates and dividend rates don't move by the
minute, and the widget's whole job is to ask often.

## Goroutines, and waiting for them

`status` needs three answers: the next 14 days (`/upcoming`), what this month has paid
(`/dividends` from the 1st to today), and what a month like this one usually pays (`/income`).
One at a time, the slowest case is the sum of the three. At the same time, it's the slowest of the
three.

In Elixir I'd write:

```elixir
tasks = [Task.async(fn -> upcoming() end), Task.async(fn -> income() end), Task.async(fn -> dividends() end)]
[up, inc, divs] = Task.await_many(tasks)
```

In Go, `go f()` runs `f` on a new goroutine and returns at once. A goroutine is cheap, like a BEAM
process, but it has no mailbox and no result: `go` gives you nothing back to await. Waiting is a
separate thing, a `sync.WaitGroup`:

```go
var wg sync.WaitGroup
wg.Go(func() {
	upcoming.v, upcoming.at, upcoming.err = askQuantic(cmd, opts, s, ...)
})
wg.Go(func() { ... income ... })
wg.Go(func() { ... dividends ... })
wg.Wait()
```

`wg.Go` is new: Go 1.25 added it. Before that, it was `wg.Add(1)`, then `go func() { defer
wg.Done(); ... }()`, and forgetting the `Add` or the `Done` was a classic bug. Each function
closes over its own variable, `upcoming`, `income` or `dividends`, and writes only that one.

That's what makes it safe. Three goroutines writing three different variables never touch the
same memory. `wg.Wait()` does more than wait: Go's memory model guarantees that everything a
goroutine wrote before it finished is visible after `Wait` returns. Without that guarantee,
reading `upcoming.v` afterwards would be a data race even though the goroutine had finished.
`go test -race` checks this at run time, and the whole suite runs under it.

There's no channel here. I'd expected Go concurrency to mean channels everywhere, and it's often
written that way, but when each piece of work has its own slot to fill, a channel is ceremony.

## Why not errgroup

The design said `errgroup`, from `golang.org/x/sync`, which the module already had as an indirect
dependency. `errgroup.Group` is a `WaitGroup` whose functions return an error, and `Wait` returns
the first one. With `errgroup.WithContext`, the first error also cancels the context the others
are using.

That's exactly right for "all or nothing", and exactly wrong here. The design (§8) says a partial
failure returns what it has, with an `errors` list, and exits 0. Income failing must not cancel
upcoming. So each part keeps its own error, and the rule comes after `Wait`:

- **all three failed**: nothing to show. Exit as the first did, the same as any other command:
  3 offline, 4 for a revoked token, 2 for an unknown `--portfolio`. It's nearly always the same
  reason three times.
- **some failed**: the parts that worked; the ones that didn't are null (or empty) and listed in
  `errors`, with a line each on stderr; exit 0.

With errgroup I'd have had every function return `nil`, and then it's a `WaitGroup` with an extra
import. I updated the design to say so.

## One goroutine, one slot: a generic struct

Each part is a value, when it was fetched, and an error, and the three values are different types.
A small generic struct holds any of them:

```go
type statusPart[T any] struct {
	v   *T
	at  fetched
	err error
}

var upcoming statusPart[api.Upcoming]
var income statusPart[api.Income]
```

Lesson 02's `fetch[T]` was a generic function. This is a generic type, and it's the same `[T any]`.

## stderr is shared

Each call can be answered from the cache when Quantic can't be reached. `fetch` then prints
`quantic: can't reach …` on stderr. Three goroutines doing that would mean three lines for the
widget's log, written at the same time to one writer. In the tests that writer is a
`bytes.Buffer`, which isn't safe for concurrent use.

So `callQuantic` split in two. `askQuantic` makes the call and says nothing. `callQuantic` is
`askQuantic` followed by `warnStale`. `status` calls `askQuantic` from the goroutines and writes to
stderr only after `Wait`, from one goroutine. It warns once, with the oldest time: a document is
as old as its oldest part, and stale if any part is. The script checks that with a mix: at 09:50
the cached `upcoming` (fresh for an hour) is still fresh, while income and dividends (fresh for
15 minutes) are stale copies.

```
stdout '"fetched_at": "2026-10-15T09:00:00Z",\n  "stale": true'
stderr -count=1 '^quantic: '
```

## A test that knows it ran at once

How do you test that something is concurrent? The test's fake Quantic doesn't answer anything
until all three requests have arrived:

```go
mu.Lock()
if arrived++; arrived == 3 {
	close(all)
}
mu.Unlock()
select {
case <-all:
	return false // pass it on to the fake Quantic
case <-time.After(2 * time.Second):
	w.WriteHeader(http.StatusTeapot)
	return true
}
```

Closing a channel wakes everyone waiting on it, so the third arrival releases all three. To break
it, I swapped the `WaitGroup` for one that runs each function right away (`Go(f) { f() }`). Then
the first request waits alone and times out, and so does the second:

```
--- FAIL: TestStatusAsksAtOnce (4.00s)
    status_test.go:60: exit 0, stderr "quantic: no upcoming: Quantic answered 418 I'm a teapot, not JSON\nquantic: no income: Quantic answered 418 I'm a teapot, not JSON\n"; want 0 and nothing
```

That one failure shows two behaviours. It took 4 seconds, two waits of 2, so the calls ran one
after the other. And it still exited 0 with the third part: partial failure working as designed.

## Money in more than one currency

The design had `this_month.received` as one amount. But `/dividends` gives each dividend in the
currency it was paid in, with no exchange rates. The demo user got 58.50 GBP from Diageo and
USD from three others in the same month. I asked rather than guess, and the answer was a list,
one sum per currency:

```json
"received": [
  {"amount": "49.72", "currency": "GBP"},
  {"amount": "141.52", "currency": "USD"}
],
"projected": {"amount": "138.60", "currency": "EUR"},
"net_of_withholding": true
```

`projected` is in your preferred currency, and Quantic makes it gross or net according to a
setting. `received` is summed on the same basis, so the two compare like with like, and
`net_of_withholding` says which.

The sums are exact. Amounts are decimal strings, and as `float64`s, `0.1 + 0.2` is
`0.30000000000000004`. `math/big.Rat` is an exact fraction: `SetString("0.1")` is 1/10, adding
fractions is exact, and `FloatString(2)` writes it back with two decimals. Ruby has `BigDecimal`
and Elixir has the `Decimal` library. Go has `big.Rat` in the standard library, and it's enough
for adding. Breaking `addMoney` to use floats fails `TestAddMoney` at the first case, `0.1 + 0.2`.

## nil isn't empty

The first real run, against the local Quantic, printed this:

```json
"this_month": {"month": "2026-10", "received": null, "projected": {...}}
```

with no errors. The demo user hadn't been paid anything yet in October, so there was nothing to
sum. But in this contract `null` means "that part failed", and the text output had quietly left
the line out.

The culprit was one line:

```go
out := slices.Collect(maps.Values(sums))
```

`slices.Collect` of an empty iterator returns a `nil` slice. In Go, a `nil` slice and an empty one
behave the same almost everywhere (`len` is 0, `range` does nothing, `append` works), so the
difference is easy to forget. `encoding/json` is the exception: it writes `nil` as `null` and an
empty slice as `[]`. Every list in these outputs is initialised with `make` for this reason (`income`
has `orEmpty` for the same thing), and I still missed it. The fix is the same `make`:

```go
out := slices.AppendSeq(make([]money, 0, len(sums)), maps.Values(sums))
```

The fixtures had dividends in them, so no test could see this. Now one does:

```
--- FAIL: TestSumByCurrencyOfNothing (0.00s)
    status_internal_test.go:84: got []cli.money(nil), <nil>; want an empty list
```

## A test inside the package

Until now every test in `internal/cli` was in `package cli_test`. That's a separate package that
can only see what's exported, and in practice it runs the whole CLI through `cli.RunWith`. The
rules for "next" are easier to test directly: a past ex-date whose payment is still to come, the
radar left out, a stock in two portfolios added up. A table of those against the unexported
`nextEvent` is clearer than a fixture file for each one.

So `status_internal_test.go` says `package cli`, and sits in the same directory as
`status_test.go`, which says `package cli_test`. Go allows both side by side: the `go` tool accepts a
second package in a directory only when its name is the first's plus `_test`. I use the outside view for behaviour
and the inside view only for pure functions with many cases.

## Dates, and whose today

"In 2 days" depends on what today is, and that depends on where you are. Quantic's `from` is
today in UTC, but at 00:30 in Madrid it's still yesterday in UTC. `status` takes the date from the
clock as the person running it sees it (`time.Now()` is in their local zone), then makes it
midnight UTC so it compares with the dates Quantic sends, which parse at midnight UTC.

The golden tests run with a fixed clock, `2026-10-15T09:00:00Z`, the day the fixtures were
recorded for. `runAt` passes it in through lesson 04's `Deps.Now`.

## Testing it

- **Golden files** for `upcoming` (table, JSON, one portfolio) and `status` (table and JSON), from
  answers recorded from a demo user on a local Quantic. The fake Quantic now serves a file named
  with the whole query first (`dividends?from=2026-10-01&to=2026-10-15.json`), so `status`'s month
  is its own fixture.
- **`status_test.go`**: the three calls at once, one part failing, all failing (offline, an unknown
  portfolio), signed out, and the exact three requests with `--portfolio`.
- **`status_internal_test.go`**: `nextEvent`, `addMoney`, and the sum of nothing.
- **`status.txtar`**: the real binary signed out, the request parameters, the cache across two
  times, offline with one warning, offline with nothing cached.

## Breaking it on purpose

| Broken | Caught by |
|---|---|
| the three calls one after another | `TestStatusAsksAtOnce` |
| any failure fails the whole | `TestStatusPartialFailure` |
| every part failing still exits 0 | `TestStatusAllFail`, `status.txtar` |
| signed out with `--json` prints nothing | `TestStatusSignedOut`, `status.txtar` |
| the radar counts as your next dividend | `TestNextEvent` |
| past dates count as next | `TestNextEvent` |
| one stock in two portfolios, two events | `TestNextEvent`, golden `status` |
| amounts added as `float64` | `TestAddMoney`, golden `status`, `status.txtar` |
| received always gross | golden `status` |
| a warning per stale part | `status.txtar` |
| the newest time instead of the oldest | `status.txtar` |
| stale only if every part is | `status.txtar` |
| `upcoming` not cached | `status.txtar` |
| `--portfolio` not passed on | `TestStatusRequests`, `TestStatusAllFail`, `status.txtar` |
| the month starting a month ago | golden `status`, `TestStatusPartialFailure`, `status.txtar` |
| radar rows showing shares | golden `upcoming`, `status` |
| nothing received is `null` | `TestSumByCurrencyOfNothing` (found by running it, not by a test) |

## What I'm taking away

- `go` starts work and returns nothing. Waiting is a `WaitGroup`, and `wg.Wait()` is also what
  makes the goroutines' writes safe to read.
- One goroutine, one variable to fill: no channels, no mutex, nothing shared.
- `errgroup` is for all-or-nothing. Partial failure is a decision about what the whole means
  when a part is missing, and it's made after `Wait`, not inside the goroutines.
- Only one goroutine writes to a shared writer. Here that's after `Wait`.
- Money is added as fractions, never floats, and in one currency at a time.
- `nil` and empty are the same slice until `encoding/json` sees them. Running the real thing found
  what the fixtures couldn't.

Next: milestone 6, the Omarchy widget, in its own repository, reading `quantic.cli/status/v1`.
