package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/fleveque/quantic-cli/internal/api"
	"github.com/fleveque/quantic-cli/internal/auth"
	"github.com/fleveque/quantic-cli/internal/cache"
)

// defaultURL is Quantic in production. QUANTIC_URL replaces it, e.g. with
// http://localhost:4000 for a local Phoenix server.
const defaultURL = "https://quantic.finance"

// need says whether a command needs a token. Every command sends one when
// there is one: a public command signed in is richer (stock) and isn't held
// to the anonymous rate limit.
type need int

const (
	tokenIfAny  need = iota // public: works signed out
	tokenNeeded             // your own data: fails with ExitNotSignedIn before calling
)

// session is one Quantic and the token for it.
type session struct {
	base    string     // QUANTIC_URL, or defaultURL
	host    string     // its host, which the token is kept under
	token   auth.Token // zero when signed out
	noCache bool       // always ask Quantic: `auth login` checking a new token
}

// newSession finds Quantic and, if there is one, the token for it.
func (o *Options) newSession(n need) (session, error) {
	s, err := quantic()
	if err != nil {
		return session{}, err
	}
	s.token, err = o.store().Load(s.host)
	switch {
	case err == nil:
	case errors.Is(err, auth.ErrNotSignedIn) && n == tokenIfAny:
	case errors.Is(err, auth.ErrNotSignedIn):
		return session{}, &ExitError{Code: ExitNotSignedIn,
			Err: fmt.Errorf("not signed in to %s; run: quantic auth login (or set QUANTIC_TOKEN)", s.host)}
	default:
		return session{}, &ExitError{Code: ExitNotSignedIn, Err: err}
	}
	return s, nil
}

// quantic is where Quantic is, from QUANTIC_URL: a session with no token.
func quantic() (session, error) {
	base := os.Getenv("QUANTIC_URL")
	if base == "" {
		base = defaultURL
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return session{}, usageError("QUANTIC_URL: %q isn't an http or https URL", base)
	}
	return session{base: base, host: strings.ToLower(u.Host)}, nil
}

// settingsURL is where Quantic's API tokens are made and revoked.
func (s session) settingsURL() string {
	return strings.TrimSuffix(s.base, "/") + "/settings#api-tokens-settings"
}

// store is where tokens are kept: the keyring, a file under the user's
// config directory (os.UserConfigDir: $XDG_CONFIG_HOME, or ~/.config, on
// Linux), and QUANTIC_TOKEN over both.
func (o *Options) store() *auth.Store {
	s := &auth.Store{Env: os.Getenv("QUANTIC_TOKEN"), Keyring: o.keyring}
	if dir, err := os.UserConfigDir(); err == nil {
		s.File = filepath.Join(dir, "quantic", "tokens.json")
	}
	return s
}

// fetched says where an answer came from: when Quantic gave it, and whether
// it is a cached copy used because Quantic couldn't be reached.
type fetched struct {
	At    time.Time
	Stale bool
}

// fetch makes one call to Quantic for cmd: it finds the session, gives the
// call --timeout to finish, retries included, answers from the cache when it
// can, and turns an error into the right exit code (design §6, §7).
func fetch[T any](cmd *cobra.Command, opts *Options, n need, call func(context.Context, *api.Client) (*T, error)) (*T, fetched, error) {
	s, err := opts.newSession(n)
	if err != nil {
		return nil, fetched{}, err
	}
	return callQuantic(cmd, opts, s, call)
}

// callQuantic is fetch for a session already found: `auth login` makes its
// own, with the token it is about to store.
func callQuantic[T any](cmd *cobra.Command, opts *Options, s session, call func(context.Context, *api.Client) (*T, error)) (*T, fetched, error) {
	v, at, err := askQuantic(cmd, opts, s, call)
	if err == nil && at.Stale {
		warnStale(cmd, opts, s, at)
	}
	return v, at, err
}

// askQuantic is callQuantic without the warning on stderr when the answer is
// a stale copy: `status` asks three times at once, and warns once.
func askQuantic[T any](cmd *cobra.Command, opts *Options, s session, call func(context.Context, *api.Client) (*T, error)) (*T, fetched, error) {
	cfg := api.Config{BaseURL: s.base, UserAgent: opts.userAgent, Token: s.token.Secret}
	var cached *cache.Transport
	if dir := opts.cacheDir(); dir != "" && !opts.NoCache && !s.noCache {
		cached = &cache.Transport{Store: cache.Store{Dir: dir}, MaxAge: freshFor, Now: opts.now}
		cfg.Transport = cached
	}
	client, err := api.New(cfg)
	if err != nil {
		return nil, fetched{}, usageError("QUANTIC_URL: %v", err)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), opts.Timeout)
	defer cancel()

	v, err := call(ctx, client)
	if err != nil {
		return nil, fetched{}, exitErrorFor(err, opts.Timeout, s)
	}
	if cached == nil {
		return v, fetched{At: opts.now()}, nil
	}
	last := cached.Last()
	return v, fetched{At: last.FetchedAt, Stale: last.Stale}, nil
}

