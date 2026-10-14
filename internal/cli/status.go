package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/spf13/cobra"

	"github.com/fleveque/quantic-cli/internal/api"
	"github.com/fleveque/quantic-cli/internal/render"
)

// statusOutput is `quantic status --json`, the widget's one call (design §8):
// what a glance needs, from three calls to Quantic made at once. A part that
// failed is null, or empty, and says why in Errors.
type statusOutput struct {
	envelope
	SignedIn  bool           `json:"signed_in"`
	Portfolio *string        `json:"portfolio"`
	Next      *statusEvent   `json:"next"`       // your holdings' next ex-date or payment; null if none in the window
	ThisMonth *statusMonth   `json:"this_month"` // null when signed out
	Upcoming  []upcomingItem `json:"upcoming"`   // the next statusDays days, as `quantic upcoming`
	Errors    []statusError  `json:"errors"`     // empty when every part worked
}

type statusEvent struct {
	Symbol    string  `json:"symbol"`
	Name      *string `json:"name"`
	Event     string  `json:"event"` // "ex_date" or "pay_date"
	Date      string  `json:"date"`
	InDays    int     `json:"in_days"` // 0 is today
	Expected  *money  `json:"expected"`
	Portfolio *string `json:"portfolio"`
}

type statusMonth struct {
	Month string `json:"month"` // "2026-10"
	// Received is what was paid to you this month so far, one sum per
	// currency it was paid in: Quantic doesn't convert them. Null when that
	// part failed.
	Received []money `json:"received"`
	// Projected is what a typical such month pays, in your preferred
	// currency. Null when that part failed, or the portfolio is too small
	// for Quantic to give the months a shape.
	Projected *money `json:"projected"`
	// NetOfWithholding: both are after withholding abroad (your setting in
	// Quantic), or both before.
	NetOfWithholding bool `json:"net_of_withholding"`
}

// statusError is a part that failed: "upcoming", "income" or "dividends".
type statusError struct {
	Part    string `json:"part"`
	Message string `json:"message"`
}

const (
	statusSchema = "quantic.cli/status/v1"
	statusDays   = 14
)

func newStatusCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Your next dividend, and this month's: one call for a status bar",
		Long: `What a glance needs: your next ex-date or payment, what this month has paid
and what a month like it usually pays, and the next ` + strconv.Itoa(statusDays) + ` days.

The three calls to Quantic are made at once. One that fails leaves its part
out and says why, in "errors" with --json, and the rest still shows: the exit
code is 0. Only when every call fails does it exit as they did.`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd, opts)
		},
	}
}

// statusPart is one call's outcome.
type statusPart[T any] struct {
	v   *T
	at  fetched
	err error
}

