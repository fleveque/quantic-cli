// Package cache keeps Quantic's answers on disk (design §7): a fresh answer
// is used without asking again, a stale one is revalidated with its ETag,
// and when Quantic can't be reached, the last answer is used, marked stale.
//
// Transport does all of that as an http.RoundTripper, under the client the
// api package builds, so nothing above it knows about files or ETags.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/fleveque/quantic-cli/internal/privfile"
)

// Entry is one answer, as kept on disk: the body of a 200, when it was
// fetched (or last confirmed unchanged), and the ETag to revalidate it with.
type Entry struct {
	URL         string          `json:"url"` // for someone reading the file; the key is a hash
	FetchedAt   time.Time       `json:"fetched_at"`
	ETag        string          `json:"etag,omitempty"`
	ContentType string          `json:"content_type"`
	Body        json.RawMessage `json:"body"`
}

// Store is a directory of entries, one file per key, and a lock file beside
// each. It holds portfolio data, so the directory is 0700 and every file in
// it 0600 (privfile).
type Store struct {
	Dir string
}

// Key names the entry for req: a hash of the method, the URL, and the
// Authorization header. Each token gets its own entries, so two accounts on
// one machine never see each other's, and the token itself is never
// written: a SHA-256 can't be turned back into it.
func Key(req *http.Request) string {
	h := sha256.New()
	h.Write([]byte(req.Method + " " + req.URL.String() + "\n" + req.Header.Get("Authorization")))
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func (s Store) path(key, ext string) string { return filepath.Join(s.Dir, key+ext) }

// Get returns the entry for key, or nil when there is none. An entry that
// can't be read as one (cut short, or from another version) is no entry:
// the cache is a copy, and Quantic has the original.
func (s Store) Get(key string) *Entry {
	b, err := os.ReadFile(s.path(key, ".json"))
	if err != nil {
		return nil
	}
	var e Entry
	if json.Unmarshal(b, &e) != nil || e.FetchedAt.IsZero() {
		return nil
	}
	return &e
}

// Put stores e under key, replacing what was there in one step.
func (s Store) Put(key string, e *Entry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return privfile.Write(s.path(key, ".json"), b)
}

// Clear removes every entry and lock. The directory is the CLI's own, so it
// goes as a whole.
func (s Store) Clear() error {
	if s.Dir == "" {
		return nil
	}
	err := os.RemoveAll(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// lockPoll is how often a waiting Lock tries again.
const lockPoll = 25 * time.Millisecond

// Lock takes key's lock, waiting for another process (or goroutine) that
// holds it, until ctx is done. The status bar and a command in a terminal
// may want the same answer at once; one asks Quantic, and the other, once
// it gets the lock, finds the answer already stored.
//
// The lock is an advisory lock on a file (flock on Unix), which the
// operating system releases when the process exits, even if it crashes, so
// a lock is never left behind. Taking it never blocks: a blocking flock
// can't be cancelled, and a wait must end at --timeout.
func (s Store) Lock(ctx context.Context, key string) (unlock func(), err error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(s.path(key, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		ok, err := tryLock(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		if ok {
			// Closing the file releases the lock with it.
			return func() { unlockFile(f); f.Close() }, nil
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(lockPoll):
		}
	}
}
