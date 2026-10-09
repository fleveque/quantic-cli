package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/fleveque/quantic-cli/internal/api"
	"github.com/fleveque/quantic-cli/internal/auth"
	"github.com/fleveque/quantic-cli/internal/render"
)

func newAuthCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Sign in, sign out, and see who you are",
		Long: `Your own portfolio needs a personal API token, made in Quantic's settings
(Settings → AI and API access). The token is kept in the system keyring, one
per Quantic host; QUANTIC_TOKEN, when set, is used instead.`,
		Args: usageArgs(cobra.NoArgs),
	}
	cmd.AddCommand(newLoginCmd(opts), newLogoutCmd(opts), newAuthStatusCmd(opts))
	return cmd
}

func newLoginCmd(opts *Options) *cobra.Command {
	var withToken bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store a personal API token",
		Long: `Asks for a personal API token, checks it with Quantic, and stores it in the
system keyring. Without a keyring, it goes to a file only you can read, with a
warning.

With --with-token, the token is read from standard input instead of asked for:
  quantic auth login --with-token < token.txt`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Where Quantic is, without the stored token: the new one replaces it.
			s, err := quantic()
			if err != nil {
				return err
			}
			secret, err := readToken(cmd, withToken, s)
			if err != nil {
				return err
			}
			s.token = auth.Token{Secret: secret}

			// Checked before it's stored: a typo is found now, not by the next
			// command, and a token that works is the only kind kept.
			me, err := callQuantic(cmd, opts, s, func(ctx context.Context, c *api.Client) (*api.Me, error) {
				return c.Me(ctx)
			})
			if ExitCode(err) == ExitNotSignedIn {
				return &ExitError{Code: ExitNotSignedIn,
					Err: fmt.Errorf("%s doesn't accept that token, so it wasn't stored. Make one at %s", s.host, s.settingsURL())}
			}
			if err != nil {
				return err
			}

			saved, err := opts.store().Save(s.host, secret)
			if err != nil {
				return fmt.Errorf("the token works, but it couldn't be stored: %w", err)
			}

			errOut := cmd.ErrOrStderr()
			if saved.Source == auth.FromFile {
				fmt.Fprintf(errOut, "quantic: no system keyring (%v);\nquantic: the token is in %s instead, readable only by you.\n",
					saved.KeyringErr, opts.store().File)
			}
			if os.Getenv("QUANTIC_TOKEN") != "" {
				fmt.Fprintln(errOut, "quantic: QUANTIC_TOKEN is set, and is used instead of the stored token while it is.")
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Signed in to %s as %s.\n", s.host, who(me))
			return err
		},
	}
	cmd.Flags().BoolVar(&withToken, "with-token", false, "read the token from standard input")
	return cmd
}

// readToken gets the token from stdin. At a terminal it asks, without echoing
// what is pasted; otherwise it reads stdin only when --with-token says to, so
// a script that forgot the token fails instead of waiting for one.
func readToken(cmd *cobra.Command, withToken bool, s session) (auth.Secret, error) {
	in := cmd.InOrStdin()
	f, isFile := in.(*os.File)
	atTerminal := isFile && term.IsTerminal(int(f.Fd()))

	var raw string
	switch {
	case withToken:
		// 4 KiB is far more than a token; the limit stops `quantic auth
		// login --with-token < /dev/zero` reading forever.
		b, err := io.ReadAll(io.LimitReader(in, 4096))
		if err != nil {
			return auth.Secret{}, fmt.Errorf("reading the token: %w", err)
		}
		raw = string(b)
	case atTerminal:
		fmt.Fprintf(cmd.ErrOrStderr(), "Make a token at %s\nPaste it here (it won't show): ", s.settingsURL())
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(cmd.ErrOrStderr())
		if err != nil {
			return auth.Secret{}, fmt.Errorf("reading the token: %w", err)
		}
		raw = string(b)
	default:
		return auth.Secret{}, usageError("standard input isn't a terminal; to read the token from it, add --with-token")
	}

	secret, err := auth.ParseSecret(raw)
	if err != nil {
		return auth.Secret{}, usageError("%v", err)
	}
	return secret, nil
}

func newLogoutCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget the stored token",
		Long: `Removes the token stored for this Quantic host. The token itself keeps working
until you revoke it in Quantic's settings.`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Not newSession: a token that can't be loaded (in a file others
			// can read, say) is still one to remove.
			s, err := quantic()
			if err != nil {
				return err
			}

			removed, err := opts.store().Delete(s.host)
			if err != nil {
				return err
			}

			w, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
			if len(removed) == 0 {
				fmt.Fprintf(w, "No token was stored for %s.\n", s.host)
			} else {
				fmt.Fprintf(w, "Signed out of %s.\n", s.host)
				fmt.Fprintf(errOut, "quantic: the token still works until you revoke it at %s\n", s.settingsURL())
			}
			if os.Getenv("QUANTIC_TOKEN") != "" {
				fmt.Fprintln(errOut, "quantic: QUANTIC_TOKEN is still set, and commands use it until you unset it.")
			}
			return nil
		},
	}
}

// authStatusOutput is `quantic auth status --json`.
type authStatusOutput struct {
	envelope
	Host        string      `json:"host"`
	TokenSource auth.Source `json:"token_source"` // QUANTIC_TOKEN, keyring or file
	User        authUser    `json:"user"`
}

type authUser struct {
	ID                string  `json:"id"`
	Name              *string `json:"name"`
	Email             string  `json:"email"`
	PreferredCurrency string  `json:"preferred_currency"` // what Quantic adds amounts up in
}

const authStatusSchema = "quantic.cli/auth/v1"

func newAuthStatusCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Who you are signed in as, and where the token comes from",
		Long: `Asks Quantic who the token belongs to. Exits 4 when there's no token, or
Quantic rejects it, so a script can check before it starts.`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := opts.newSession(tokenNeeded)
			if err != nil {
				return err
			}
			me, err := callQuantic(cmd, opts, s, func(ctx context.Context, c *api.Client) (*api.Me, error) {
				return c.Me(ctx)
			})
			if err != nil {
				return err
			}

			out := authStatusOutput{
				envelope:    newEnvelope(authStatusSchema),
				Host:        s.host,
				TokenSource: s.token.Source,
				User: authUser{
					ID:                me.Id.String(),
					Name:              me.Name,
					Email:             me.Email,
					PreferredCurrency: me.PreferredCurrency,
				},
			}
			w := cmd.OutOrStdout()
			if opts.JSON {
				return render.JSON(w, out)
			}
			return render.Fields(w, [][2]string{
				{"Signed in to", out.Host},
				{"As", who(me)},
				{"Currency", out.User.PreferredCurrency},
				{"Token", tokenFrom(s.token.Source)},
			})
		},
	}
}

// who is a user as people read it: a name and an email, or just the email.
func who(me *api.Me) string {
	if me.Name != nil && strings.TrimSpace(*me.Name) != "" {
		return fmt.Sprintf("%s <%s>", strings.TrimSpace(*me.Name), me.Email)
	}
	return me.Email
}

func tokenFrom(src auth.Source) string {
	switch src {
	case auth.FromEnv:
		return "from QUANTIC_TOKEN"
	case auth.FromKeyring:
		return "in the system keyring"
	}
	return "in a file (no system keyring)"
}
