package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// limited answers 429 to its first `limit` requests, then 200, and counts
// every request it gets.
func limited(t *testing.T, limit int32, header http.Header) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= limit {
			for k, v := range header {
				w.Header()[k] = v
			}
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func get(t *testing.T, rt http.RoundTripper, ctx context.Context, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := rt.RoundTrip(req)
	if err == nil {
		t.Cleanup(func() { resp.Body.Close() })
	}
	return resp, err
}

func TestRetryTransport(t *testing.T) {
	tests := []struct {
		name      string
		limit     int32 // how many 429s before a 200
		wantCode  int
		wantCalls int32
	}{
		{"no limit, one call", 0, http.StatusOK, 1},
		{"limited twice, then through", 2, http.StatusOK, 3},
		{"limited throughout: gives up after 3", 100, http.StatusTooManyRequests, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, calls := limited(t, tt.limit, nil)
			rt := &retryTransport{next: http.DefaultTransport, attempts: 3, base: time.Millisecond}

			resp, err := get(t, rt, t.Context(), srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tt.wantCode {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantCode)
			}
			if got := calls.Load(); got != tt.wantCalls {
				t.Errorf("requests = %d, want %d", got, tt.wantCalls)
			}
		})
	}
}

// A wait that would outlast the caller's deadline isn't started: the 429
// comes back at once, so the caller can say "rate limited" (exit 5) instead
// of "timed out" (exit 3).
func TestRetryTransportStopsBeforeTheDeadline(t *testing.T) {
	srv, calls := limited(t, 100, http.Header{"Retry-After": {"30"}})
	rt := &retryTransport{next: http.DefaultTransport, attempts: 3, base: time.Millisecond}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	start := time.Now()
	resp, err := get(t, rt, ctx, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusTooManyRequests || calls.Load() != 1 {
		t.Errorf("status %d after %d requests, want 429 after 1", resp.StatusCode, calls.Load())
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("took %s: it waited for a retry it could never make", elapsed)
	}
}

func TestRetryAfter(t *testing.T) {
	if got := retryAfter(http.Header{"Retry-After": {"7"}}, time.Second); got != 7*time.Second {
		t.Errorf("Retry-After: 7 → %s, want 7s", got)
	}
	// An HTTP date is valid Retry-After too; Quantic doesn't send one, and
	// backoff is a fine answer to it.
	seen := map[time.Duration]bool{}
	for range 100 {
		got := retryAfter(http.Header{"Retry-After": {"Wed, 21 Oct 2026 07:28:00 GMT"}}, time.Second)
		if got < 500*time.Millisecond || got >= 1500*time.Millisecond {
			t.Fatalf("backoff 1s → %s, want within [0.5s, 1.5s)", got)
		}
		seen[got] = true
	}
	// Without jitter every wait is the same, and clients limited together
	// retry together.
	if len(seen) < 50 {
		t.Errorf("100 waits took %d distinct values: no jitter?", len(seen))
	}
}
