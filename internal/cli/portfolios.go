package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/fleveque/quantic-cli/internal/api"
	"github.com/fleveque/quantic-cli/internal/render"
)

// portfoliosOutput is `quantic portfolios --json`.
type portfoliosOutput struct {
	envelope
	Items []portfolioItem `json:"items"`
}

type portfolioItem struct {
	Name    string  `json:"name"`
	Default bool    `json:"default"`
	Slug    *string `json:"slug"` // its public link's name, when it's shared; else null
}

const portfoliosSchema = "quantic.cli/portfolios/v1"

func newPortfoliosCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "portfolios",
		Short: "Your portfolios",
		Long: `Your portfolios, by name. Any of them can be passed to --portfolio, by name or
slug, in any case.`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			list, err := fetch(cmd, opts, tokenNeeded, func(ctx context.Context, c *api.Client) (*api.PortfolioList, error) {
				return c.Portfolios(ctx)
			})
			if err != nil {
				return err
			}

			out := portfoliosOutput{envelope: newEnvelope(portfoliosSchema), Items: make([]portfolioItem, 0, len(list.Portfolios))}
			for _, p := range list.Portfolios {
				out.Items = append(out.Items, portfolioItem{Name: p.Name, Default: p.Default, Slug: p.Slug})
			}

			w := cmd.OutOrStdout()
			if opts.JSON {
				return render.JSON(w, out)
			}
			rows := make([][]string, 0, len(out.Items))
			for _, p := range out.Items {
				def := ""
				if p.Default {
					def = "yes"
				}
				rows = append(rows, []string{p.Name, def, render.Or(p.Slug)})
			}
			return render.Table(w, []string{"NAME", "DEFAULT", "SHARED AS"}, rows)
		},
	}
}
