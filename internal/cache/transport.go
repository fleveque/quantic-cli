package cache

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Transport is an http.RoundTripper that answers GET requests from the
// Store when it can:
//
//   - an entry younger than MaxAge is the answer, and Quantic isn't asked;
//   - an older one is revalidated: sent with If-None-Match, a 304 means it
//     is still right, and it counts as fetched now;
//   - when Quantic can't be reached (no connection, no answer in time, or a
//     502, 503 or 504 from what's in front of it), an entry of any age is
//     the answer, marked stale.
//
// Every other answer (a 401, a 404, a 429) passes through, not stored: the
// cache never hides what Quantic says.
type Transport struct {
	Store Store

	// Next sends what the cache can't answer; nil means http.DefaultTransport.
	Next http.RoundTripper

	// MaxAge is how long an answer to req stays fresh. Zero means "always
	// ask", which still revalidates with the ETag and still falls back to
	// the entry when Quantic can't be reached.
	MaxAge func(req *http.Request) time.Duration

	// Now is the clock; nil means time.Now.
	Now func() time.Time

	mu   sync.Mutex
	last Result
}

// Result says where the last answer came from.
type Result struct {
	FetchedAt time.Time // when Quantic gave (or confirmed) this answer
	Stale     bool      // Quantic couldn't be reached; this is the last answer it gave
}

// Last is the Result of the most recent RoundTrip.
func (t *Transport) Last() Result {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.last
}

func (t *Transport) record(r Result) {
	t.mu.Lock()
	t.last = r
	t.mu.Unlock()
}

// now is the clock, in UTC: entries say when in a form that reads the same
// on every machine.
func (t *Transport) now() time.Time {
	if t.Now != nil {
		return t.Now().UTC()
	}
	return time.Now().UTC()
}

func (t *Transport) next() http.RoundTripper {
	if t.Next != nil {
		return t.Next
	}
	return http.DefaultTransport
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return t.next().RoundTrip(req)
	}
	key := Key(req)
	maxAge := time.Duration(0)
	if t.MaxAge != nil {
		maxAge = t.MaxAge(req)
	}

	if e := t.Store.Get(key); e != nil && t.fresh(e, maxAge) {
		return t.serve(req, e, false), nil
	}

	// Only one process asks Quantic for this at a time. If another is asking
	// now, this waits for it, then finds its answer.
	unlock, err := t.Store.Lock(req.Context(), key)
	switch {
	case err == nil:
		defer unlock()
	case req.Context().Err() != nil:
		// Out of time waiting for the other process.
		if e := t.Store.Get(key); e != nil {
			return t.serve(req, e, true), nil
		}
		return nil, err
	default:
		// No lock to be had (a cache directory that can't be written): ask
		// Quantic anyway. A broken cache is no cache, never a failure.
	}

	e := t.Store.Get(key)
	if e != nil && t.fresh(e, maxAge) {
		return t.serve(req, e, false), nil
	}

	out := req
	if e != nil && e.ETag != "" {
		out = req.Clone(req.Context())
		out.Header.Set("If-None-Match", e.ETag)
	}
	resp, err := t.next().RoundTrip(out)
	if err != nil {
		if e != nil {
			return t.serve(req, e, true), nil
		}
		return nil, err
	}

	switch {
	case resp.StatusCode == http.StatusNotModified && e != nil:
		discard(resp)
		e.FetchedAt = t.now()
		t.Store.Put(key, e) // best effort: a cache that can't be written is still an answer
		return t.serve(req, e, false), nil

	case unreachable(resp.StatusCode) && e != nil:
		discard(resp)
		return t.serve(req, e, true), nil

	case resp.StatusCode == http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			if e != nil {
				return t.serve(req, e, true), nil
			}
			return nil, err
		}
		fetched := &Entry{
			URL:         req.URL.RequestURI(),
			FetchedAt:   t.now(),
			ETag:        resp.Header.Get("ETag"),
			ContentType: resp.Header.Get("Content-Type"),
			Body:        body,
		}
		// Best effort, as above. Only JSON can be stored: Entry holds the body
		// as a json.RawMessage, which json.Marshal refuses unless it's valid
		// JSON. So a captive portal's HTML page is answered, never kept.
		t.Store.Put(key, fetched)
		return t.serve(req, fetched, false), nil
	}

	t.record(Result{FetchedAt: t.now()})
	return resp, nil
}

func (t *Transport) fresh(e *Entry, maxAge time.Duration) bool {
	age := t.now().Sub(e.FetchedAt)
	return age >= 0 && age < maxAge
}

// serve answers req with e, as a 200 from Quantic would have.
func (t *Transport) serve(req *http.Request, e *Entry, stale bool) *http.Response {
	t.record(Result{FetchedAt: e.FetchedAt, Stale: stale})
	h := http.Header{}
	h.Set("Content-Type", e.ContentType)
	if e.ETag != "" {
		h.Set("ETag", e.ETag)
	}
	h.Set("Content-Length", strconv.Itoa(len(e.Body)))
	return &http.Response{
		Status:        "200 OK",
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        h,
		Body:          io.NopCloser(bytes.NewReader(e.Body)),
		ContentLength: int64(len(e.Body)),
		Request:       req,
	}
}

// unreachable is a status from something in front of Quantic saying it
// isn't there: a proxy or load balancer with no Quantic behind it.
func unreachable(status int) bool {
	return status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

// discard reads what's left of a body and closes it, so the connection can
// be used again.
func discard(resp *http.Response) {
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}
