package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fleveque/quantic-cli/internal/api"
	"github.com/fleveque/quantic-cli/internal/render"
)

// searchOutput is `quantic search --json`.
type searchOutput struct {
	envelope
	Query string       `json:"query"`
	Items []searchItem `json:"items"`
}

type searchItem struct {
	Symbol   string  `json:"symbol"`
	Name     *string `json:"name"`
	Exchange *string `json:"exchange"`
	Type     *string `json:"type"`
	Tracked  bool    `json:"tracked"` // `quantic stock SYMBOL` has data for it
}

const searchSchema = "quantic.cli/search/v1"

func newSearchCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "search QUERY",
		Short: "Find a stock by ticker or company name",
		Long: `Find a stock by ticker or company name: up to about 10 matches. Without signing
in, the search covers the stocks Quantic already tracks. Several words are
searched as one query, as if quoted.`,
		Args: usageArgs(cobra.MinimumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.Join(strings.Fields(strings.Join(args, " ")), " ")
			if query == "" {
				return usageError("the query is empty")
			}
			found, err := fetch(cmd, opts, tokenIfAny, func(ctx context.Context, c *api.Client) (*api.StockSearch, error) {
				return c.SearchStocks(ctx, query)
			})
			if err != nil {
				return err
			}

			out := searchOutput{
				envelope: newEnvelope(searchSchema),
				Query:    query,
				Items:    make([]searchItem, 0, len(found.Results)),
			}
			for _, r := range found.Results {
				out.Items = append(out.Items, searchItem{
					Symbol: r.Symbol, Name: r.Name, Exchange: r.Exchange, Type: r.Type, Tracked: r.Tracked,
				})
			}

			w := cmd.OutOrStdout()
			if opts.JSON {
				return render.JSON(w, out)
			}
			if len(out.Items) == 0 {
				_, err := fmt.Fprintf(w, "Nothing matches %q.\n", query)
				return err
			}
			rows := make([][]string, 0, len(out.Items))
			for _, it := range out.Items {
				tracked := "no"
				if it.Tracked {
					tracked = "yes"
				}
				rows = append(rows, []string{it.Symbol, render.Or(it.Name), render.Or(it.Exchange), render.Or(it.Type), tracked})
			}
			return render.Table(w, []string{"SYMBOL", "NAME", "EXCHANGE", "TYPE", "TRACKED"}, rows)
		},
	}
}
