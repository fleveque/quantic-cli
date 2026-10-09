package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/fleveque/quantic-cli/internal/api"
)

// defaultURL is Quantic in production. QUANTIC_URL replaces it, e.g. with
// http://localhost:4000 for a local Phoenix server.
const defaultURL = "https://quantic.finance"

// fetch makes one call to Quantic for cmd: it builds the client, gives the
// call --timeout to finish, retries included, and turns its error into the
// right exit code (design §6).
func fetch[T any](cmd *cobra.Command, opts *Options, call func(context.Context, *api.Client) (*T, error)) (*T, error) {
	base := os.Getenv("QUANTIC_URL")
	if base == "" {
		base = defaultURL
	}
	client, err := api.New(api.Config{BaseURL: base, UserAgent: opts.userAgent})
	if err != nil {
		return nil, usageError("QUANTIC_URL: %v", err)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), opts.Timeout)
	defer cancel()

	v, err := call(ctx, client)
	if err != nil {
		return nil, exitErrorFor(err, opts.Timeout)
	}
	return v, nil
}

// exitErrorFor gives an error from the api package its exit code, and a
// message for someone at a terminal.
func exitErrorFor(err error, timeout time.Duration) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &ExitError{Code: ExitUnreachable,
			Err: fmt.Errorf("Quantic didn't answer within %s (--timeout)", timeout)}
	}
	if _, ok := errors.AsType[*api.UnreachableError](err); ok {
		return &ExitError{Code: ExitUnreachable, Err: err}
	}
	if statusErr, ok := errors.AsType[*api.StatusError](err); ok {
		switch statusErr.Status {
		case http.StatusTooManyRequests:
			// Quantic's own message suggests sending a token, which the CLI
			// can't do yet (milestone 3).
			return &ExitError{Code: ExitRateLimited,
				Err: errors.New("Quantic is rate limiting this address (60 calls a minute without signing in); try again in a minute")}
		case http.StatusUnauthorized:
			return &ExitError{Code: ExitNotSignedIn, Err: err}
		}
	}
	return err
}

// envelope starts every data command's --json output (design §5): which
// shape it is, when it was fetched, and whether it came from a stale cache.
// Nothing is cached yet (milestone 4), so stale is always false for now; it
// is in the shape from the start so no program has to check for it later.
type envelope struct {
	Schema    string    `json:"schema"`
	FetchedAt time.Time `json:"fetched_at"`
	Stale     bool      `json:"stale"`
}

func newEnvelope(schema string) envelope {
	return envelope{Schema: schema, FetchedAt: time.Now().UTC().Truncate(time.Second)}
}

// money is an amount and its currency, as Quantic sends it. The amount stays
// the decimal string it arrived as: turned into a float64 and back, "0.1"
// would survive, but sums and long decimals wouldn't, and the CLI only passes
// money through.
type money struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

func (m money) text() string { return m.Amount + " " + m.Currency }
