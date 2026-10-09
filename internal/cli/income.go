package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/fleveque/quantic-cli/internal/api"
	"github.com/fleveque/quantic-cli/internal/render"
)

// incomeOutput is `quantic income --json`: the API's Income, with the year's
// months as a list in calendar order instead of an object keyed "1" to "12".
type incomeOutput struct {
	envelope
	Portfolio       *string         `json:"portfolio"`
	AnnualIncome    money           `json:"annual_income"`     // forward, at today's rates, gross
	NetAnnualIncome money           `json:"net_annual_income"` // after estimated withholding abroad
	YieldPct        *float64        `json:"yield_pct"`         // a percentage
	GrowthRate      *float64        `json:"growth_rate"`       // income-weighted, a fraction a year
	DRIP            bool            `json:"drip"`              // the projection reinvests dividends
	Years           int             `json:"years"`
	Projection      []incomeYear    `json:"projection"`  // years 0 (now) to Years
	IncomeYear      *incomeMonths   `json:"income_year"` // null for a portfolio too small to have a shape
	Holdings        []incomeHolding `json:"holdings"`    // biggest first
}

type incomeYear struct {
	Year      int   `json:"year"` // from now: 0 is this year
	Income    money `json:"income"`
	NetIncome money `json:"net_income"`
}

type incomeMonths struct {
	NetOfWithholding   bool          `json:"net_of_withholding"`
	AverageMonth       *money        `json:"average_month"`
	ByMonth            []incomeMonth `json:"by_month"`             // January first, all twelve
	BiggestMonths      []int         `json:"biggest_months"`       // 1–12
	BiggestMonthsShare *float64      `json:"biggest_months_share"` // of the year; 0.25 is perfectly even
	ThinMonths         []int         `json:"thin_months"`          // below average, thinnest first
	EmptyMonths        []int         `json:"empty_months"`
}

type incomeMonth struct {
	Month  int   `json:"month"` // 1–12
	Income money `json:"income"`
}

type incomeHolding struct {
	Symbol      string   `json:"symbol"`
	Income      money    `json:"income"`
	NetIncome   money    `json:"net_income"`
	Growth      *float64 `json:"growth"`  // a fraction
	CAGR5y      *float64 `json:"cagr_5y"` // a fraction
	StreakYears *int     `json:"streak_years"`
}

const incomeSchema = "quantic.cli/income/v1"

func newIncomeCmd(opts *Options) *cobra.Command {
	var years int
	var drip bool
	cmd := &cobra.Command{
		Use:   "income",
		Short: "Your dividend income: a year from now, by month, and projected",
		Long: `What your holdings pay in a year at today's dividend rates, before and after
withholding abroad; how it falls across the months; which holdings it comes
from; and a projection --years ahead at their dividend growth.`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if years < 1 {
				return usageError("--years must be at least 1, got %d", years)
			}
			inc, err := fetch(cmd, opts, tokenNeeded, func(ctx context.Context, c *api.Client) (*api.Income, error) {
				return c.Income(ctx, api.GetIncomeParams{Years: &years, Drip: &drip, Portfolio: optional(opts.Portfolio)})
			})
			if err != nil {
				return err
			}
			out := newIncomeOutput(inc, optional(opts.Portfolio))

			w := cmd.OutOrStdout()
			if opts.JSON {
				return render.JSON(w, out)
			}
			return out.writeText(w)
		},
	}
	cmd.Flags().IntVar(&years, "years", 10, "how many years to project, 1 to 50")
	cmd.Flags().BoolVar(&drip, "drip", false, "project with dividends reinvested")
	return cmd
}

func newIncomeOutput(inc *api.Income, portfolio *string) incomeOutput {
	out := incomeOutput{
		envelope:        newEnvelope(incomeSchema),
		Portfolio:       portfolio,
		AnnualIncome:    money(inc.AnnualIncome),
		NetAnnualIncome: money(inc.NetAnnualIncome),
		YieldPct:        inc.CurrentYieldPct,
		GrowthRate:      inc.GrowthRate,
		DRIP:            inc.Drip,
		Years:           inc.Years,
		Projection:      make([]incomeYear, 0, len(inc.Projection)),
		Holdings:        make([]incomeHolding, 0, len(inc.Holdings)),
	}
	for _, p := range inc.Projection {
		out.Projection = append(out.Projection, incomeYear{Year: p.YearOffset, Income: money(p.Income), NetIncome: money(p.NetIncome)})
	}
	for _, h := range inc.Holdings {
		out.Holdings = append(out.Holdings, incomeHolding{
			Symbol: h.Symbol, Income: money(h.Income), NetIncome: money(h.NetIncome),
			Growth: h.Growth, CAGR5y: h.Cagr5y, StreakYears: h.Streak,
		})
	}

	if y := inc.IncomeYear; y != nil {
		m := &incomeMonths{
			NetOfWithholding:   y.NetOfWithholding != nil && *y.NetOfWithholding,
			AverageMonth:       (*money)(y.AverageMonth),
			BiggestMonthsShare: y.BiggestMonthsShare,
			BiggestMonths:      orEmpty(y.BiggestMonths),
			ThinMonths:         orEmpty(y.ThinMonths),
			EmptyMonths:        orEmpty(y.EmptyMonths),
			ByMonth:            []incomeMonth{},
		}
		if y.ByMonth != nil {
			// A map has no order; the months do.
			for month := 1; month <= 12; month++ {
				if v, ok := (*y.ByMonth)[strconv.Itoa(month)]; ok {
					m.ByMonth = append(m.ByMonth, incomeMonth{Month: month, Income: money(v)})
				}
			}
		}
		out.IncomeYear = m
	}
	return out
}

