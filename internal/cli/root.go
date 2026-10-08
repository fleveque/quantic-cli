// Package cli is the command tree: Cobra commands, global flags, and how an
// error becomes an exit code (design §3, §6).
//
// Commands write to cmd.OutOrStdout() and cmd.ErrOrStderr(), never to
// os.Stdout and os.Stderr directly. Run sets those writers, so the binary
// gets the real streams and a test gets a buffer, with no other difference.
package cli

import (
	"fmt"
	"io"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Options holds the global flags (design §3). Every command sees the same
// one, through the pointer its constructor received.
type Options struct {
	JSON      bool
	Portfolio string
	NoCache   bool
	Timeout   time.Duration

	userAgent string // "quantic-cli/<version>", sent with every request
}

// defaultTimeout bounds one call to Quantic. Ten seconds is long for an API
// that usually answers in a few hundred milliseconds, and short enough that a
// status bar never hangs on a dead network.
const defaultTimeout = 10 * time.Second

// Run executes the command line in args and returns the process exit code.
// It never calls os.Exit, so tests can call it as often as they like.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, build Build) int {
	root := newRootCmd(build)
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)

	err := root.Execute()
	code := ExitCode(err)
	if err != nil {
		fmt.Fprintf(stderr, "quantic: %v\n", err)
		if code == ExitUsage {
			fmt.Fprintln(stderr, "Run 'quantic --help' for usage.")
		}
	}
	return code
}

func newRootCmd(build Build) *cobra.Command {
	info, _ := debug.ReadBuildInfo()
	opts := &Options{userAgent: "quantic-cli/" + build.withFallback(info).Version}

	root := &cobra.Command{
		Use:   "quantic",
		Short: "Your Quantic portfolio, dividends and income in the terminal",
		Long: `quantic reads your Quantic Finance portfolio, dividends and income, and
public dividend data, from https://quantic.finance. It only reads.

Tables are for people; --json is for scripts and status bars.

Environment:
  QUANTIC_URL  where Quantic is (default https://quantic.finance)`,

		// Run prints errors itself, once, with the right exit code. Left on,
		// Cobra would print each error a second time, and the whole usage
		// text after every failure, including ones that aren't about usage.
		SilenceErrors: true,
		SilenceUsage:  true,

		// With Args set, Cobra hands an unknown command to RunE below instead
		// of failing on its own, which is how it becomes a usage error.
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			msg := fmt.Sprintf("unknown command %q", args[0])
			if s := cmd.SuggestionsFor(args[0]); len(s) > 0 {
				msg += fmt.Sprintf("; did you mean %s?", strings.Join(s, " or "))
			}
			return usageError("%s", msg)
		},

		// Runs before any command, this one included: the checks every
		// command would otherwise repeat.
		PersistentPreRunE: func(*cobra.Command, []string) error {
			if opts.Timeout <= 0 {
				return usageError("--timeout must be more than zero, got %s", opts.Timeout)
			}
			return nil
		},
	}

	// SuggestionsFor, unlike Cobra's own unknown-command error, doesn't
	// apply the default distance: without this it suggests nothing.
	root.SuggestionsMinimumDistance = 2

	// Not now: a `completion` command appears with the release (milestone 7),
	// when there's somewhere to install the scripts it writes.
	root.CompletionOptions.DisableDefaultCmd = true

	// Cobra calls this for any flag it can't parse, on any command.
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError("%v", err)
	})

	flags := root.PersistentFlags()
	flags.BoolVar(&opts.JSON, "json", false, "print JSON for programs instead of tables")
	flags.StringVar(&opts.Portfolio, "portfolio", "", "only this portfolio, by name (default: all of them)")
	flags.BoolVar(&opts.NoCache, "no-cache", false, "always ask Quantic; don't read or write the cache")
	flags.DurationVar(&opts.Timeout, "timeout", defaultTimeout, "give up on Quantic after this long")

	root.AddCommand(
		newCalendarCmd(opts),
		newStockCmd(opts),
		newSearchCmd(opts),
		newVersionCmd(opts, build),
	)
	return root
}

// usageArgs makes a Cobra argument check fail with ExitUsage: the wrong
// number of arguments is wrong usage, not a failure.
func usageArgs(check cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := check(cmd, args); err != nil {
			return &ExitError{Code: ExitUsage, Err: err}
		}
		return nil
	}
}