// warnStale says on stderr that Quantic couldn't be reached, and how old the
// cached answer is instead.
func warnStale(cmd *cobra.Command, opts *Options, s session, at fetched) {
	fmt.Fprintf(cmd.ErrOrStderr(), "quantic: can't reach %s; this is what it said at %s UTC, %s\n",
		s.host, at.At.UTC().Format("2006-01-02 15:04"), ago(opts.now().Sub(at.At)))
}

// ago says how long ago, as people do: "40 minutes ago", not "40m0s".
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "moments ago"
	case d < 2*time.Minute:
		return "a minute ago"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d/time.Minute))
	case d < 2*time.Hour:
		return "an hour ago"
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d/time.Hour))
	}
	return fmt.Sprintf("%d days ago", int(d/(24*time.Hour)))
}

// freshFor is how long each answer is used without asking Quantic again
// (design §7). Revalidating an older one costs a 304 with no body when
// nothing changed. /me is always asked: `auth status` is how a script checks
// a token still works.
func freshFor(req *http.Request) time.Duration {
	p := req.URL.Path
	if i := strings.Index(p, "/api/v1/"); i >= 0 {
		p = p[i:] // QUANTIC_URL may have a path of its own
	}
	switch {
	case p == "/api/v1/calendar":
		return 6 * time.Hour
	case p == "/api/v1/holdings", p == "/api/v1/income", p == "/api/v1/dividends":
		return 15 * time.Minute
	case strings.HasPrefix(p, "/api/v1/stocks/"):
		return 15 * time.Minute // prices move
	case p == "/api/v1/stocks", p == "/api/v1/portfolios", p == "/api/v1/upcoming":
		return time.Hour
	}
	return 0
}

// cacheDir is where answers are kept: os.UserCacheDir ($XDG_CACHE_HOME, or
// ~/.cache, on Linux) plus "quantic". Empty when there's no such place, and
// then nothing is cached.
func (o *Options) cacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "quantic")
}

// exitErrorFor gives an error from the api package its exit code, and a
// message for someone at a terminal.
func exitErrorFor(err error, timeout time.Duration, s session) error {
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
			msg := "Quantic is rate limiting this address (60 calls a minute without signing in); " +
				"try again in a minute, or sign in: quantic auth login"
			if !s.token.Secret.IsZero() {
				msg = "Quantic is rate limiting this token; try again in a minute"
			}
			return &ExitError{Code: ExitRateLimited, Err: errors.New(msg)}
		case http.StatusUnauthorized:
			return &ExitError{Code: ExitNotSignedIn, Err: rejected(s)}
		case http.StatusUnprocessableEntity:
			// A parameter Quantic can't use: an unknown --portfolio, say. Its
			// message names the alternatives.
			return &ExitError{Code: ExitUsage, Err: err}
		}
	}
	return err
}

// rejected says which token Quantic turned down, and what to do about it:
// expired or revoked, it won't work again.
func rejected(s session) error {
	switch s.token.Source {
	case auth.FromEnv:
		return fmt.Errorf("%s rejected the token in QUANTIC_TOKEN; it may have been revoked. Make a new one at %s", s.host, s.settingsURL())
	case auth.FromKeyring, auth.FromFile:
		return fmt.Errorf("%s rejected the stored token; it may have been revoked. Sign in again: quantic auth login", s.host)
	}
	return fmt.Errorf("%s needs a token for this; run: quantic auth login", s.host)
}

// envelope starts every data command's --json output (design §5): which
// shape it is, when Quantic gave the answer, and whether it is a cached copy
// used because Quantic couldn't be reached (design §7).
type envelope struct {
	Schema    string    `json:"schema"`
	FetchedAt time.Time `json:"fetched_at"`
	Stale     bool      `json:"stale"`
}

func newEnvelope(schema string, f fetched) envelope {
	return envelope{Schema: schema, FetchedAt: f.At.UTC().Truncate(time.Second), Stale: f.Stale}
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
