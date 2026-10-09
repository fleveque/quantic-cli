# Lesson 03 — A token, the keyring, and your own data

**Milestone 3** — signing in. `quantic auth login` takes a personal API token, checks it with
Quantic, and keeps it in the system keyring. Then `portfolios`, `holdings`, `dividends` and `income`
read your own data. Most of the code is plumbing: where a secret lives on a desktop, how it gets
there, and how to make sure it never comes back out on a screen. This milestone is about Go's small
interfaces, the operating system underneath, and a type that refuses to be printed.

*Also readable as a [formatted page](https://claude.ai/artifact/AKBiEL9M7YYDTGskQD8md9). The code, file by file:
[walkthrough](https://claude.ai/artifact/2SYakbzwsUHAzUKasqxrre).*

---

## What landed

```
internal/auth/
  secret.go               package auth   Secret: a token fmt can't print
  store.go                               where a token is found, kept, and forgotten
  keyring.go                             SystemKeyring: go-keyring behind a small interface
  authtest/keyring.go     package authtest  keyrings for tests: in memory, or in a file
internal/api/client.go                   the token as a Bearer header; five signed-in endpoints
internal/cli/
  client.go               package cli    a session: where Quantic is, and which token
  auth.go                                auth login, logout, status
  portfolios.go, holdings.go,
  dividends.go, income.go                your own data, as tables and --json
cmd/quantic/main_test.go                 scripts get a keyring of their own
docs/decisions/0003-where-the-token-lives.md
```

Two new dependencies: `zalando/go-keyring` for the keyring, and `golang.org/x/term` to read a pasted
token without showing it. `golang.org/x/...` is maintained by the Go team, outside the standard
library's compatibility promise.

```
$ quantic auth login --with-token < token.txt
Signed in to localhost:4000 as Demo Investor <demo-u4hNjybg@demo.quantic.invalid>.
$ quantic dividends --symbol ko --from 2026-07-01
DATE        SYMBOL  NAME           GROSS        NET  PORTFOLIO
2026-10-15  KO      Coca-Cola  58.50 USD  49.72 USD  Main
2026-07-15  KO      Coca-Cola  58.50 USD  49.72 USD  Main
```

(Every output here is real, from a local Quantic and its demo user, never a real portfolio.)

## The keyring is an interface I wrote

The CLI needs three things from a keyring: get, set, delete. So that's the interface:

```go
type Keyring interface {
	Get(service, user string) (string, error)
	Set(service, user, secret string) error
	Delete(service, user string) error
}
```

`go-keyring` has package functions with exactly those signatures (`keyring.Get`, `keyring.Set`…).
`SystemKeyring` is an empty struct whose methods call them, and translate one error: go-keyring's
`ErrNotFound` becomes `auth.ErrNotFound`, so nothing else in the CLI imports go-keyring.

What surprised me coming from Elixir is who declares what. A behaviour in Elixir is declared by the
module that will call it (`@callback`), and every implementing module says `@behaviour Keyring`. In Go,
nobody says anything. `SystemKeyring` doesn't mention `Keyring`; it has the three methods, so it is
one. The interface lives where it's used, in `package auth`, sized to what `auth` needs, which is
the Go habit: "accept interfaces, return structs." Ruby's duck typing is close, but Ruby finds out at
the call. Go finds out at compile time, at the line that hands a `SystemKeyring` to something wanting
a `Keyring`.

Three types satisfy it: `SystemKeyring`, and two in `authtest` for tests. Which one the program
uses is decided once, at the top:

```go
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, build Build) int {
	return RunWith(args, stdin, stdout, stderr, build, auth.SystemKeyring{})
}
```

`main` calls `Run`. Tests call `RunWith` with their own. That isn't a framework, it's an argument.

## The day a test reached my keyring

I first planned to keep tests away from the keyring by clearing the environment: no
`DBUS_SESSION_BUS_ADDRESS`, so no D-Bus, so no Secret Service. To try it, I ran the real binary that
way against a local Quantic:

```
$ unset DBUS_SESSION_BUS_ADDRESS; export XDG_RUNTIME_DIR=/tmp/empty
$ quantic auth login --with-token < token.txt
Signed in to localhost:4000 as Demo Investor <demo-u4hNjybg@demo.quantic.invalid>.
$ quantic auth status | tail -1
Token         in the system keyring
```

In the keyring. Mine. `godbus`, the D-Bus library under go-keyring, falls back to
`/run/user/<uid>/bus` when the variable is missing, whatever `XDG_RUNTIME_DIR` says: it builds the
path from the user id. The token was a throwaway demo user's on a local server, and it's gone now,
but the lesson is that the environment is not a fence. A private `dbus-run-session` isn't either:
the gnome-keyring it starts finds the one already running and defers to it.

testscript clears the environment for every script, so the scripts would have done the same thing on
every `go test`. What keeps them out now is that no test can reach `SystemKeyring` at all:
`main_test.go` runs `mainForTests`, which calls `RunWith` with a keyring in `$WORK/keyring.json`, and
`internal/cli`'s `run` helper passes an in-memory one. There's a line about it in `CLAUDE.md`.

## A type that can't be printed

"The token never appears in output, logs or errors" was a rule in the design. I wanted it to be a
property of a type, so that the next error message I write can't break it by accident:

```go
type Secret struct {
	value *string
}

func (s Secret) Reveal() string { … }

func (Secret) Format(f fmt.State, _ rune) { io.WriteString(f, "[redacted]") }
```

The token only comes out through `Reveal`, called in one place, the line that sets the
`Authorization` header. A `grep Reveal` is the security review. Everything else is about what
happens when some other code prints a `Secret`, and I got that wrong twice before breaking things
showed me how it works.

**My first version had `value string` and relied on `Format`.** `Format` is `fmt.Formatter`: for any
verb, `fmt` calls it instead of looking inside, so `fmt.Errorf("bad token %v", tok)` writes
`bad token [redacted]`. But `fmt` can't call a method through an unexported field of another
struct, because reflection won't hand out an interface for one. So it prints the fields itself:

```
type holder struct{ tok auth.Secret }   // tok is unexported
fmt.Sprintf("%+v", holder{s})           // with value string:
{tok:{value:qtc_leak}}
```

**The pointer is what fixed it, and it turned out to be the whole guarantee.** Below the top level,
`fmt` prints a pointer as its address, not what it points to. I added the pointer for that one
case. Then, breaking `Format` on purpose, the test complained only that it didn't see
`[redacted]`; nothing had leaked. With `Format` deleted, every verb, nested or not, prints an
address:

```
%v   {0x38e1bda66440} | {{0x38e1bda66440}}
%+v  {value:0x38e1bda66440} | {tok:{value:0x38e1bda66440}}
%#v  auth.Secret{value:(*string)(0x38e1bda66440)} | auth_test.holder{tok:auth.Secret{value:(*string)(0x38e1bda66440)}}
%x   {38e1bda66440} | {{38e1bda66440}}
```

So `Format` stays for people, since `[redacted]` in an error message says what happened and an
address doesn't, and the pointer does the protecting. It's the strangest reason I've had for a
pointer: not sharing, not mutation, not "maybe" (lesson 02), just hiding.

**encoding/json can't see it at all.** The field is unexported, and `encoding/json` only encodes
exported fields, so a `Secret` encodes as `{}`. I'd also written a `MarshalText` method for JSON and a
`LogValue` for `log/slog`. Breaking each showed neither did anything: slog's text handler prints
through `fmt`, and its JSON handler is `encoding/json`. I deleted both.

In Elixir I'd reach for a struct with `@derive {Inspect, except: [:token]}`. That's the same idea,
but it covers only `inspect`, and here `fmt` is everything that prints.

## Files that only you can read

Without a keyring (a server, a container, a desktop with no Secret Service), the token goes to
`tokens.json` under `os.UserConfigDir()`, which is `$XDG_CONFIG_HOME`, or `~/.config`, on Linux. A file
with a secret in it has three rules:

1. **Never readable by others, not even for a moment.** `os.WriteFile(path, data, 0o600)` is wrong for
   a file that already exists: the mode only applies when the file is created. So the token is
   written to a new temporary file next to it, `os.CreateTemp`, which is `0600` from birth, then
   `os.Rename`d over the old one. On the same filesystem, a rename is atomic: a crash leaves the
   old file or the new one, never half of either. The directory is `0700`.
2. **Refuse it if others can read it.** ssh refuses a private key that's group- or world-readable, and
   so does this:

   ```
   if perm := info.Mode().Perm(); private && perm&0o077 != 0 {
   ```

   `0o077` is the group and other bits, the leading `0o` is Go's octal, and `&` masks them.
3. **…but let people fix it.** My first version refused the file everywhere, and the test script
   found that `auth logout` then couldn't remove the token from it, which is exactly what you'd want
   to do with a leaked file. Reading a token to use it checks the mode; reading the file to replace or
   remove one doesn't.

In Elixir I'd have written the file and then called `File.chmod/2`, which leaves a moment, between
the two, when anyone can read it. I'd never had to think about that moment.

## Reading a secret from a terminal

`quantic auth login` asks for the token without echoing it, as `sudo` does:

```go
f, isFile := in.(*os.File)
atTerminal := isFile && term.IsTerminal(int(f.Fd()))
```

`in` is an `io.Reader`, because tests pass a `strings.Reader`. `in.(*os.File)` is a type assertion:
"if what's in this interface is an `*os.File`, give it to me." With the two-value form it never
panics. Only a real file has a descriptor to ask the terminal about, and `term.ReadPassword` turns
echo off on it for the read.

When stdin isn't a terminal, it reads stdin only with `--with-token`, as `gh` does. Otherwise a script
that forgot to pipe the token would wait forever for a paste:

```
$ quantic auth login < token.txt
quantic: standard input isn't a terminal; to read the token from it, add --with-token
Run 'quantic --help' for usage.
```

And `--with-token` reads at most 4 KiB, through `io.LimitReader`, so `< /dev/zero` ends.

## Checked before it's kept

`login` calls `GET /api/v1/me` with the new token before storing it:

```
$ echo qtc_typo | quantic auth login --with-token
quantic: localhost:4000 doesn't accept that token, so it wasn't stored. Make one at http://localhost:4000/settings#api-tokens-settings
```

Every 401 after that says which token was refused and what to do. A stored token: sign in again. One
from `QUANTIC_TOKEN`: make a new one, since signing in won't change the variable:

```
$ QUANTIC_TOKEN=qtc_revoked quantic holdings
quantic: localhost:4000 rejected the token in QUANTIC_TOKEN; it may have been revoked. Make a new one at http://localhost:4000/settings#api-tokens-settings
```

Tokens are kept per host (`quantic.finance`, `localhost:4000`), so the token for my local Phoenix
never replaces the real one.

## Converting between twin structs

The generated client has `api.Money{Amount, Currency string}` with JSON tags and doc comments. The
CLI's output has its own `money` with the same two fields. To go from one to the other:

```go
AverageCost:    money(h.AverageCost),
WithholdingTax: (*money)(d.WithholdingTax),
```

That's a conversion, not a constructor. Go allows it between struct types with the same fields in
the same order, and since Go 1.8 the tags don't have to match. It works through pointers too, so a
`*api.Money` that may be `nil` becomes a `*money` that may be `nil`, with no `if`. I'd written
`&money{a.Amount, a.Currency}` with a nil check in milestone 2. This is better, and if Quantic ever
adds a field to `Money`, the conversion stops compiling and tells me.

## A map has no order, in either language

Quantic sends a typical year's income as an object keyed by month, `"1"` to `"12"`. The CLI's JSON
turns it into a list, because a status bar wants January first. The first way that comes to mind,
ranging over the map, gives this on three runs of a twelve-key Go map:

```
1 3 4 5 6 7 8 9 2 10 11 12
1 3 4 5 6 8 9 10 2 7 11 12
6 7 9 10 1 2 4 8 11 12 3 5
```

Go randomizes map iteration on purpose, so that nobody comes to depend on an order it never promised.
Elixir doesn't randomize, which is worse in a way. A small map comes out sorted, and with string keys
sorted means this, every time:

```
1 10 11 12 2 3 4 5 6 7 8 9
```

Stable and wrong, so a test might well pin it. Go's version comes out in a different order almost
every run, so the golden test fails, which is how the break below got caught. The code loops `month := 1; month <= 12` and looks
each one up.

## Testing it without touching anything real

- **Unit tests** (`internal/auth`) use `authtest.Keyring`: a map behind a mutex, with an `Err` field
  that makes every call fail like a missing Secret Service. One test uses go-keyring's own
  `MockInit` to check `SystemKeyring`'s error translation. That mock is process-wide, so it lives in
  one test, not as the strategy.
- **Command tests** run `cli.RunWith` with buffers and a keyring. Their fake Quantic answers 401
  without `Bearer qtc_test` and 422 for a portfolio it has no file for, as the real one does. The
  responses are recorded from a demo user on my local Quantic, trimmed, plus one hand-written sparse
  income for the edge cases.
- **Scripts** run the real binary as separate processes, so the keyring has to outlive a process:
  `authtest.FileKeyring` keeps it in `$WORK/keyring.json`. `auth.txtar` signs in, checks the keyring
  file, sends commands, logs out, then does it all again with `TEST_KEYRING=none` and checks the
  token file is `0600` with a new `mode` command, since testscript has no `stat`. The fake logs each
  request's `Authorization` header, so a script can `grep` for exactly which token was sent.
- **`TestTheTokenIsNeverPrinted`** runs every command, with `--json` and without, through every
  failure: 401, 429, 500, 403, nothing listening, a malformed token, a token file others can read.
  Each run uses the token `qtc_SECRET_never_print_me`, and the test fails if `SECRET_never_print_me`
  appears anywhere on stdout or stderr.

## Breaking it on purpose

Each guarantee, broken once, and what caught it. Two breaks were first written wrong and changed
nothing (shown as fixed). Two found code that guaranteed nothing, and that code is gone.

| Broken | Caught by |
|---|---|
| `Format` removed | `TestSecretNeverPrints`, which wants `[redacted]`; the pointer still hid the token |
| the token as a plain `string`, not behind a pointer | `TestSecretNeverPrints` (`{tok:{value:qtc_supersecret}}`) |
| `MarshalText` removed | nothing: `encoding/json` never saw the token; method deleted |
| `LogValue` removed | nothing: slog prints through `fmt` or `encoding/json`; method deleted |
| spaces allowed in a token | `TestParseSecret`, `TestLoginUsage`, `TestEnvWinsOverWhatIsStored` |
| the keyring before `QUANTIC_TOKEN` | `TestEnvWinsOverWhatIsStored`, `TestGolden` |
| token file `0644` | `TestWithoutAKeyringTheTokenGoesToAPrivateFile`, `TestLoginWithoutAKeyring`, `auth.txtar` |
| token directory `0755` | `TestWithoutAKeyringTheTokenGoesToAPrivateFile`, `auth.txtar` |
| a readable file not refused | `TestAFileOthersCanReadIsRefused`, `TestTheTokenIsNeverPrinted`, `auth.txtar` |
| a readable file refused on logout too | `TestAFileOthersCanReadIsRefused`, `auth.txtar` |
| the file's copy left after a keyring save | `TestSavingToTheKeyringRemovesTheFilesCopy` |
| a failing keyring on logout reported as "nothing stored" | `TestDelete` |
| go-keyring's `ErrNotFound` let through | `TestSystemKeyring` |
| no `Authorization` header | `TestToken`, `TestGolden`, `TestDividendsFilters` |
| `portfolios` without a token | `TestNotSignedIn` (it asked Quantic) |
| public commands don't send the token | `TestPublicCommandsSendTheTokenIfAny`, `auth.txtar` |
| 401 exits 1 | `TestTokenRejected`, `TestLoginRejectsATokenQuanticDoesnt`, `portfolio.txtar` |
| the rejection message shows the token | `TestTheTokenIsNeverPrinted`, `TestTokenRejected` |
| 422 exits 1 | `TestUnknownPortfolio`, `portfolio.txtar` |
| login stores before checking | `TestLoginRejectsATokenQuanticDoesnt` |
| login reads stdin without `--with-token` | `TestLoginUsage`, `auth.txtar` |
| no warning when the token goes to a file | `TestLoginWithoutAKeyring`, `auth.txtar` |
| `--symbol` not upper-cased | `TestDividendsFilters`, `portfolio.txtar` |
| `--to` before `--from` allowed | `TestCommandUsage` |
| months in map order | `TestGolden` (income, run 5 times) |
| missing lists as `null` | `TestGolden/income-Sparse.json` |
| amounts not right-aligned | `TestGolden` (six tables) |
| scripts with the real `XDG_CONFIG_HOME` | `auth.txtar` |

## What I'm taking away

- An interface in Go is declared by the code that needs it, as small as it needs, and satisfied
  without anyone saying so. Swapping the real thing for a test one is an argument, not a framework.
- The environment is not a fence. Libraries find the user's session in ways you don't see, so a test
  that must not touch something must not be able to call it.
- A pointer in an unexported field makes "never print this" a property of a type: `fmt` shows an
  address, `encoding/json` shows nothing. `fmt.Formatter` makes it say `[redacted]`.
- A file's mode is set when it's created. A secret file is written new, as `0600`, and renamed into
  place.
- Struct conversion between twins (`money(apiMoney)`, `(*money)(p)`) is free, and stops compiling when
  they drift apart.
- Maps have no order. Go makes sure you notice; Elixir lets you get away with it until the keys
  are strings.
- Breaking the code finds dead code as well as missing tests, and sometimes a wrong explanation. Two
  methods I'd written "to be safe" didn't make anything safer, and the one I thought did the work
  didn't.

Next, milestone 4: a cache, so the status bar keeps working when the network doesn't.
