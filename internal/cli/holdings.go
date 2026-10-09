package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fleveque/quantic-cli/internal/api"
	"github.com/fleveque/quantic-cli/internal/render"
)

// holdingsOutput is `quantic holdings --json`.
type holdingsOutput struct {
	envelope
	Portfolio *string       `json:"portfolio"` // --portfolio as given; null for all of them
	Items     []holdingItem `json:"items"`
}

type holdingItem struct {
	Symbol         string  `json:"symbol"`
	Name           *string `json:"name"`
	Shares         string  `json:"shares"`       // a decimal string: shares can be fractional
	AverageCost    money   `json:"average_cost"` // per share, in the currency it trades in
	Sector         *string `json:"sector"`
	InstrumentType *string `json:"instrument_type"`
	ISIN           *string `json:"isin"`
	Portfolio      *string `json:"portfolio"` // which one, with several and no --portfolio; else null
}

const holdingsSchema = "quantic.cli/holdings/v1"

func newHoldingsCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "holdings",
		Short: "What you hold, and what it cost",
		Long: `Every position in your portfolios, or in the one --portfolio names: shares and
the average cost of each, in the currency it trades in.`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			list, err := fetch(cmd, opts, tokenNeeded, func(ctx context.Context, c *api.Client) (*api.HoldingList, error) {
				return c.Holdings(ctx, api.ListHoldingsParams{Portfolio: optional(opts.Portfolio)})
			})
			if err != nil {
				return err
			}

			out := holdingsOutput{
				envelope:  newEnvelope(holdingsSchema),
				Portfolio: optional(opts.Portfolio),
				Items:     make([]holdingItem, 0, len(list.Holdings)),
			}
			for _, h := range list.Holdings {
				out.Items = append(out.Items, holdingItem{
					Symbol:         h.Symbol,
					Name:           h.Name,
					Shares:         h.Shares,
					AverageCost:    money(h.AverageCost),
					Sector:         h.Sector,
					InstrumentType: h.InstrumentType,
					ISIN:           h.Isin,
					Portfolio:      h.Portfolio,
				})
			}

			w := cmd.OutOrStdout()
			if opts.JSON {
				return render.JSON(w, out)
			}
			if len(out.Items) == 0 {
				_, err := fmt.Fprintln(w, "No holdings.")
				return err
			}
			headers := []string{"SYMBOL", "NAME", "SHARES", "AVERAGE COST"}
			byPortfolio := out.Items[0].Portfolio != nil
			if byPortfolio {
				headers = append(headers, "PORTFOLIO")
			}
			rows := make([][]string, 0, len(out.Items))
			for _, h := range out.Items {
				row := []string{h.Symbol, render.Or(h.Name), h.Shares, h.AverageCost.text()}
				if byPortfolio {
					row = append(row, render.Or(h.Portfolio))
				}
				rows = append(rows, row)
			}
			render.AlignRight(headers, rows, 2, 3)
			return render.Table(w, headers, rows)
		},
	}
}

// optional is a flag's value as an API parameter: nil when it wasn't given.
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