func runStatus(cmd *cobra.Command, opts *Options) error {
	w := cmd.OutOrStdout()
	now := opts.now()
	s, err := opts.newSession(tokenNeeded)
	if err != nil {
		// Signed out is a state the widget shows, not only an exit code.
		if opts.JSON && ExitCode(err) == ExitNotSignedIn {
			out := statusOutput{envelope: newEnvelope(statusSchema, fetched{At: now}),
				Portfolio: optional(opts.Portfolio), Upcoming: []upcomingItem{}, Errors: []statusError{}}
			if err := render.JSON(w, out); err != nil {
				return err
			}
		}
		return err
	}

	today := civilDate(now)
	firstOfMonth := today.AddDate(0, 0, 1-today.Day())
	portfolio := optional(opts.Portfolio)

	var upcoming statusPart[api.Upcoming]
	var income statusPart[api.Income]
	var dividends statusPart[api.DividendList]

	// Three calls at once, each its own goroutine writing only its own part.
	// Not errgroup: with WithContext its first error cancels the others, and
	// here one part failing must not take the others with it. wg.Wait is
	// what makes the parts safe to read afterwards.
	var wg sync.WaitGroup
	wg.Go(func() {
		days := statusDays
		upcoming.v, upcoming.at, upcoming.err = askQuantic(cmd, opts, s, func(ctx context.Context, c *api.Client) (*api.Upcoming, error) {
			return c.Upcoming(ctx, api.ListUpcomingParams{Days: &days, Portfolio: portfolio})
		})
	})
	wg.Go(func() {
		years := 1
		income.v, income.at, income.err = askQuantic(cmd, opts, s, func(ctx context.Context, c *api.Client) (*api.Income, error) {
			return c.Income(ctx, api.GetIncomeParams{Years: &years, Portfolio: portfolio})
		})
	})
	wg.Go(func() {
		params := api.ListDividendsParams{
			From:      &openapi_types.Date{Time: firstOfMonth},
			To:        &openapi_types.Date{Time: today},
			Portfolio: portfolio,
		}
		dividends.v, dividends.at, dividends.err = askQuantic(cmd, opts, s, func(ctx context.Context, c *api.Client) (*api.DividendList, error) {
			return c.Dividends(ctx, params)
		})
	})
	wg.Wait()

	// Every part failed: there's nothing to show, so fail as the first did.
	// It's the same reason three times, nearly always: offline, or a token
	// that was revoked.
	if upcoming.err != nil && income.err != nil && dividends.err != nil {
		return upcoming.err
	}

	out := statusOutput{
		SignedIn:  true,
		Portfolio: portfolio,
		ThisMonth: &statusMonth{Month: today.Format("2006-01")},
		Upcoming:  []upcomingItem{},
		Errors:    []statusError{},
	}
	var ats []fetched
	fail := func(part string, err error) {
		out.Errors = append(out.Errors, statusError{Part: part, Message: err.Error()})
		fmt.Fprintf(cmd.ErrOrStderr(), "quantic: no %s: %v\n", part, err)
	}

	if upcoming.err != nil {
		fail("upcoming", upcoming.err)
	} else {
		ats = append(ats, upcoming.at)
		out.Upcoming = upcomingItems(upcoming.v)
		out.Next = nextEvent(out.Upcoming, today)
	}

	if income.err != nil {
		fail("income", income.err)
	} else {
		ats = append(ats, income.at)
		if y := income.v.IncomeYear; y != nil {
			out.ThisMonth.NetOfWithholding = y.NetOfWithholding != nil && *y.NetOfWithholding
			if y.ByMonth != nil {
				if m, ok := (*y.ByMonth)[strconv.Itoa(int(today.Month()))]; ok {
					out.ThisMonth.Projected = (*money)(&m)
				}
			}
		}
	}

	if dividends.err != nil {
		fail("dividends", dividends.err)
	} else {
		ats = append(ats, dividends.at)
		// Summed as the projection is: gross, or net when it's net. Without
		// the income part, gross.
		received, err := sumByCurrency(dividends.v.Dividends, out.ThisMonth.NetOfWithholding)
		if err != nil {
			fail("dividends", err)
		} else {
			out.ThisMonth.Received = received
		}
	}

	// The oldest answer is how old the whole is, and it's stale if any part
	// is: one warning, not three.
	at := ats[0]
	for _, a := range ats[1:] {
		if a.At.Before(at.At) {
			at.At = a.At
		}
		at.Stale = at.Stale || a.Stale
	}
	if at.Stale {
		warnStale(cmd, opts, s, at)
	}
	out.envelope = newEnvelope(statusSchema, at)

	if opts.JSON {
		return render.JSON(w, out)
	}
	return out.writeText(w, today)
}

// civilDate is t's date, at midnight UTC: the day as the person running the
// command sees it, whatever their time zone, made comparable with the dates
// Quantic sends, which are parsed at midnight UTC.
func civilDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// nextEvent is your holdings' first ex-date or payment from today on. Items
// come soonest first, but an item's ex-date can be past while its payment
// is to come, so every date is looked at. The radar has no dividend of
// yours, so it's left out. Two on the same day: the first in Quantic's
// order. A stock held in several portfolios is one event: what they pay
// together, with no portfolio.
func nextEvent(items []upcomingItem, today time.Time) *statusEvent {
	var next *statusEvent
	for _, it := range items {
		if !it.Held {
			continue
		}
		for _, e := range []struct {
			name string
			date *string
		}{{"ex_date", it.ExDate}, {"pay_date", it.PayDate}} {
			if e.date == nil {
				continue
			}
			d, err := time.Parse(time.DateOnly, *e.date)
			if err != nil || d.Before(today) {
				continue
			}
			days := int(d.Sub(today).Hours() / 24)
			switch {
			case next == nil || days < next.InDays:
				next = &statusEvent{Symbol: it.Symbol, Name: it.Name, Event: e.name, Date: *e.date,
					InDays: days, Expected: it.Expected, Portfolio: it.Portfolio}
			case next.Symbol == it.Symbol && next.Event == e.name && next.Date == *e.date:
				next.Portfolio = nil
				if next.Expected != nil && it.Expected != nil {
					sum, err := addMoney(*next.Expected, *it.Expected)
					if err == nil {
						next.Expected = &sum
					} else {
						next.Expected = nil // can't be added up: better none than half
					}
				}
			}
		}
	}
	return next
}

