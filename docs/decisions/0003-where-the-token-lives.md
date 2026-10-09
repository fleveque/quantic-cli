# 0003 — Where the token lives, and how it stays out of sight

**Status:** accepted · **Date:** 2026-10-09 · **Milestone:** 3

## Context

Design §4 says the CLI signs in with a personal API token kept in the system keyring, falls back to
a `0600` file, lets `QUANTIC_TOKEN` override both, and never shows the token. Building it raised
choices the design leaves open:

- One token, or one per Quantic? `QUANTIC_URL` already points the CLI at a local server in
  development.
- When is a token checked: when it's stored, or when it's first used?
- What happens to a token file that others can read?
- Do public commands (`calendar`, `stock`, `search`) send the token?
- How does "never appears in output" become something a test can check, and something a future
  change can't quietly break?
- How do tests stay away from the keyring of the machine running them?

## Decision

- **One token per host.** The keyring entry is service `quantic-cli`, user the host of `QUANTIC_URL`
  (`quantic.finance`, `localhost:4000`). The file holds a map with the same keys. Signing in to a
  local server never replaces the production token.
- **Lookup order:** `QUANTIC_TOKEN`, then the keyring, then the file. A keyring that fails (no Secret
  Service) is treated as empty, because the token may be in the file for exactly that reason.
- **`auth login` checks the token with `GET /api/v1/me` before storing it.** A token Quantic rejects
  is never stored, and the exit code is 4. Stored in the keyring, any copy in the file is removed, so
  a stale token can't turn up later when the keyring fails.
- **The file:** `tokens.json` under `os.UserConfigDir()/quantic` (`$XDG_CONFIG_HOME` on Linux),
  written to a temporary file and renamed into place. `os.CreateTemp` makes that file `0600` from the
  start, and the directory is `0700`. **A token in a file others can read is refused**, as ssh
  refuses such a private key, with a message saying to `chmod 600` it and consider revoking the
  token. Replacing or removing a token in that file is still allowed: that's the fix.
- **Every command sends the token when there is one**, public ones included. Signed in, `stock` is
  richer and nothing is held to the anonymous rate limit. The cost: a revoked stored token makes
  `calendar` fail with exit 4 until you sign in again or log out. That is the same as `gh`, and it
  tells you about the revoked token.
- **`auth.Secret` holds the token**, behind a pointer in an unexported field, and `Reveal` is the
  only way to it: one caller, the code that sets the `Authorization` header. `encoding/json` can't
  see an unexported field at all. `fmt` can, through reflection, but it prints a pointer below the
  top level as its address, so no verb and no nesting shows the token. A `Format` method makes
  messages say `[redacted]` instead of an address. Tokens are checked when parsed: one word, no
  whitespace or control characters, and the error never contains the token.
- **A server answer of 422** (an unknown `--portfolio`) is exit 2, wrong usage, with Quantic's
  message, which lists the portfolios you do have.
- **The keyring is a dependency of `cli.RunWith`.** `cli.Run`, which `main` calls, passes the system
  keyring. Tests pass `internal/auth/authtest`'s: in memory for unit tests, a file in `$WORK` for
  testscript, so a script's `auth login` is found by its next command.

## Consequences

**Good.**
- "The token is never printed" is a property of a type, not a habit to keep up. A new error message
  that formats the token prints `[redacted]`, and `TestTheTokenIsNeverPrinted` runs every command
  through every failure with a marked token to check the output.
- A typo in a pasted token is found at `login`, not by the next command.
- Development against a local Quantic and daily use against production don't share a token.

**Costs.**
- `cli.Run` and `cli.RunWith` are two entry points. The second exists only so tests can choose the
  keyring.
- go-keyring brings `godbus/dbus` and `golang.org/x/sys` into the binary.
- On Linux, the first `login` can open the desktop's "create a keyring" or "unlock" dialog if there
  is no unlocked login keyring. That's the Secret Service doing its job, but a script that runs
  `login` unattended can wait on it. `QUANTIC_TOKEN` is the answer for scripts.

**Found while building it.** The D-Bus library behind the Secret Service finds the session bus at
`/run/user/<uid>/bus` even with `DBUS_SESSION_BUS_ADDRESS` unset and `XDG_RUNTIME_DIR` pointing
elsewhere. Clearing the environment, which testscript does, is not enough to keep a test away from
the real keyring; neither is a private `dbus-run-session` with its own `gnome-keyring-daemon`, which
finds the user's running daemon through `/run/user/<uid>/keyring` and defers to it. During
development, hand-run checks against a local server stored a demo user's token in the author's
keyring this way; it was removed. Only never calling the system keyring from tests is enough, which
is why `RunWith` takes the keyring.

**Rejected alternative — the keyring only, no file.** Servers, containers and minimal desktops have
no Secret Service, and `QUANTIC_TOKEN` in a shell profile is a worse file than a `0600` one.

**Rejected alternative — warn about a readable token file instead of refusing it.** A warning
printed by a status bar's background command is a warning nobody reads.

**Rejected alternative — `go-keyring`'s own mock (`keyring.MockInit`) in every test.** It replaces
the keyring for the whole process, so it can't give testscript's separate processes a shared
keyring. Forgetting to call it in one test would reach the real keyring, with nothing to catch it.
It is used once, to test `SystemKeyring`'s translation of go-keyring's errors.
