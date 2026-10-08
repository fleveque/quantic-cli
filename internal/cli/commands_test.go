package cli_test

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fleveque/quantic-cli/internal/cli"
)

var update = flag.Bool("update", false, "rewrite testdata/golden from the current output")

// fakeQuantic serves testdata/api: GET /api/v1/calendar is calendar.json,
// /api/v1/stocks/KO is stock-KO.json, /api/v1/stocks?q=coca is
// search-coca.json. Anything else is Quantic's 404. QUANTIC_URL points at it
// for the rest of the test.
func fakeQuantic(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var name string
		switch {
		case r.URL.Path == "/api/v1/calendar":
			name = "calendar"
		case r.URL.Path == "/api/v1/stocks":
			name = "search-" + r.URL.Query().Get("q")
		case strings.HasPrefix(r.URL.Path, "/api/v1/stocks/"):
			name = "stock-" + strings.TrimPrefix(r.URL.Path, "/api/v1/stocks/")
		}
		w.Header().Set("Content-Type", "application/json")
		body, err := os.ReadFile(filepath.Join("testdata", "api", name+".json"))
		if name == "" || err != nil {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"code":"not_found","message":"Not found. Try /api/v1/stocks?q=."}}`))
			return
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("QUANTIC_URL", srv.URL)
}

// fetchedAt is the one part of the output that changes on every run.
var fetchedAt = regexp.MustCompile(`"fetched_at": "[^"]+"`)

// Every shape and every table, pinned by a golden file: a change to the
// output contract (design §5) shows up as a diff in testdata/golden, reviewed
// like code. `go test ./internal/cli -update` rewrites them.
func TestGolden(t *testing.T) {
	fakeQuantic(t)
	tests := []struct {
		golden string
		args   []string
	}{
		{"calendar.txt", []string{"calendar", "--days", "7"}},
		{"calendar.json", []string{"calendar", "--days", "7", "--json"}},
		{"stock-KO.txt", []string{"stock", "ko"}},
		{"stock-KO.json", []string{"stock", "KO", "--json"}},
		{"stock-SPARSE.txt", []string{"stock", "SPARSE"}},
		{"stock-SPARSE.json", []string{"stock", "SPARSE", "--json"}},
		{"search.txt", []string{"search", "coca"}},
		{"search.json", []string{"search", "coca", "--json"}},
		{"search-nothing.txt", []string{"search", "nothing", "at", "all"}},
	}
	for _, tt := range tests {
		t.Run(tt.golden, func(t *testing.T) {
			code, stdout, stderr := run(t, tt.args...)
			if code != cli.ExitOK || stderr != "" {
				t.Fatalf("exit %d, stderr %q", code, stderr)
			}
			got := fetchedAt.ReplaceAllString(stdout, `"fetched_at": "FETCHED_AT"`)

			path := filepath.Join("testdata", "golden", tt.golden)
			if *update {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if got != string(want) {
				t.Errorf("output differs from %s (-update rewrites it)\ngot:\n%s\nwant:\n%s", path, got, want)
			}
		})
	}
}

func TestFetchedAtIsNowInUTC(t *testing.T) {
	fakeQuantic(t)
	_, stdout, _ := run(t, "calendar", "--json")
	m := regexp.MustCompile(`"fetched_at": "([^"]+)"`).FindStringSubmatch(stdout)
	if m == nil {
		t.Fatalf("no fetched_at in %s", stdout)
	}
	at, err := time.Parse(time.RFC3339, m[1])
	if err != nil || !strings.HasSuffix(m[1], "Z") {
		t.Fatalf("fetched_at = %q, want an RFC 3339 time in UTC", m[1])
	}
	if d := time.Since(at); d < 0 || d > time.Minute {
		t.Errorf("fetched_at = %s, %s from now", at, d)
	}
}

// Each failure exits with its own code (design §6), says why on stderr, and
// leaves stdout empty.
func TestAPIFailures(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc // nil: nothing listening
		args    []string
		code    int
		message string
	}{
		{"unknown symbol", nil, []string{"stock", "nopexx"}, cli.ExitFailed,
			"quantic: Quantic doesn't track NOPEXX; try: quantic search NOPEXX\n"},
		{"rate limited", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"code":"rate_limited","message":"Rate limited: try again shortly, or send a token."}}`))
		}, []string{"calendar"}, cli.ExitRateLimited,
			"quantic: Quantic is rate limiting this address (60 calls a minute without signing in); try again in a minute\n"},
		{"server error", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}, []string{"calendar"}, cli.ExitFailed,
			"quantic: Quantic answered 502 Bad Gateway, not JSON\n"},
		{"no answer in time", func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}, []string{"calendar", "--timeout", "50ms"}, cli.ExitUnreachable,
			"quantic: Quantic didn't answer within 50ms (--timeout)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.handler == nil {
				fakeQuantic(t)
			} else {
				srv := httptest.NewServer(tt.handler)
				t.Cleanup(srv.Close)
				t.Setenv("QUANTIC_URL", srv.URL)
			}
			code, stdout, stderr := run(t, tt.args...)
			if code != tt.code {
				t.Errorf("exit code = %d, want %d", code, tt.code)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing", stdout)
			}
			if stderr != tt.message {
				t.Errorf("stderr = %q\nwant     %q", stderr, tt.message)
			}
		})
	}
}

func TestUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	t.Setenv("QUANTIC_URL", srv.URL)

	code, stdout, stderr := run(t, "calendar")
	if code != cli.ExitUnreachable || stdout != "" {
		t.Errorf("exit %d, stdout %q; want %d and nothing", code, stdout, cli.ExitUnreachable)
	}
	if !strings.HasPrefix(stderr, "quantic: can't reach Quantic at 127.0.0.1:") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestCommandUsage(t *testing.T) {
	fakeQuantic(t)
	tests := []struct {
		args    []string
		message string
	}{
		{[]string{"calendar", "--days", "0"}, "--days must be at least 1, got 0"},
		{[]string{"calendar", "--days", "soon"}, `invalid argument "soon" for "--days"`},
		{[]string{"calendar", "KO"}, `unknown command "KO" for "quantic calendar"`},
		{[]string{"stock"}, "accepts 1 arg(s), received 0"},
		{[]string{"stock", "KO", "PEP"}, "accepts 1 arg(s), received 2"},
		{[]string{"stock", " "}, "the symbol is empty"},
		{[]string{"search"}, "requires at least 1 arg(s), only received 0"},
		{[]string{"search", "  "}, "the query is empty"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			code, stdout, stderr := run(t, tt.args...)
			if code != cli.ExitUsage || stdout != "" || !strings.Contains(stderr, tt.message) {
				t.Errorf("exit %d, stdout %q, stderr %q\nwant exit %d, no stdout, stderr with %q",
					code, stdout, stderr, cli.ExitUsage, tt.message)
			}
		})
	}
}

func TestBadQuanticURL(t *testing.T) {
	t.Setenv("QUANTIC_URL", "quantic.finance")
	code, _, stderr := run(t, "calendar")
	if code != cli.ExitUsage || !strings.Contains(stderr, `QUANTIC_URL: "quantic.finance" isn't an http or https URL`) {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}
