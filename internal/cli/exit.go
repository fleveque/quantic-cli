package cli

import (
	"errors"
	"fmt"
)

// Exit codes (design §6). A script or the widget branches on these, so they
// are part of the CLI's contract: a meaning never changes once released.
const (
	ExitOK          = 0 // done, possibly from cache
	ExitFailed      = 1 // anything not covered below
	ExitUsage       = 2 // wrong usage: unknown command or flag, bad value
	ExitUnreachable = 3 // Quantic unreachable, and nothing cached
	ExitNotSignedIn = 4 // no token, or Quantic rejected it (401)
	ExitRateLimited = 5 // still rate limited after retrying
)

// ExitError is an error that knows which exit code it means. Commands return
// one when the plain "1" isn't enough; any other error exits 1.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }

// Unwrap lets errors.Is and errors.As see the error underneath, so wrapping
// an error in an exit code doesn't hide what it was.
func (e *ExitError) Unwrap() error { return e.Err }

// usageError is an ExitError with ExitUsage: the caller asked for something
// that can't mean anything, and trying again the same way won't help.
func usageError(format string, args ...any) error {
	return &ExitError{Code: ExitUsage, Err: fmt.Errorf(format, args...)}
}

// ExitCode is the exit status an error from a command maps to: 0 for nil,
// the code of the first ExitError in its chain, otherwise ExitFailed.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	if exitErr, ok := errors.AsType[*ExitError](err); ok {
		return exitErr.Code
	}
	return ExitFailed
}
