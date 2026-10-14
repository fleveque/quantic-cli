package cli

import (
	"testing"
	"time"
)

func ptr(s string) *string { return &s }

func TestNextEvent(t *testing.T) {
	today := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	usd := func(a string) *money { return &money{Amount: a, Currency: "USD"} }

	tests := []struct {
		name  string
		items []upcomingItem
		want  *statusEvent
	}{
		{"nothing", nil, nil},
		{"the radar isn't yours", []upcomingItem{
			{Symbol: "MSFT", ExDate: ptr("2026-10-16")},
		}, nil},
		{"ex-dated already: its payment is next", []upcomingItem{
			{Symbol: "KO", Held: true, ExDate: ptr("2026-10-01"), PayDate: ptr("2026-10-20"), Expected: usd("10.00")},
			{Symbol: "PEP", Held: true, ExDate: ptr("2026-10-25")},
		}, &statusEvent{Symbol: "KO", Event: "pay_date", Date: "2026-10-20", InDays: 5, Expected: usd("10.00")}},
		{"today is in", []upcomingItem{
			{Symbol: "KO", Held: true, ExDate: ptr("2026-10-15")},
		}, &statusEvent{Symbol: "KO", Event: "ex_date", Date: "2026-10-15", InDays: 0}},
		{"a later item with a sooner date wins", []upcomingItem{
			{Symbol: "KO", Held: true, ExDate: ptr("2026-10-14"), PayDate: ptr("2026-10-30")},
			{Symbol: "PEP", Held: true, ExDate: ptr("2026-10-18")},
		}, &statusEvent{Symbol: "PEP", Event: "ex_date", Date: "2026-10-18", InDays: 3}},
		{"one stock in two portfolios is one event, added up", []upcomingItem{
			{Symbol: "O", Held: true, ExDate: ptr("2026-10-17"), Expected: usd("54.72"), Portfolio: ptr("Main")},
			{Symbol: "O", Held: true, ExDate: ptr("2026-10-17"), Expected: usd("10.9"), Portfolio: ptr("Pension")},
		}, &statusEvent{Symbol: "O", Event: "ex_date", Date: "2026-10-17", InDays: 2, Expected: usd("65.62")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextEvent(tt.items, today)
			if (got == nil) != (tt.want == nil) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			if got == nil {
				return
			}
			if got.Symbol != tt.want.Symbol || got.Event != tt.want.Event || got.Date != tt.want.Date ||
				got.InDays != tt.want.InDays || (got.Portfolio == nil) != (tt.want.Portfolio == nil) ||
				(got.Expected == nil) != (tt.want.Expected == nil) ||
				(got.Expected != nil && *got.Expected != *tt.want.Expected) {
				t.Errorf("got %+v (expected %v), want %+v (expected %v)", got, got.Expected, tt.want, tt.want.Expected)
			}
		})
	}
}

func TestAddMoney(t *testing.T) {
	tests := []struct{ a, b, want string }{
		{"0.1", "0.2", "0.3"}, // 0.30000000000000004 as float64s
		{"54.72", "10.9", "65.62"},
		{"0", "0.2736", "0.2736"},
		{"1", "2", "3"},
	}
	for _, tt := range tests {
		got, err := addMoney(money{tt.a, "USD"}, money{tt.b, "USD"})
		if err != nil || got != (money{tt.want, "USD"}) {
			t.Errorf("%s + %s = %v, %v; want %s", tt.a, tt.b, got, err, tt.want)
		}
	}
	if _, err := addMoney(money{"1", "USD"}, money{"1", "EUR"}); err == nil {
		t.Error("USD + EUR: no error")
	}
	if _, err := addMoney(money{"1", "USD"}, money{"1,5", "USD"}); err == nil {
		t.Error(`"1,5": no error`)
	}
}

// Nothing paid yet this month is an empty list, never nil: null in the JSON
// would say the part failed.
func TestSumByCurrencyOfNothing(t *testing.T) {
	got, err := sumByCurrency(nil, false)
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("got %#v, %v; want an empty list", got, err)
	}
}
