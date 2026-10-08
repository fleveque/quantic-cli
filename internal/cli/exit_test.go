package cli_test

import (
	"errors"
	"fmt"
	"io/fs"
	"testing"

	"github.com/fleveque/quantic-cli/internal/cli"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "no error", err: nil, want: cli.ExitOK},
		{name: "plain error", err: errors.New("boom"), want: cli.ExitFailed},
		{name: "exit error", err: &cli.ExitError{Code: cli.ExitNotSignedIn, Err: errors.New("401")}, want: cli.ExitNotSignedIn},
		{
			// A command adds context with %w on its way up; the code must
			// survive that, or the widget reads "failed" for "log in again".
			name: "wrapped exit error",
			err:  fmt.Errorf("holdings: %w", &cli.ExitError{Code: cli.ExitUnreachable, Err: errors.New("dial tcp")}),
			want: cli.ExitUnreachable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cli.ExitCode(tt.err); got != tt.want {
				t.Errorf("ExitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

// An exit code is a label on an error, not a replacement for it: errors.Is
// still finds what's underneath.
func TestExitErrorUnwraps(t *testing.T) {
	err := &cli.ExitError{Code: cli.ExitFailed, Err: fmt.Errorf("reading cache: %w", fs.ErrNotExist)}

	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("errors.Is(%v, fs.ErrNotExist) = false, want true", err)
	}
}
