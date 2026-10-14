package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fleveque/quantic-cli/internal/cli"
)

// oct15 is the day the status fixtures were recorded for.
var oct15 = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

// inFront puts a Quantic in front of the fake one: handle answers what it
// wants to, and returns false to pass the request on.
func inFront(t *testing.T, handle func(w http.ResponseWriter, r *http.Request) bool) {
	t.Helper()
	fake := fakeQuanticServer(t)
	u, _ := url.Parse(fake.URL)
	proxy := httputil.NewSingleHostReverseProxy(u)
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !handle(w, r) {
			proxy.ServeHTTP(w, r)
		}
	}))
	t.Cleanup(front.Close)
	t.Setenv("QUANTIC_URL", front.URL)
}

// The three calls are made at once: none is answered until all three have
// arrived. Made one after another, the first would wait alone, and fail.
func TestStatusAsksAtOnce(t *testing.T) {
	var mu sync.Mutex
	arrived := 0
	all := make(chan struct{})
	inFront(t, func(w http.ResponseWriter, _ *http.Request) bool {
		mu.Lock()
		if arrived++; arrived == 3 {
			close(all)
		}
		mu.Unlock()
		select {
		case <-all:
			return false
		case <-time.After(2 * time.Second):
			w.WriteHeader(http.StatusTeapot)
			return true
		}
	})
	t.Setenv("QUANTIC_TOKEN", testToken)

	code, _, stderr := runAt(t, oct15, "status", "--json")
	if code != cli.ExitOK || stderr != "" {
		t.Errorf("exit %d, stderr %q; want 0 and nothing", code, stderr)
	}
}

// One part failing leaves that part out and says why; the rest still shows,
// and the exit code is 0 (design §8).
func TestStatusPartialFailure(t *testing.T) {
	inFront(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/income" {
			return false
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"code":"internal","message":"Something went wrong."}}`))
		return true
	})
	t.Setenv("QUANTIC_TOKEN", testToken)

	code, stdout, stderr := runAt(t, oct15, "status", "--json")
	if code != cli.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if stderr != "quantic: no income: Something went wrong.\n" {
		t.Errorf("stderr = %q", stderr)
	}
	var out struct {
		Next      *struct{ Symbol string }
		ThisMonth struct {
			Received  []struct{ Amount, Currency string }
			Projected *struct{ Amount string }
		} `json:"this_month"`
		Upcoming []json.RawMessage
		Errors   []struct{ Part, Message string }
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Errors) != 1 || out.Errors[0].Part != "income" || out.Errors[0].Message != "Something went wrong." {
		t.Errorf("errors = %+v", out.Errors)
	}
	if out.ThisMonth.Projected != nil {
		t.Errorf("projected = %+v, want null", out.ThisMonth.Projected)
	}
	// Without the income part, received is gross: 18.00 + 90.00 + 58.50.
	if len(out.ThisMonth.Received) != 2 || out.ThisMonth.Received[1].Amount != "166.50" {
		t.Errorf("received = %+v", out.ThisMonth.Received)
	}
	if out.Next == nil || out.Next.Symbol != "O" || len(out.Upcoming) != 8 {
		t.Errorf("next %+v, %d upcoming", out.Next, len(out.Upcoming))
	}

	// For people: the month still shows what it has.
	_, stdout, _ = runAt(t, oct15, "status")
	if !strings.Contains(stdout, "October  58.50 GBP + 166.50 USD received\n") {
		t.Errorf("stdout:\n%s", stdout)
	}
}

// Every part failing is a failure: nothing on stdout, and the exit code of
// why, as any other command.
func TestStatusAllFail(t *testing.T) {
	srv := fakeQuanticServer(t)
	srv.Close()
	t.Setenv("QUANTIC_TOKEN", testToken)

	code, stdout, stderr := runAt(t, oct15, "status", "--json")
	if code != cli.ExitUnreachable || stdout != "" || !strings.HasPrefix(stderr, "quantic: can't reach Quantic at ") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}

	fakeQuantic(t)
	code, stdout, stderr = runAt(t, oct15, "status", "--portfolio", "nope")
	if code != cli.ExitUsage || stdout != "" || !strings.HasPrefix(stderr, `quantic: Unknown portfolio "nope".`) {
		t.Errorf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

// Signed out is a state the widget shows: --json still says so, with exit 4.
func TestStatusSignedOut(t *testing.T) {
	requests := fakeQuantic(t)

	code, stdout, stderr := runAt(t, oct15, "status", "--json")
	if code != cli.ExitNotSignedIn || !strings.Contains(stderr, "not signed in") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	want := `{
  "schema": "quantic.cli/status/v1",
  "fetched_at": "2026-10-15T09:00:00Z",
  "stale": false,
  "signed_in": false,
  "portfolio": null,
  "next": null,
  "this_month": null,
  "upcoming": [],
  "errors": []
}
`
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}

	code, stdout, _ = runAt(t, oct15, "status")
	if code != cli.ExitNotSignedIn || stdout != "" {
		t.Errorf("without --json: exit %d, stdout %q", code, stdout)
	}
	if len(*requests) != 0 {
		t.Errorf("asked Quantic %v", *requests)
	}
}

// status asks for the next 14 days, this month's dividends to today, and one
// year of income, each for --portfolio when it's given.
func TestStatusRequests(t *testing.T) {
	requests := fakeQuantic(t)
	t.Setenv("QUANTIC_TOKEN", testToken)
	runAt(t, oct15, "status", "--portfolio", "Pension")

	got := map[string]bool{}
	for _, r := range *requests {
		got[r] = true
	}
	for _, want := range []string{
		"GET /api/v1/upcoming?days=14&portfolio=Pension",
		"GET /api/v1/dividends?from=2026-10-01&to=2026-10-15&portfolio=Pension",
		"GET /api/v1/income?years=1&portfolio=Pension",
	} {
		if !got[want] {
			t.Errorf("no %s in %v", want, *requests)
		}
	}
	if len(*requests) != 3 {
		t.Errorf("%d requests: %v", len(*requests), *requests)
	}
}
