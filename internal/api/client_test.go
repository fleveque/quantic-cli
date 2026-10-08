package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// serve answers every request with status and body, and remembers the last
// request it saw.
func serve(t *testing.T, status int, contentType, body string) (*Client, *http.Request) {
	t.Helper()
	var last http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last = *r
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Retry-After", "0") // keeps a 429 test fast
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	c, err := New(Config{BaseURL: srv.URL, UserAgent: "quantic-cli/test"})
	if err != nil {
		t.Fatal(err)
	}
	return c, &last
}

func TestCalendar(t *testing.T) {
	c, req := serve(t, 200, "application/json", `{"from":"2026-10-08","days":7,"stocks":[
		{"symbol":"KO","name":"Coca-Cola","ex_dividend_date":"2026-10-17","payment_frequency":"quarterly","sector":null}]}`)

	cal, err := c.Calendar(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Path != "/api/v1/calendar" || req.URL.RawQuery != "days=7" {
		t.Errorf("requested %s, want /api/v1/calendar?days=7", req.URL)
	}
	if got := req.Header.Get("User-Agent"); got != "quantic-cli/test" {
		t.Errorf("User-Agent = %q, want quantic-cli/test", got)
	}
	if len(cal.Stocks) != 1 || cal.Stocks[0].Symbol != "KO" || cal.Stocks[0].Sector != nil {
		t.Errorf("stocks = %+v", cal.Stocks)
	}
}

// The symbol is a path segment, escaped as one: a slash in it can't reach
// another endpoint.
func TestStockEscapesTheSymbol(t *testing.T) {
	c, req := serve(t, 404, "application/json", `{"error":{"code":"not_found","message":"no"}}`)
	c.Stock(t.Context(), "A/../../me")
	if got := req.URL.EscapedPath(); got != "/api/v1/stocks/A%2F..%2F..%2Fme" {
		t.Errorf("requested %s", got)
	}
}

// A path in QUANTIC_URL is kept: Quantic behind a proxy at /quantic works.
func TestBaseURLWithAPath(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[]}`))
	}))
	t.Cleanup(srv.Close)

	c, err := New(Config{BaseURL: srv.URL + "/quantic"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SearchStocks(t.Context(), "coca cola"); err != nil {
		t.Fatal(err)
	}
	if got != "/quantic/api/v1/stocks?q=coca+cola" {
		t.Errorf("requested %s", got)
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		want        StatusError
	}{
		{"an API error", 404, "application/json",
			`{"error":{"code":"not_found","message":"Quantic doesn't track ZZZ."}}`,
			StatusError{404, "not_found", "Quantic doesn't track ZZZ."}},
		{"still rate limited", 429, "application/json",
			`{"error":{"code":"rate_limited","message":"Rate limited."}}`,
			StatusError{429, "rate_limited", "Rate limited."}},
		{"a proxy's HTML page", 502, "text/html", `<h1>Bad Gateway</h1>`,
			StatusError{502, "", "Quantic answered 502 Bad Gateway, not JSON"}},
		{"a 200 that isn't JSON", 200, "text/html", `<h1>Maintenance</h1>`,
			StatusError{200, "", "Quantic answered 200 OK, not JSON"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := serve(t, tt.status, tt.contentType, tt.body)
			_, err := c.Stock(t.Context(), "ZZZ")
			got, ok := errors.AsType[*StatusError](err)
			if !ok {
				t.Fatalf("err = %v (%T), want a *StatusError", err, err)
			}
			if *got != tt.want {
				t.Errorf("err = %+v, want %+v", *got, tt.want)
			}
		})
	}
}

func TestUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // nothing listens there now

	c, err := New(Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Calendar(t.Context(), 7)
	unreachable, ok := errors.AsType[*UnreachableError](err)
	if !ok {
		t.Fatalf("err = %v (%T), want an *UnreachableError", err, err)
	}
	if !strings.HasPrefix(err.Error(), "can't reach Quantic at 127.0.0.1:") || strings.Contains(err.Error(), "/api/v1") {
		t.Errorf("message = %q", err)
	}
	if unreachable.Host == "" {
		t.Error("Host is empty")
	}
}

func TestDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // never answers
	}))
	t.Cleanup(srv.Close)
	c, err := New(Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = c.Calendar(ctx, 7)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want one that is context.DeadlineExceeded", err)
	}
}

func TestNewRejectsWhatIsntAnHTTPURL(t *testing.T) {
	for _, base := range []string{"", "quantic.finance", "ftp://quantic.finance", "https://", "://x"} {
		if _, err := New(Config{BaseURL: base}); err == nil {
			t.Errorf("New(%q) = nil error", base)
		}
	}
}
