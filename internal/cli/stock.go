package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/fleveque/quantic-cli/internal/api"
	"github.com/fleveque/quantic-cli/internal/render"
)

// stockOutput is `quantic stock --json`. It mirrors the API's Stock, with
// the calendar's names for the same things (ex_date, frequency), and every
// key present: a value Quantic doesn't have is null, not missing.
type stockOutput struct {
	envelope
	Symbol         string        `json:"symbol"`
	Name           *string       `json:"name"`
	Currency       string        `json:"currency"` // the one it trades in
	Sector         *string       `json:"sector"`
	Industry       *string       `json:"industry"`
	InstrumentType *string       `json:"instrument_type"`
	Price          *stockPrice   `json:"price"` // null when Quantic has none
	Dividend       stockDividend `json:"dividend"`
	Scores         stockScores   `json:"scores"`
}

type stockPrice struct {
	money
	Live bool       `json:"live"`
	AsOf *time.Time `json:"as_of"` // when a stored price was seen; null when live
}

type stockDividend struct {
	YieldPct      *float64 `json:"yield_pct"`        // a percentage: 2.44 is 2.44%
	YieldAvg5yPct *float64 `json:"yield_avg_5y_pct"` // the same, averaged over about 5 years
	Annual        *money   `json:"annual"`           // per share, per year; null signed out
	Frequency     *string  `json:"frequency"`
	PaymentMonths []int    `json:"payment_months"` // 1–12
	ExDate        *string  `json:"ex_date"`
	CAGR5y        *float64 `json:"cagr_5y"`      // a fraction: 0.05 is 5% a year
	StreakYears   *int     `json:"streak_years"` // consecutive years of increases
	Safety        string   `json:"safety"`       // safe, watch, at_risk or unknown
}

// stockScores are Quantic's three lenses, each from 0 to 10.
type stockScores struct {
	Rating   *float64 `json:"rating"`
	Value    *float64 `json:"value"`
	Momentum *float64 `json:"momentum"`
}

const stockSchema = "quantic.cli/stock/v1"

func newStockCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "stock SYMBOL",
		Short: "One stock: price, dividend, safety and scores (public)",
		Long: `One stock: its price, dividend profile, dividend safety and Quantic's 0–10
scores. Signed out, the price is the last one Quantic stored, with its time.`,
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			symbol := strings.ToUpper(strings.TrimSpace(args[0]))
			if symbol == "" {
				return usageError("the symbol is empty")
			}
			s, err := fetch(cmd, opts, func(ctx context.Context, c *api.Client) (*api.Stock, error) {
				return c.Stock(ctx, symbol)
			})
			if statusErr, ok := errors.AsType[*api.StatusError](err); ok && statusErr.Status == http.StatusNotFound {
				// Quantic's message points to the API's search; this one
				// points to the CLI's.
				return fmt.Errorf("Quantic doesn't track %s; try: quantic search %s", symbol, symbol)
			}
			if err != nil {
				return err
			}

			out := newStockOutput(s)
			w := cmd.OutOrStdout()
			if opts.JSON {
				return render.JSON(w, out)
			}
			return out.writeText(w)
		},
	}
}

func newStockOutput(s *api.Stock) stockOutput {
	out := stockOutput{
		envelope:       newEnvelope(stockSchema),
		Symbol:         s.Symbol,
		Name:           s.Name,
		Currency:       s.Currency,
		Sector:         s.Sector,
		Industry:       s.Industry,
		InstrumentType: s.InstrumentType,
		Dividend: stockDividend{
			YieldPct:      s.Dividend.YieldPct,
			YieldAvg5yPct: s.Dividend.YieldAvg5yPct,
			Frequency:     s.Dividend.Frequency,
			PaymentMonths: s.Dividend.PaymentMonths,
			CAGR5y:        s.Dividend.Cagr5y,
			StreakYears:   s.Dividend.StreakYears,
			Safety:        string(s.Dividend.Safety),
		},
		Scores: stockScores{Rating: s.Scores.Rating, Value: s.Scores.Value, Momentum: s.Scores.Momentum},
	}
	if p := s.Price; p != nil {
		out.Price = &stockPrice{money: money{p.Amount, p.Currency}, Live: p.Live, AsOf: p.AsOf}
	}
	if a := s.Dividend.Annual; a != nil {
		out.Dividend.Annual = &money{a.Amount, a.Currency}
	}
	if d := s.Dividend.ExDividendDate; d != nil {
		ex := d.Format(time.DateOnly)
		out.Dividend.ExDate = &ex
	}
	return out
}

// writeText writes the stock for people: a heading, then one labelled line
// per topic, leaving out what Quantic doesn't know.
func (s stockOutput) writeText(w io.Writer) error {
	fmt.Fprintf(w, "%s  %s\n", s.Symbol, render.Or(s.Name))
	var about []string
	for _, v := range []*string{s.Sector, s.Industry} {
		if v != nil {
			about = append(about, render.Or(v))
		}
	}
	if len(about) > 0 {
		fmt.Fprintln(w, strings.Join(about, " · "))
	}
	fmt.Fprintln(w)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	line := func(label, value string) {
		if value != "" {
			fmt.Fprintf(tw, "%s\t%s\n", label, value)
		}
	}

	if p := s.Price; p != nil {
		price := p.text()
		if p.AsOf != nil {
			price += ", as of " + p.AsOf.UTC().Format("2006-01-02 15:04 UTC")
		}
		line("Price", price)
	} else {
		line("Price", "-")
	}

	d := s.Dividend
	var yield []string
	if d.YieldPct != nil {
		yield = append(yield, fmt.Sprintf("%.2f%%", *d.YieldPct))
	}
	if d.YieldAvg5yPct != nil {
		yield = append(yield, fmt.Sprintf("5-year average %.2f%%", *d.YieldAvg5yPct))
	}
	line("Yield", strings.Join(yield, ", "))

	var div []string
	if d.Annual != nil {
		div = append(div, d.Annual.text()+" a share a year")
	}
	if d.Frequency != nil {
		div = append(div, *d.Frequency)
	}
	if len(d.PaymentMonths) > 0 {
		months := make([]string, len(d.PaymentMonths))
		for i, m := range d.PaymentMonths {
			months[i] = time.Month(m).String()[:3]
		}
		div = append(div, "paid in "+strings.Join(months, " "))
	}
	if d.ExDate != nil {
		div = append(div, "next ex-date "+*d.ExDate)
	}
	line("Dividend", strings.Join(div, ", "))

	var growth []string
	if d.CAGR5y != nil {
		growth = append(growth, fmt.Sprintf("%.1f%% a year over 5 years", *d.CAGR5y*100))
	}
	if d.StreakYears != nil {
		growth = append(growth, fmt.Sprintf("raised %d years in a row", *d.StreakYears))
	}
	line("Growth", strings.Join(growth, ", "))
	line("Safety", d.Safety)

	var scores []string
	for _, sc := range []struct {
		name string
		v    *float64
	}{{"rating", s.Scores.Rating}, {"value", s.Scores.Value}, {"momentum", s.Scores.Momentum}} {
		if sc.v != nil {
			scores = append(scores, fmt.Sprintf("%s %.1f", sc.name, *sc.v))
		}
	}
	if len(scores) > 0 {
		line("Scores", strings.Join(scores, " · ")+" (out of 10)")
	}
	return tw.Flush()
}
