# Lesson 06 — The other side of the contract

**Milestone 6**: the Omarchy widget, in its own repository,
[omarchy-quantic](https://github.com/fleveque/omarchy-quantic). There's no Go in it: it's QML and
JavaScript running inside Omarchy's shell. This lesson is about the other side of a contract. For
five milestones I wrote `--json` for "a program that reads it". Now I wrote that program, and found
out what the contract needed.

*Also readable as a [formatted page](https://claude.ai/artifact/2DyPcTgPY5PeZsMaDVVwGH). No walkthrough this
time: the walkthroughs are for Go code, and this milestone has none.*

---

## What landed

```
omarchy-quantic/
  manifest.json            what Omarchy needs to know: a bar widget, its settings
  BarWidget.qml            the pill
  Panel.qml                the popup; runs quantic, holds the state
  Model.js                 every decision, in plain JavaScript
  test/model.test.js       node --test, against quantic's golden output
  test/manifest.js         the manifest checks, for CI, which has no Omarchy
  docs/screenshots/        from checking it by hand
```

![The pill](https://raw.githubusercontent.com/fleveque/omarchy-quantic/main/docs/screenshots/pill.png)

Install the CLI, sign in, then:

```
omarchy plugin add https://github.com/fleveque/omarchy-quantic.git --enable
```

## Read the built-in widgets first

The design (§9) said the widget would "follow the pattern of the built-in Tailscale widget". I'd
written that from memory. Before writing any QML I read the real thing, which on Omarchy is on disk:
`~/.local/share/omarchy/shell/plugins/panels/`. The weather widget turned out to be the closer
model. It has a pill, a popup, and a timer, and its `BarWidget.qml` is 83 lines that only draw what
`Panel.qml` decides. The Tailscale one showed how to find a CLI (`which tailscale`) and read its
exit codes (`onExited`). Omarchy's own `Model.js` files end like this:

```js
if (typeof module !== "undefined") {
  module.exports = { ... }
}
```

QML imports the file as a JavaScript library, where `module` doesn't exist. Node `require`s it,
and gets the exports. So the logic can live in one file, with no QML in it, and be tested with
plain node. That one idiom decided how the widget is split up.

The design's other guesses held up. A plugin is a git repository with `manifest.json` at its root;
`omarchy plugin add` clones it; settings are a schema in the manifest. Two details I couldn't have
guessed: there's no boolean setting type, so "show amounts" is an `enum` of `Off`/`On`, as
Omarchy's own Agents widget does it; and settings are changed from a terminal, with `omarchy bar
set <id> <key> <value>`.

## Exit codes are states

On the CLI side, exit codes were a table in the design (§6). On this side, each one is something
the bar has to show:

```js
function parseRun(installed, exitCode, stdout, stderr) {
  if (!installed) return { state: "not_installed", doc: null, message: "" }
  if (exitCode === EXIT_NOT_SIGNED_IN) return { state: "signed_out", ... }
  if (exitCode === EXIT_UNREACHABLE) return { state: "offline", ... }
  ...
```

This is where milestone 1's choice paid off. A widget that had to tell "log in again" from
"offline" by parsing English on stderr would break the first time I reworded an error. Exit codes
4 and 3 don't change. In milestone 5 I'd also made `status --json` print a document with
`"signed_in": false` when signed out. The widget didn't need it, because the exit code already
says so. That's worth knowing: the contract has more in it than its first reader uses.

The schema name earns its place too. A `quantic` that prints `quantic.cli/status/v2` isn't read as
if it were v1, which could show wrong numbers without complaint. It's an error, and the panel says
which version it got:

```
quantic printed quantic.cli/status/v2; this widget reads quantic.cli/status/v1
```

## The golden file, shared

The design (§11) said `Model.js` would be tested "against this repo's golden `status` JSON". But
two repositories can't share a file. So the widget has a copy, `test/fixtures/status.json`, byte
for byte the same as `internal/cli/testdata/golden/status.json`, and its CI checks that they still
match:

```sh
curl -fsS https://raw.githubusercontent.com/fleveque/quantic-cli/main/internal/cli/testdata/golden/status.json -o /tmp/cli-status.json
diff -u /tmp/cli-status.json test/fixtures/status.json
```

A change to `status` here changes the golden file, and `-update` rewrites it. After that, the
widget's CI fails until someone copies the file over and checks the widget still reads it. That
makes the contract visible in the place where breaking it hurts. The golden file even has
`"fetched_at": "FETCHED_AT"` in it, a placeholder the golden tests put there, and the widget's test
swaps in a time before parsing. I'd never thought of that placeholder as part of the contract,
but here it is.

## What the contract didn't say

Three things I only found by writing the reader.

**`in_days` is out of date the moment it's cached.** The CLI computes it when it runs. The widget
runs the CLI every ten minutes. When Quantic can't be reached, the answer can be a copy from
yesterday, and "tomorrow" would be wrong. So the widget counts days itself, from the date and its
own today, and ignores `in_days`. The field isn't wrong, but a consumer of cached data can't trust
anything relative to "now".

**Amounts don't add up across currencies.** §9 planned `€42.10 / €118` in the pill: received
against projected. But milestone 5 made received a list, one sum per currency, because Quantic
doesn't convert them. The demo user's October would have read `€0.00 / €138.60`, with $141.52 and
£49.72 left out. I asked, and the pill now shows the next event's own amount, which is always one
currency: `O ex-div in 3 days · $65.66`. The month moved to the panel, where there's room for every
currency. A decision on the server's side (no exchange rates) reached two repositories later.

**Where `quantic` is.** `go install` puts it in `~/go/bin`. My shell has that on its `PATH`.
Omarchy's shell, started by Hyprland, doesn't:

```
$ tr '\0' '\n' < /proc/$(pgrep -f quickshell | head -1)/environ | grep ^PATH= | tr ':' '\n' | grep -c go/bin
0
```

So the widget said "not installed" for a CLI I had just installed. It now has a `command` setting
(`omarchy bar set fleveque.quantic command ~/go/bin/quantic`), and it expands `~/` itself, because
`Process` runs a command with no shell to do it. When milestone 7 packages the CLI into `/usr/bin`,
most people won't need the setting.

## Counting days, again

Lesson 05 counted days in Go with midnight UTC on both sides. JavaScript has the same trap and the
same fix:

```js
var a = Date.parse(today + "T00:00:00Z")
var b = Date.parse(date + "T00:00:00Z")
return Math.round((b - a) / 86400000)
```

Without the `Z`, `Date.parse` reads the time as local, and on the day the clocks change, a day is
23 or 25 hours. My first test crossed October's change (a 25-hour day), and breaking the code
didn't fail it: 49 hours, rounded down to days, is still 2. The day that breaks a local count is
in spring, when 47 hours rounds down to 1. The test now crosses 2027-03-28. And CI runs on UTC,
which has no clock changes, so that test could never fail there. The workflow sets
`TZ: Europe/Madrid` for the test step. A test about time zones only proves something in a time
zone that has the problem.

## Checking QML by hand

There's no test runner for the QML here. It only runs inside Omarchy's shell, and on this machine
that's my desktop. So I checked it by hand. The fake was a fake `quantic`: a shell script that
prints one of the fixtures and exits with a code read from a file. The widget's `command` setting
pointed at it. Changing the file and asking for a refresh went through every state:

```
omarchy-shell fleveque.quantic refresh
```

![signed out](https://raw.githubusercontent.com/fleveque/omarchy-quantic/main/docs/screenshots/pill-signed-out.png)
![stale](https://raw.githubusercontent.com/fleveque/omarchy-quantic/main/docs/screenshots/pill-stale.png)

Signed out shows a key. A stale copy is dimmed, with "As of …, offline" in the panel. With nothing
cached, it's the icon alone. The screenshots in the README come from that run. No real portfolio
data was involved, because the fake only knows the fixtures. Before enabling the widget, I backed
up `~/.config/omarchy/shell.json`; afterwards I disabled the widget and checked the file was
byte-for-byte what it had been.

`omarchy plugin validate` printed nothing on success, which isn't proof of anything. So I broke a
copy of the manifest:

```
$ omarchy plugin validate /tmp/…/badplugin
omarchy-plugin-validate: manifest missing required field 'version'
```

It does check. CI has no Omarchy, so `test/manifest.js` repeats the checks that matter.

## Testing it

- **`node --test`**: 19 tests of `Model.js` against the golden `status.json` and two hand-written
  fixtures (signed out, and a stale copy with a failed part), in Europe/Madrid time.
- **`test/manifest.js`**: required fields, entry points that exist, defaults that match the
  settings schema.
- **CI**: both of those, and the golden file compared with quantic-cli's `main`.
- **By hand**: every state in the live shell, with a fake `quantic`.

## Breaking it on purpose

| Broken | Caught by |
|---|---|
| days from `in_days`, not from today | "the pill counts from today…", "the pill: the next event…" |
| the amount shown with `showAmounts` off | "the pill: the next event, and its amount only when asked" |
| no 14-day window on the rows | "rows: every date from today on…" |
| no ex-before-pay on the same day | nothing, at first: one item can't show it. Now "rows with an ex-date and a payment on the same day" uses two |
| any schema accepted | "output it can't read is an error that says why" |
| exit 3 and 4 swapped | "each exit code is its own state" |
| no 5-minute floor | "refresh is never more often than every 5 minutes" |
| days counted in local time | nothing, at first: October's change is 25 hours. Now the spring change, in `TZ=Europe/Madrid` |
| stale not dimmed | "a stale copy is dimmed and says since when" |
| `-` after the currency symbol | "money as quantic sent it…" |
| no `~/` expansion | "the command, with a portfolio only when one is set" |
| a failed part shown as "nothing received" | "the month with nothing paid yet, and with a part missing" |
| radar rows without their label | "rows: every date from today on…" |

## What I'm taking away

- Read the real thing before following the design's memory of it. Omarchy's own widgets decided
  the shape: logic in a `Model.js` that node can test, and a panel that owns the state.
- Exit codes and schema names, chosen for the CLI in milestone 1, are what make a consumer simple.
  The consumer never parses a message.
- A contract shared between two repositories needs a check in both. A copied golden file and a
  `diff` in CI are enough.
- Anything relative to "now" goes stale in a cache. The reader recomputes it.
- A decision upstream (no currency conversion) changes what a downstream screen can honestly show.
- The environment a program runs in isn't yours: Omarchy's shell has its own `PATH`.
- A test about time zones needs a time zone with the problem, in CI too.

Next: milestone 7, the release: GoReleaser, an AUR package, Homebrew.
