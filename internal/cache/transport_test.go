package cache_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fleveque/quantic-cli/internal/cache"
)

// quantic is a fake Quantic: it answers body with an ETag, a 304 when the
// request's If-None-Match matches, and whatever status is set instead of
// either. It counts the requests that reach it.
type quantic struct {
	srv      *httptest.Server
	requests atomic.Int32
	mu       sync.Mutex
	body     string
	status   int           // 0: answer normally
	delay    time.Duration // before answering
	ifNone   []string      // If-None-Match of each request
}

func newQuantic(t *testing.T, body string) *quantic {
	q := &quantic{body: body}
	q.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q.requests.Add(1)
		q.mu.Lock()
		body, status, delay := q.body, q.status, q.delay
		q.ifNone = append(q.ifNone, r.Header.Get("If-None-Match"))
		q.mu.Unlock()
		time.Sleep(delay)
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		etag := `W/"` + body + `"`
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		io.WriteString(w, body)
	}))
	t.Cleanup(q.srv.Close)
	return q
}

func (q *quantic) set(body string, status int) {
	q.mu.Lock()
	q.body, q.status = body, status
	q.mu.Unlock()
}

// clock is a time that moves only when told to.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time                  { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Add(d time.Duration)             { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }
func newClock() *clock                           { return &clock{now: time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)} }
func fifteenMinutes(*http.Request) time.Duration { return 15 * time.Minute }

func newTransport(t *testing.T, c *clock) *cache.Transport {
	return &cache.Transport{
		Store:  cache.Store{Dir: filepath.Join(t.TempDir(), "quantic")},
		MaxAge: fifteenMinutes,
		Now:    c.Now,
	}
}

// get asks for url through tr, as the api package's client would.
func get(t *testing.T, tr http.RoundTripper, url string, header ...string) (string, error) {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", errors.New(resp.Status)
	}
	return string(b), nil
}

func mustGet(t *testing.T, tr http.RoundTripper, url string, header ...string) string {
	t.Helper()
	body, err := get(t, tr, url, header...)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// Fresh: answered from the cache, and Quantic isn't asked.
func TestAFreshAnswerIsntAskedAgain(t *testing.T) {
	q, c := newQuantic(t, `{"n":1}`), newClock()
	tr := newTransport(t, c)

	mustGet(t, tr, q.srv.URL+"/api/v1/holdings")
	first := tr.Last()
	c.Add(14 * time.Minute)
	if got := mustGet(t, tr, q.srv.URL+"/api/v1/holdings"); got != `{"n":1}` {
		t.Errorf("body = %s", got)
	}
	if n := q.requests.Load(); n != 1 {
		t.Errorf("Quantic was asked %d times, want 1", n)
	}
	if last := tr.Last(); !last.FetchedAt.Equal(first.FetchedAt) || last.Stale {
		t.Errorf("Last = %+v, want the first fetch's time, not stale", last)
	}
}

// Stale: revalidated with the ETag; a 304 keeps the body and counts as
// fetched now.
func TestAnOldAnswerIsRevalidated(t *testing.T) {
	q, c := newQuantic(t, `{"n":1}`), newClock()
	tr := newTransport(t, c)

	mustGet(t, tr, q.srv.URL+"/api/v1/holdings")
	c.Add(16 * time.Minute)
	if got := mustGet(t, tr, q.srv.URL+"/api/v1/holdings"); got != `{"n":1}` {
		t.Errorf("after a 304, body = %s", got)
	}
	if q.ifNone[1] != `W/"{"n":1}"` {
		t.Errorf("If-None-Match = %q, want the ETag", q.ifNone[1])
	}
	if last := tr.Last(); !last.FetchedAt.Equal(c.Now()) || last.Stale {
		t.Errorf("Last = %+v, want fetched now", last)
	}

	// Changed on the server: a 200 with the new body replaces the entry.
	q.set(`{"n":2}`, 0)
	c.Add(16 * time.Minute)
	if got := mustGet(t, tr, q.srv.URL+"/api/v1/holdings"); got != `{"n":2}` {
		t.Errorf("after a change, body = %s", got)
	}
	c.Add(time.Minute)
	if got := mustGet(t, tr, q.srv.URL+"/api/v1/holdings"); got != `{"n":2}` || q.requests.Load() != 3 {
		t.Errorf("body = %s after %d requests; want the new body, from the cache", got, q.requests.Load())
	}
}

// Offline: the last answer, of any age, marked stale.
func TestUnreachableServesTheLastAnswer(t *testing.T) {
	for _, tt := range []struct {
		name string
		fail func(q *quantic)
	}{
		{"connection refused", func(q *quantic) { q.srv.Close() }},
		{"502", func(q *quantic) { q.set("", http.StatusBadGateway) }},
		{"503", func(q *quantic) { q.set("", http.StatusServiceUnavailable) }},
		{"504", func(q *quantic) { q.set("", http.StatusGatewayTimeout) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			q, c := newQuantic(t, `{"n":1}`), newClock()
			tr := newTransport(t, c)
			mustGet(t, tr, q.srv.URL+"/api/v1/holdings")
			fetched := c.Now()

			tt.fail(q)
			c.Add(30 * 24 * time.Hour)
			if got := mustGet(t, tr, q.srv.URL+"/api/v1/holdings"); got != `{"n":1}` {
				t.Errorf("body = %s", got)
			}
			if last := tr.Last(); !last.Stale || !last.FetchedAt.Equal(fetched) {
				t.Errorf("Last = %+v, want stale, fetched %s", last, fetched)
			}
		})
	}
}

// With nothing cached, being offline is an error, as without a cache.
func TestUnreachableWithNothingCached(t *testing.T) {
	q, c := newQuantic(t, `{"n":1}`), newClock()
	q.srv.Close()
	if _, err := get(t, newTransport(t, c), q.srv.URL+"/api/v1/holdings"); err == nil {
		t.Error("no error")
	}
}

// What Quantic says is never hidden: a 401, 404 or 429 passes through, even
// with an answer cached, and isn't stored.
func TestOtherAnswersPassThrough(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusTooManyRequests, http.StatusInternalServerError} {
		q, c := newQuantic(t, `{"n":1}`), newClock()
		tr := newTransport(t, c)
		mustGet(t, tr, q.srv.URL+"/api/v1/holdings")
		q.set("", status)
		c.Add(16 * time.Minute)
		want := fmt.Sprintf("%d %s", status, http.StatusText(status))
		if _, err := get(t, tr, q.srv.URL+"/api/v1/holdings"); err == nil || err.Error() != want {
			t.Errorf("err = %v, want Quantic's answer, %s", err, want)
		}
	}
}

