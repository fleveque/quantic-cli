package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/spf13/cobra"

	"github.com/fleveque/quantic-cli/internal/api"
	"github.com/fleveque/quantic-cli/internal/render"
)

// upcomingOutput is `quantic upcoming --json` (design §5).
type upcomingOutput struct {
	envelope
	From      string         `json:"from"` // today, UTC
	Days      int            `json:"days"` // after Quantic's clamping to 1–120
	Portfolio *string        `json:"portfolio"`
	Items     []upcomingItem `json:"items"`
}

// upcomingItem is one dividend to come: a holding's, or, with held false, a
// stock on your radar's, which has no shares and so no amounts.
type upcomingItem struct {
	Symbol      string  `json:"symbol"`
	Name        *string `json:"name"`
	Held        bool    `json:"held"`
	ExDate      *string `json:"ex_date"`  // can be past when the payment is still to come
	PayDate     *string `json:"pay_date"` // null until known
	Shares      *string `json:"shares"`
	PerShare    *money  `json:"per_share"` // an estimate until the dividend is declared
	Expected    *money  `json:"expected"`
	ExpectedNet *money  `json:"expected_net"` // after estimated withholding abroad
	Portfolio   *string `json:"portfolio"`
}

const upcomingSchema = "quantic.cli/upcoming/v1"

func newUpcomingCmd(opts *Options) *cobra.Command {
	var days int
	cmd := &cobra.Command{
		Use:   "upcoming",
		Short: "Your next dividends: ex-dates, pay dates, and what each should pay",
		Long: `Each of your holdings going ex-dividend or paying in the next --days days,
soonest first: both dates, the payment per share, and what it should pay you,
before and after withholding abroad. Stocks on your radar come too, without
amounts; --portfolio leaves them out.`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if days < 1 {
				return usageError("--days must be at least 1, got %d", days)
			}
			up, at, err := fetch(cmd, opts, tokenNeeded, func(ctx context.Context, c *api.Client) (*api.Upcoming, error) {
				return c.Upcoming(ctx, api.ListUpcomingParams{Days: &days, Portfolio: optional(opts.Portfolio)})
			})
			if err != nil {
				return err
			}
			out := upcomingOutput{
				envelope:  newEnvelope(upcomingSchema, at),
				From:      up.From.Format(time.DateOnly),
				Days:      up.Days,
				Portfolio: optional(opts.Portfolio),
				Items:     upcomingItems(up),
			}

			w := cmd.OutOrStdout()
			if opts.JSON {
				return render.JSON(w, out)
			}
			if len(out.Items) == 0 {
				_, err := fmt.Fprintf(w, "Nothing of yours goes ex-dividend or pays in the next %d days.\n", out.Days)
				return err
			}
			return writeUpcoming(w, out.Items)
		},
	}
	cmd.Flags().IntVar(&days, "days", 45, "how many days ahead, 1 to 120")
	return cmd
}

// upcomingItems is the API's items as the CLI's, in Quantic's order: soonest
// first. `status` shows them too.
func upcomingItems(up *api.Upcoming) []upcomingItem {
	items := make([]upcomingItem, 0, len(up.Items))
	for _, it := range up.Items {
		items = append(items, upcomingItem{
			Symbol:      it.Symbol,
			Name:        it.Name,
			Held:        it.Held,
			ExDate:      dateOrNil(it.ExDividendDate),
			PayDate:     dateOrNil(it.PayDate),
			Shares:      it.Shares,
			PerShare:    (*money)(it.PerShare),
			Expected:    (*money)(it.Expected),
			ExpectedNet: (*money)(it.ExpectedNet),
			Portfolio:   it.Portfolio,
		})
	}
	return items
}

// writeUpcoming is the table: a stock on your radar says so where a
// holding's shares would be.
func writeUpcoming(w io.Writer, items []upcomingItem) error {
	headers := []string{"EX-DATE", "PAY DATE", "SYMBOL", "NAME", "SHARES", "PER SHARE", "EXPECTED", "NET"}
	byPortfolio := false
	for _, it := range items {
		byPortfolio = byPortfolio || it.Portfolio != nil
	}
	if byPortfolio {
		headers = append(headers, "PORTFOLIO")
	}
	rows := make([][]string, 0, len(items))
	for _, it := range items {
		shares := "radar"
		if it.Held {
			shares = render.Or(it.Shares)
		}
		row := []string{render.Or(it.ExDate), render.Or(it.PayDate), it.Symbol, render.Or(it.Name),
			shares, it.PerShare.orDash(), it.Expected.orDash(), it.ExpectedNet.orDash()}
		if byPortfolio {
			row = append(row, render.Or(it.Portfolio))
		}
		rows = append(rows, row)
	}
	render.AlignRight(headers, rows, 4, 5, 6, 7)
	return render.Table(w, headers, rows)
}

// dateOrNil is an optional date from the API as the CLI writes dates.
func dateOrNil(d *openapi_types.Date) *string {
	if d == nil {
		return nil
	}
	s := d.Format(time.DateOnly)
	return &s
}

// orDash is money for a table, or "-" when there's none.
func (m *money) orDash() string {
	if m == nil {
		return "-"
	}
	return m.text()
}
