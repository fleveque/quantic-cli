package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/fleveque/quantic-cli/internal/auth"
)

// Client is what the commands call: one method per endpoint, each returning
// the decoded 200 response or one of the errors below.
type Client struct {
	gen  *ClientWithResponses
	base string
}

// Config is what New needs. Only BaseURL is required.
type Config struct {
	// BaseURL is where Quantic is: https://quantic.finance, or a local
	// Phoenix server in development. The API's paths are added to it.
	BaseURL string

	// UserAgent names the client in Quantic's logs, e.g. "quantic-cli/v0.1.0".
	UserAgent string

	// Token is sent as "Authorization: Bearer …" on every request; zero
	// means signed out. Go's http.Client drops the header if a redirect
	// leads to another host, so it only ever goes to BaseURL's.
	Token auth.Secret

	// Transport sends the requests; nil means http.DefaultTransport. Tests
	// replace it to count or break requests.
	Transport http.RoundTripper
}

// New checks cfg.BaseURL and builds a Client.
func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%q isn't an http or https URL", cfg.BaseURL)
	}

	next := cfg.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	// No Timeout on the http.Client: the context each method takes carries the
	// deadline, retries and their waits included.
	httpClient := &http.Client{
		Transport: &retryTransport{next: next, attempts: 3, base: time.Second},
	}

	gen, err := NewClientWithResponses(cfg.BaseURL,
		WithHTTPClient(httpClient),
		WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Accept", "application/json")
			if cfg.UserAgent != "" {
				req.Header.Set("User-Agent", cfg.UserAgent)
			}
			if !cfg.Token.IsZero() {
				req.Header.Set("Authorization", "Bearer "+cfg.Token.Reveal())
			}
			return nil
		}),
	)
	if err != nil {
		return nil, err
	}
	return &Client{gen: gen, base: u.Host}, nil
}

// Calendar is GET /api/v1/calendar: stocks going ex-dividend in the next days
// days. Public. Quantic clamps days to 1–120.
func (c *Client) Calendar(ctx context.Context, days int) (*Calendar, error) {
	resp, err := c.gen.GetCalendarWithResponse(ctx, &GetCalendarParams{Days: &days})
	if err != nil {
		return nil, c.requestError(err)
	}
	return result(resp.JSON200, resp.HTTPResponse, resp.Body)
}

// Stock is GET /api/v1/stocks/{symbol}. Public, richer with a token.
func (c *Client) Stock(ctx context.Context, symbol string) (*Stock, error) {
	resp, err := c.gen.GetStockWithResponse(ctx, symbol)
	if err != nil {
		return nil, c.requestError(err)
	}
	return result(resp.JSON200, resp.HTTPResponse, resp.Body)
}

// SearchStocks is GET /api/v1/stocks?q=: up to about 10 matches by ticker or
// name. Public.
func (c *Client) SearchStocks(ctx context.Context, query string) (*StockSearch, error) {
	resp, err := c.gen.SearchStocksWithResponse(ctx, &SearchStocksParams{Q: query})
	if err != nil {
		return nil, c.requestError(err)
	}
	return result(resp.JSON200, resp.HTTPResponse, resp.Body)
}

// Me is GET /api/v1/me: who the token belongs to. Needs a token.
func (c *Client) Me(ctx context.Context) (*Me, error) {
	resp, err := c.gen.GetMeWithResponse(ctx)
	if err != nil {
		return nil, c.requestError(err)
	}
	return result(resp.JSON200, resp.HTTPResponse, resp.Body)
}

// Portfolios is GET /api/v1/portfolios. Needs a token.
func (c *Client) Portfolios(ctx context.Context) (*PortfolioList, error) {
	resp, err := c.gen.ListPortfoliosWithResponse(ctx)
	if err != nil {
		return nil, c.requestError(err)
	}
	return result(resp.JSON200, resp.HTTPResponse, resp.Body)
}

// Holdings is GET /api/v1/holdings, in every portfolio or the one named.
// Needs a token.
func (c *Client) Holdings(ctx context.Context, params ListHoldingsParams) (*HoldingList, error) {
	resp, err := c.gen.ListHoldingsWithResponse(ctx, &params)
	if err != nil {
		return nil, c.requestError(err)
	}
	return result(resp.JSON200, resp.HTTPResponse, resp.Body)
}

// Dividends is GET /api/v1/dividends: dividends received, newest first.
// Needs a token.
func (c *Client) Dividends(ctx context.Context, params ListDividendsParams) (*DividendList, error) {
	resp, err := c.gen.ListDividendsWithResponse(ctx, &params)
	if err != nil {
		return nil, c.requestError(err)
	}
	return result(resp.JSON200, resp.HTTPResponse, resp.Body)
}

// Income is GET /api/v1/income: forward income and its projection. Needs a
// token.
func (c *Client) Income(ctx context.Context, params GetIncomeParams) (*Income, error) {
	resp, err := c.gen.GetIncomeWithResponse(ctx, &params)
	if err != nil {
		return nil, c.requestError(err)
	}
	return result(resp.JSON200, resp.HTTPResponse, resp.Body)
}

// StatusError is an answer from Quantic that isn't a 200: Status is the HTTP
// status, Code the API's stable error code ("not_found", "rate_limited"…)
// when the body had one, and Message what it said, for people.
type StatusError struct {
	Status  int
	Code    string
	Message string
}

func (e *StatusError) Error() string { return e.Message }

// UnreachableError means there was no answer at all: no network, a refused
// connection, a name that doesn't resolve, or no answer before the deadline.
// errors.Is(err, context.DeadlineExceeded) tells the last one apart.
type UnreachableError struct {
	Host string
	Err  error
}

func (e *UnreachableError) Error() string {
	return fmt.Sprintf("can't reach Quantic at %s: %v", e.Host, e.Err)
}

func (e *UnreachableError) Unwrap() error { return e.Err }

// requestError sorts an error from the generated client. A *url.Error comes
// from sending the request; anything else, from reading or decoding the
// answer, so Quantic was reached.
func (c *Client) requestError(err error) error {
	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		// urlErr.Err without the "Get <url>:" prefix urlErr adds: the host is
		// enough, and the full URL repeats what the command line said.
		return &UnreachableError{Host: c.base, Err: urlErr.Err}
	}
	return fmt.Errorf("reading Quantic's answer: %w", err)
}

// result returns the decoded 200 response, or the error the response was.
// The generated client decodes each status into its own field (JSON200,
// JSON404…), so a nil v means the answer wasn't a 200 with a JSON body.
func result[T any](v *T, resp *http.Response, body []byte) (*T, error) {
	if v != nil {
		return v, nil
	}
	var e Error
	if resp.StatusCode != http.StatusOK && json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		return nil, &StatusError{Status: resp.StatusCode, Code: string(e.Error.Code), Message: e.Error.Message}
	}
	return nil, &StatusError{Status: resp.StatusCode, Message: fmt.Sprintf("Quantic answered %s, not JSON", resp.Status)}
}
