package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/fleveque/quantic-cli/internal/api"
	"github.com/fleveque/quantic-cli/internal/render"
)

// calendarOutput is `quantic calendar --json`. The envelope's fields come
// first: encoding/json writes an embedded struct's fields as if they were
// this struct's own, in the place the struct is embedded.
type calendarOutput struct {
	envelope
	From  string         `json:"from"` // today, UTC
	Days  int            `json:"days"` // after Quantic's clamping to 1–120
	Items []calendarItem `json:"items"`
}

type calendarItem struct {
	ExDate    string  `json:"ex_date"`
	Symbol    string  `json:"symbol"`
	Name      *string `json:"name"`
	Sector    *string `json:"sector"`
	Frequency *string `json:"frequency"`
}

const calendarSchema = "quantic.cli/calendar/v1"

func newCalendarCmd(opts *Options) *cobra.Command {
	var days int
	cmd := &cobra.Command{
		Use:   "calendar",
		Short: "Stocks going ex-dividend soon (public, no account needed)",
		Long: `Every stock Quantic tracks going ex-dividend in the next --days days, soonest
first. Buy before the ex-date to receive the next dividend.`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if days < 1 {
				return usageError("--days must be at least 1, got %d", days)
			}
			cal, err := fetch(cmd, opts, tokenIfAny, func(ctx context.Context, c *api.Client) (*api.Calendar, error) {
				return c.Calendar(ctx, days)
			})
			if err != nil {
				return err
			}

			out := calendarOutput{
				envelope: newEnvelope(calendarSchema),
				From:     cal.From.Format(time.DateOnly),
				Days:     cal.Days,
				Items:    make([]calendarItem, 0, len(cal.Stocks)),
			}
			for _, s := range cal.Stocks {
				out.Items = append(out.Items, calendarItem{
					ExDate:    s.ExDividendDate.Format(time.DateOnly),
					Symbol:    s.Symbol,
					Name:      s.Name,
					Sector:    s.Sector,
					Frequency: s.PaymentFrequency,
				})
			}

			w := cmd.OutOrStdout()
			if opts.JSON {
				return render.JSON(w, out)
			}
			if len(out.Items) == 0 {
				_, err := fmt.Fprintf(w, "No stock Quantic tracks goes ex-dividend in the next %d days.\n", out.Days)
				return err
			}
			rows := make([][]string, 0, len(out.Items))
			for _, it := range out.Items {
				rows = append(rows, []string{it.ExDate, it.Symbol, render.Or(it.Name), render.Or(it.Sector), render.Or(it.Frequency)})
			}
			return render.Table(w, []string{"EX-DATE", "SYMBOL", "NAME", "SECTOR", "FREQUENCY"}, rows)
		},
	}
	cmd.Flags().IntVar(&days, "days", 45, "how many days ahead, 1 to 120")
	return cmd
}