// sumByCurrency adds up the dividends, gross or net, one sum per currency,
// in alphabetical order of currency.
func sumByCurrency(divs []api.Dividend, net bool) ([]money, error) {
	sums := map[string]money{}
	for _, d := range divs {
		m := money(d.Amount)
		if net {
			m = money(d.Net)
		}
		sum, ok := sums[m.Currency]
		if !ok {
			sum = money{Amount: "0", Currency: m.Currency}
		}
		sum, err := addMoney(sum, m)
		if err != nil {
			return nil, fmt.Errorf("%s's dividend on %s: %w", d.Symbol, d.Date.Format(time.DateOnly), err)
		}
		sums[m.Currency] = sum
	}
	// Not slices.Collect(maps.Values(sums)): of an empty map, that's nil,
	// null in the JSON, which says the part failed. Nothing paid yet is [].
	out := slices.AppendSeq(make([]money, 0, len(sums)), maps.Values(sums))
	slices.SortFunc(out, func(a, b money) int { return strings.Compare(a.Currency, b.Currency) })
	return out, nil
}

// addMoney adds two amounts in the same currency. They're decimal strings,
// and they're added exactly, as fractions (math/big.Rat), never as
// float64s; the sum keeps as many decimal places as the more precise one.
func addMoney(a, b money) (money, error) {
	if a.Currency != b.Currency {
		return money{}, fmt.Errorf("can't add %s to %s", a.text(), b.text())
	}
	x, ok := new(big.Rat).SetString(a.Amount)
	if !ok {
		return money{}, fmt.Errorf("%q isn't an amount", a.Amount)
	}
	y, ok := new(big.Rat).SetString(b.Amount)
	if !ok {
		return money{}, fmt.Errorf("%q isn't an amount", b.Amount)
	}
	return money{Amount: x.Add(x, y).FloatString(max(places(a.Amount), places(b.Amount))), Currency: a.Currency}, nil
}

// places is how many digits a decimal string has after its point.
func places(amount string) int {
	if i := strings.IndexByte(amount, '.'); i >= 0 {
		return len(amount) - i - 1
	}
	return 0
}

// writeText is status for people: the next event and the month in a few
// lines, then the next days as `quantic upcoming` shows them.
func (o statusOutput) writeText(w io.Writer, today time.Time) error {
	var fields [][2]string
	if slices.ContainsFunc(o.Errors, func(e statusError) bool { return e.Part == "upcoming" }) {
		fields = append(fields, [2]string{"Next", "-"})
	} else if e := o.Next; e != nil {
		what := "goes ex-dividend"
		if e.Event == "pay_date" {
			what = "pays"
		}
		line := fmt.Sprintf("%s %s %s (%s)", e.Symbol, what, inDays(e.InDays), e.Date)
		if e.Expected != nil {
			line += ", " + e.Expected.text()
		}
		fields = append(fields, [2]string{"Next", line})
	} else {
		fields = append(fields, [2]string{"Next", fmt.Sprintf("nothing of yours in the next %d days", statusDays)})
	}

	if m := o.ThisMonth; m != nil {
		var parts []string
		if m.Received != nil {
			received := "nothing received yet"
			if len(m.Received) > 0 {
				texts := make([]string, len(m.Received))
				for i, r := range m.Received {
					texts[i] = r.text()
				}
				received = strings.Join(texts, " + ") + " received"
			}
			parts = append(parts, received)
		}
		if m.Projected != nil {
			parts = append(parts, m.Projected.text()+" in a typical "+today.Month().String())
		}
		if len(parts) > 0 {
			line := strings.Join(parts, "; ")
			if m.NetOfWithholding {
				line += ", after withholding"
			}
			fields = append(fields, [2]string{today.Month().String(), line})
		}
	}
	if err := render.Fields(w, fields); err != nil {
		return err
	}

	if len(o.Upcoming) > 0 {
		fmt.Fprintf(w, "\nThe next %d days\n", statusDays)
		return writeUpcoming(w, o.Upcoming)
	}
	return nil
}

// inDays says when, as people do: "today", "tomorrow", "in 3 days".
func inDays(n int) string {
	switch n {
	case 0:
		return "today"
	case 1:
		return "tomorrow"
	}
	return fmt.Sprintf("in %d days", n)
}
