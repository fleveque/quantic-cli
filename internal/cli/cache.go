package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fleveque/quantic-cli/internal/cache"
)

func newCacheCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "The answers kept on disk",
		Long: `Answers from Quantic are kept for a while, so a status bar that asks every
few minutes doesn't ask Quantic every time, and still shows something when
Quantic can't be reached. --no-cache skips the cache for one command.`,
		Args: usageArgs(cobra.NoArgs),
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "clear",
		Short: "Remove every cached answer",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := opts.cacheDir()
			if err := (cache.Store{Dir: dir}).Clear(); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "Cleared %s.\n", dir)
			return err
		},
	})
	return cmd
}
