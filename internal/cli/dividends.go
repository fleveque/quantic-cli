package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/spf13/cobra"

	"github.com/fleveque/quantic-cli/internal/api"
	"github.com/fleveque/quantic-cli/internal/render"
)

// dividendsOutput is `quantic dividends --json`. The filters are echoed, as
// given, so a program reading a saved output knows what it holds.
type dividendsOutput struct {
	envelope
	Symbol    *string        `json:"symbol"`
	From      *string        `json:"from"`
	To        *string        `json:"to"`
	Portfolio *string        `json:"portfolio"`
	Items     []dividendItem `json:"items"`
}

type dividendItem struct {
	Date           string  `json:"date"` // the pay date
	Symbol         string  `json:"symbol"`
	Name           *string `json:"name"`
	Gross          money   `json:"gross"`
	WithholdingTax *money  `json:"withholding_tax"` // withheld abroad
	Net            money   `json:"net"`             // what reached you
	PerShare       *money  `json:"per_share"`
	Shares         *int    `json:"shares"` // what it was paid on
	Source         *string `json:"source"` // how it was recorded: manual, or an import
	ISIN           *string `json:"isin"`
	Portfolio      *string `json:"portfolio"`
}

const dividendsSchema = "quantic.cli/dividends/v1"

func newDividendsCmd(opts *Options) *cobra.Command {
	var symbol, from, to string
	cmd := &cobra.Command{
		Use:   "dividends",
		Short: "Dividends you've received",
		Long: `Dividends paid to you, newest first: gross, withheld abroad, and net. Narrow
them with --symbol, --from and --to (dates as 2026-01-31), and --portfolio.`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			params := api.ListDividendsParams{Portfolio: optional(opts.Portfolio)}
			if symbol != "" {
				symbol = strings.ToUpper(symbol)
				params.Symbol = &symbol
			}
			var err error
			if params.From, err = date("--from", from); err != nil {
				return err
			}
			if params.To, err = date("--to", to); err != nil {
				return err
			}
			if params.From != nil && params.To != nil && params.To.Before(params.From.Time) {
				return usageError("--to (%s) is before --from (%s)", to, from)
			}

			list, err := fetch(cmd, opts, tokenNeeded, func(ctx context.Context, c *api.Client) (*api.DividendList, error) {
				return c.Dividends(ctx, params)
			})
			if err != nil {
				return err
			}

			out := dividendsOutput{
				envelope:  newEnvelope(dividendsSchema),
				Symbol:    params.Symbol,
				From:      optional(from),
				To:        optional(to),
				Portfolio: params.Portfolio,
				Items:     make([]dividendItem, 0, len(list.Dividends)),
			}
			for _, d := range list.Dividends {
				out.Items = append(out.Items, dividendItem{
					Date:           d.Date.Format(time.DateOnly),
					Symbol:         d.Symbol,
					Name:           d.Name,
					Gross:          money(d.Amount),
					WithholdingTax: (*money)(d.WithholdingTax),
					Net:            money(d.Net),
					PerShare:       (*money)(d.PerShareAmount),
					Shares:         d.Quantity,
					Source:         d.Source,
					ISIN:           d.Isin,
					Portfolio:      d.Portfolio,
				})
			}

			w := cmd.OutOrStdout()
			if opts.JSON {
				return render.JSON(w, out)
			}
			if len(out.Items) == 0 {
				_, err := fmt.Fprintln(w, "No dividends match.")
				return err
			}
			headers := []string{"DATE", "SYMBOL", "NAME", "GROSS", "NET"}
			byPortfolio := out.Items[0].Portfolio != nil
			if byPortfolio {
				headers = append(headers, "PORTFOLIO")
			}
			rows := make([][]string, 0, len(out.Items))
			for _, d := range out.Items {
				row := []string{d.Date, d.Symbol, render.Or(d.Name), d.Gross.text(), d.Net.text()}
				if byPortfolio {
					row = append(row, render.Or(d.Portfolio))
				}
				rows = append(rows, row)
			}
			render.AlignRight(headers, rows, 3, 4)
			return render.Table(w, headers, rows)
		},
	}
	f := cmd.Flags()
	f.StringVar(&symbol, "symbol", "", "only this ticker")
	f.StringVar(&from, "from", "", "paid on or after this date, as 2026-01-31")
	f.StringVar(&to, "to", "", "paid on or before this date")
	return cmd
}

// date parses a date flag: nil when it wasn't given, a usage error when it
// isn't a date.
func date(flag, s string) (*openapi_types.Date, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil, usageError("%s %q isn't a date like 2026-01-31", flag, s)
	}
	return &openapi_types.Date{Time: t}, nil
}