// MaxAge 0 always asks, but still uses the ETag and still falls back.
func TestMaxAgeZeroAlwaysAsks(t *testing.T) {
	q, c := newQuantic(t, `{"n":1}`), newClock()
	tr := newTransport(t, c)
	tr.MaxAge = func(*http.Request) time.Duration { return 0 }

	mustGet(t, tr, q.srv.URL+"/api/v1/me")
	mustGet(t, tr, q.srv.URL+"/api/v1/me")
	if n := q.requests.Load(); n != 2 || q.ifNone[1] == "" {
		t.Errorf("%d requests, If-None-Match %q; want 2, the second with the ETag", n, q.ifNone[1])
	}
	q.srv.Close()
	if got := mustGet(t, tr, q.srv.URL+"/api/v1/me"); got != `{"n":1}` || !tr.Last().Stale {
		t.Errorf("offline: body %s, %+v", got, tr.Last())
	}
}

// Each token has its own entries, and the token isn't in any file.
func TestEachTokenHasItsOwnEntry(t *testing.T) {
	q, c := newQuantic(t, `{"n":1}`), newClock()
	tr := newTransport(t, c)

	mustGet(t, tr, q.srv.URL+"/api/v1/holdings", "Authorization", "Bearer qtc_SECRET_alice")
	q.set(`{"n":2}`, 0)
	if got := mustGet(t, tr, q.srv.URL+"/api/v1/holdings", "Authorization", "Bearer qtc_SECRET_bob"); got != `{"n":2}` {
		t.Errorf("bob got %s, alice's answer", got)
	}
	if got := mustGet(t, tr, q.srv.URL+"/api/v1/holdings"); got != `{"n":2}` || q.requests.Load() != 3 {
		t.Errorf("signed out: %s after %d requests; want its own entry", got, q.requests.Load())
	}

	filepath.WalkDir(tr.Store.Dir, func(path string, d os.DirEntry, err error) error {
		if d != nil && !d.IsDir() {
			if b, _ := os.ReadFile(path); strings.Contains(string(b), "SECRET") {
				t.Errorf("%s holds a token: %s", filepath.Base(path), b)
			}
		}
		return nil
	})
}