// orEmpty is a list the API may leave out, as an empty one: [] in the JSON,
// never null, so a program can loop over it without checking.
func orEmpty(p *[]int) []int {
	if p == nil {
		return []int{}
	}
	return *p
}

// writeText writes the income for people: the year in a few lines, the
// months as bars, the holdings, and the projection.
func (o incomeOutput) writeText(w io.Writer) error {
	summary := [][2]string{
		{"Income", fmt.Sprintf("%s a year, %s after withholding", o.AnnualIncome.text(), o.NetAnnualIncome.text())},
	}
	if o.YieldPct != nil {
		summary = append(summary, [2]string{"Yield", fmt.Sprintf("%.2f%%", *o.YieldPct)})
	}
	if o.GrowthRate != nil {
		summary = append(summary, [2]string{"Growth", percent(o.GrowthRate) + " a year, weighted by income"})
	}
	if n := len(o.Projection); n > 0 {
		last := o.Projection[n-1]
		how := "at today's growth"
		if o.DRIP {
			how += ", dividends reinvested"
		}
		in := fmt.Sprintf("In %d years", last.Year)
		if last.Year == 1 {
			in = "In 1 year"
		}
		summary = append(summary, [2]string{in,
			fmt.Sprintf("%s a year, %s after withholding (%s)", last.Income.text(), last.NetIncome.text(), how)})
	}
	if err := render.Fields(w, summary); err != nil {
		return err
	}

	if m := o.IncomeYear; m != nil && len(m.ByMonth) > 0 {
		fmt.Fprintln(w)
		heading := "By month"
		if m.NetOfWithholding {
			heading += ", after withholding"
		}
		if m.AverageMonth != nil {
			heading += "; " + m.AverageMonth.text() + " on average"
		}
		fmt.Fprintln(w, heading)
		rows := make([][]string, 0, 12)
		for _, mo := range m.ByMonth {
			rows = append(rows, []string{time.Month(mo.Month).String()[:3], mo.Income.text(), ""})
		}
		bars(m.ByMonth, rows, 2)
		render.AlignRight([]string{"", "", ""}, rows, 1)
		for _, row := range rows {
			fmt.Fprintln(w, strings.TrimRight(strings.Join(row, "  "), " "))
		}
	}

	if len(o.Holdings) > 0 {
		fmt.Fprintln(w)
		headers := []string{"SYMBOL", "INCOME", "NET", "GROWTH", "5Y CAGR", "STREAK"}
		rows := make([][]string, 0, len(o.Holdings))
		for _, h := range o.Holdings {
			streak := "-"
			if h.StreakYears != nil {
				streak = fmt.Sprintf("%d years", *h.StreakYears)
			}
			rows = append(rows, []string{h.Symbol, h.Income.text(), h.NetIncome.text(), percent(h.Growth), percent(h.CAGR5y), streak})
		}
		render.AlignRight(headers, rows, 1, 2, 3, 4)
		if err := render.Table(w, headers, rows); err != nil {
			return err
		}
	}
	return nil
}

// bars fills column col of rows with a bar as long as the month's income,
// the biggest month 24 blocks. The amounts are decimal strings, and a bar
// only needs to be about right, so a float64 is fine here, and only here.
func bars(months []incomeMonth, rows [][]string, col int) {
	const width = 24
	values := make([]float64, len(months))
	var biggest float64
	for i, m := range months {
		values[i], _ = strconv.ParseFloat(m.Income.Amount, 64)
		biggest = max(biggest, values[i])
	}
	if biggest <= 0 {
		return
	}
	for i, v := range values {
		rows[i][col] = strings.Repeat("█", int(v/biggest*width+0.5))
	}
}

// percent shows a fraction as a percentage: 0.0446 is "4.5%".
func percent(f *float64) string {
	if f == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", *f*100)
}
