package api

import (
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

// retryTransport retries a request Quantic answered with 429 Too Many
// Requests (design §7): anonymous calls share a limit of 60 a minute per IP
// address. It sits between the generated client and the network, so the
// generated code never knows a retry happened.
//
// It only retries GET, the only method the API has: a GET has no body to
// replay, and asking twice changes nothing on the server.
type retryTransport struct {
	next     http.RoundTripper
	attempts int           // in all, the first one included
	base     time.Duration // the first wait; each one after doubles
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for attempt := 1; ; attempt++ {
		resp, err := t.next.RoundTrip(req)
		if err != nil || resp.StatusCode != http.StatusTooManyRequests ||
			req.Method != http.MethodGet || attempt == t.attempts {
			return resp, err
		}

		wait := retryAfter(resp.Header, t.base<<(attempt-1))
		// No point waiting past the caller's deadline: answer with the 429
		// now, so the caller hears "rate limited", not "timed out".
		if deadline, ok := req.Context().Deadline(); ok && time.Until(deadline) < wait {
			return resp, nil
		}
		resp.Body.Close() // the next attempt replaces it

		timer := time.NewTimer(wait)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}
	}
}

// retryAfter is how long to wait before the next attempt: the server's
// Retry-After when it sends one in seconds, otherwise backoff with jitter,
// between half and one and a half times backoff. The jitter keeps several
// clients that were limited together from all retrying at the same moment.
func retryAfter(h http.Header, backoff time.Duration) time.Duration {
	if s, err := strconv.Atoi(h.Get("Retry-After")); err == nil && s >= 0 {
		return time.Duration(s) * time.Second
	}
	return backoff/2 + rand.N(backoff)
}