// The cache holds portfolio data: 0700 directory, 0600 files.
func TestTheCacheIsPrivate(t *testing.T) {
	q, c := newQuantic(t, `{"n":1}`), newClock()
	tr := newTransport(t, c)
	mustGet(t, tr, q.srv.URL+"/api/v1/holdings")

	info, _ := os.Stat(tr.Store.Dir)
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("directory %04o, want 0700", perm)
	}
	entries, _ := os.ReadDir(tr.Store.Dir)
	if len(entries) != 2 { // the entry and its lock
		t.Errorf("%d files, want 2", len(entries))
	}
	for _, e := range entries {
		info, _ := e.Info()
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s: %04o, want 0600", e.Name(), perm)
		}
	}
}

// Only a 200 with JSON is kept (a captive portal's HTML isn't), and only for
// GET.
func TestOnlyJSONAnswersToGETAreKept(t *testing.T) {
	c := newClock()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, "<html>")
	}))
	t.Cleanup(srv.Close)
	tr := newTransport(t, c)
	mustGet(t, tr, srv.URL+"/")
	mustGet(t, tr, srv.URL+"/")
	if n := requests.Load(); n != 2 {
		t.Errorf("HTML: %d requests, want 2 (not cached)", n)
	}

	q := newQuantic(t, `{"n":1}`)
	for range 2 {
		req, _ := http.NewRequest(http.MethodPost, q.srv.URL+"/api/v1/holdings", nil)
		resp, err := tr.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if n := q.requests.Load(); n != 2 {
		t.Errorf("POST: %d requests, want 2", n)
	}
}

// Two at once, for one entry: one asks Quantic, the other waits and uses
// its answer. flock is per open file, so goroutines with their own
// Transports stand in for two processes.
func TestOneFetchAtATime(t *testing.T) {
	q, c := newQuantic(t, `{"n":1}`), newClock()
	q.delay = 200 * time.Millisecond
	dir := filepath.Join(t.TempDir(), "quantic")

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			tr := &cache.Transport{Store: cache.Store{Dir: dir}, MaxAge: fifteenMinutes, Now: c.Now}
			if got, err := get(t, tr, q.srv.URL+"/api/v1/holdings"); got != `{"n":1}` {
				t.Errorf("body %s, %v", got, err)
			}
		})
	}
	wg.Wait()
	if n := q.requests.Load(); n != 1 {
		t.Errorf("Quantic was asked %d times, want 1", n)
	}
}

// Waiting for the lock ends with the caller's context.
func TestLockWaitEndsWithTheContext(t *testing.T) {
	s := cache.Store{Dir: t.TempDir()}
	unlock, err := s.Lock(t.Context(), "k")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := s.Lock(ctx, "k"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("waited %s", d)
	}
}

// An entry that isn't one (cut short, or garbage) is no entry.
func TestABrokenEntryIsIgnored(t *testing.T) {
	q, c := newQuantic(t, `{"n":1}`), newClock()
	tr := newTransport(t, c)
	mustGet(t, tr, q.srv.URL+"/api/v1/holdings")
	files, _ := filepath.Glob(filepath.Join(tr.Store.Dir, "*.json"))
	os.WriteFile(files[0], []byte(`{"fetched_at": "2026-10`), 0o600)

	// Offline, it isn't served: it isn't an answer.
	q.srv.Close()
	if body, err := get(t, tr, q.srv.URL+"/api/v1/holdings"); err == nil {
		t.Errorf("offline with a broken entry: %q, want an error", body)
	}
}

// A cache that can't be written is no cache: every request goes to Quantic,
// and none fails because of it.
func TestAnUnwritableCacheIsNoCache(t *testing.T) {
	q, c := newQuantic(t, `{"n":1}`), newClock()
	file := filepath.Join(t.TempDir(), "a-file")
	os.WriteFile(file, nil, 0o600)
	tr := &cache.Transport{Store: cache.Store{Dir: filepath.Join(file, "quantic")}, MaxAge: fifteenMinutes, Now: c.Now}
	for range 2 {
		if got := mustGet(t, tr, q.srv.URL+"/api/v1/holdings"); got != `{"n":1}` {
			t.Errorf("body = %s", got)
		}
	}
	if n := q.requests.Load(); n != 2 {
		t.Errorf("Quantic was asked %d times, want 2", n)
	}
}

func TestClear(t *testing.T) {
	q, c := newQuantic(t, `{"n":1}`), newClock()
	tr := newTransport(t, c)
	mustGet(t, tr, q.srv.URL+"/api/v1/holdings")
	if err := tr.Store.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tr.Store.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("directory still there: %v", err)
	}
	if err := tr.Store.Clear(); err != nil {
		t.Errorf("clearing an empty cache: %v", err)
	}
}
