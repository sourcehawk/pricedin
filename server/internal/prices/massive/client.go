// Package massive is the prices.Provider for Massive (formerly Polygon.io).
// It uses these endpoints: grouped daily and per-ticker aggregates (/v2),
// splits and dividends (/stocks/v1), and the ticker reference (/v3).
package massive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/prices"
)

// Recorder receives the body of every successful response, page by page,
// before the client decodes it. The key names the request, such as
// "prices/massive/grouped/2026-10-29/page-1.json"; the recorder adds what
// makes it unique. An error from Record fails the call.
type Recorder interface {
	Record(ctx context.Context, key string, body []byte) error
}

// Options configures a Client. Only APIKey is required.
type Options struct {
	// BaseURL is the http or https URL of the API. Default
	// "https://api.massive.com".
	BaseURL string
	// APIKey is sent as a bearer token. It never appears in an error or a
	// log line.
	APIKey string
	// RateLimit is the most requests a second. Default 20.
	RateLimit float64
	// MaxRetries is how many times a request is repeated after an HTTP 429,
	// an HTTP 5xx or a network error. Default 4; a negative value turns
	// retries off.
	MaxRetries int
	// RetryDelay is the wait before the first retry. Each later retry waits
	// about twice as long, up to one minute, or as long as a Retry-After
	// header asks, up to ten minutes. Default one second.
	RetryDelay time.Duration
	// HTTPClient sends the requests. Default: a client with a two-minute
	// timeout.
	HTTPClient *http.Client
	// Recorder, when not nil, receives every response body.
	Recorder Recorder
	// Logger receives a line for each retry. Default: no logging.
	Logger *slog.Logger
	// Now gives the current time. Default time.Now.
	Now func() time.Time
}

const (
	defaultBaseURL    = "https://api.massive.com"
	defaultRateLimit  = 20
	defaultMaxRetries = 4
	defaultRetryDelay = time.Second
	maxBackoff        = time.Minute
	maxRetryAfter     = 10 * time.Minute
	// The grouped daily response of about 12,600 tickers is about 2 MB.
	maxBodyBytes = 64 << 20
	maxPages     = 1000
)

// Client is the Massive prices.Provider. It is safe for concurrent use; the
// rate limit applies to all calls together.
type Client struct {
	base       string
	key        string
	http       *http.Client
	limiter    *rate.Limiter
	recorder   Recorder
	logger     *slog.Logger
	maxRetries int
	retryDelay time.Duration
	now        func() time.Time
	newYork    *time.Location
}

// New returns a Client. It returns an error when APIKey is empty or BaseURL
// is not an http or https URL with a host and no query.
func New(opts Options) (*Client, error) {
	if opts.APIKey == "" {
		return nil, errors.New("massive: no API key")
	}
	base := opts.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" {
		return nil, fmt.Errorf("massive: base URL %q: want an http or https URL with a host and no query", base)
	}
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		return nil, fmt.Errorf("massive: load time zone: %w", err)
	}
	c := &Client{
		base:       strings.TrimSuffix(u.String(), "/"),
		key:        opts.APIKey,
		http:       opts.HTTPClient,
		limiter:    rate.NewLimiter(rate.Limit(withDefault(opts.RateLimit, defaultRateLimit)), 1),
		recorder:   opts.Recorder,
		logger:     opts.Logger,
		maxRetries: opts.MaxRetries,
		retryDelay: withDefault(opts.RetryDelay, defaultRetryDelay),
		now:        opts.Now,
		newYork:    newYork,
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 2 * time.Minute}
	}
	if c.logger == nil {
		c.logger = slog.New(slog.DiscardHandler)
	}
	switch {
	case c.maxRetries == 0:
		c.maxRetries = defaultMaxRetries
	case c.maxRetries < 0:
		c.maxRetries = 0
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c, nil
}

func withDefault[T time.Duration | float64](v, fallback T) T {
	if v <= 0 {
		return fallback
	}
	return v
}

// Name returns "massive".
func (c *Client) Name() string { return source }

// GroupedDaily returns the raw bar of every US stock that traded on date,
// sorted by symbol.
func (c *Client) GroupedDaily(ctx context.Context, date domain.Date) ([]domain.PriceBar, error) {
	last := c.lastFinishedSession()
	if date.After(last) {
		return []domain.PriceBar{}, nil
	}
	rel := "/v2/aggs/grouped/locale/us/market/stocks/" + date.String() + "?adjusted=false"
	bars, err := fetchAll(ctx, c, rel, "prices/massive/grouped/"+date.String(), ParseGroupedDaily)
	if err != nil {
		return nil, err
	}
	bars = finishedOnly(bars, last)
	slices.SortStableFunc(bars, func(a, b domain.PriceBar) int { return strings.Compare(a.Symbol, b.Symbol) })
	return bars, nil
}

// DailyBars returns the raw bars of symbol from from to to, sorted by date.
// It returns an error when from is after to.
func (c *Client) DailyBars(ctx context.Context, symbol string, from, to domain.Date) ([]domain.PriceBar, error) {
	return c.dailyBars(ctx, symbol, from, to, false)
}

// AdjustedDailyBars returns the bars of symbol from from to to with
// Massive's own split adjustment. It exists to check AdjustForSplits
// against Massive; it is not part of prices.Provider, and PricedIn stores
// raw bars only.
func (c *Client) AdjustedDailyBars(ctx context.Context, symbol string, from, to domain.Date) ([]domain.PriceBar, error) {
	return c.dailyBars(ctx, symbol, from, to, true)
}

func (c *Client) dailyBars(ctx context.Context, symbol string, from, to domain.Date, adjusted bool) ([]domain.PriceBar, error) {
	if symbol == "" {
		return nil, errors.New("massive: daily bars: empty symbol")
	}
	if from.After(to) {
		return nil, fmt.Errorf("massive: daily bars %s: from %s is after to %s", symbol, from, to)
	}
	last := c.lastFinishedSession()
	if to.After(last) {
		to = last
	}
	if from.After(to) {
		return []domain.PriceBar{}, nil
	}
	q := url.Values{"adjusted": {strconv.FormatBool(adjusted)}, "sort": {"asc"}, "limit": {"50000"}}
	rel := fmt.Sprintf("/v2/aggs/ticker/%s/range/1/day/%s/%s?%s", url.PathEscape(symbol), from, to, q.Encode())
	kind := "aggs"
	if adjusted {
		kind = "aggs-adjusted"
	}
	key := fmt.Sprintf("prices/massive/%s/%s/%s_%s", kind, url.PathEscape(symbol), from, to)
	bars, err := fetchAll(ctx, c, rel, key, ParseDailyBars)
	if err != nil {
		return nil, err
	}
	bars = finishedOnly(bars, last)
	slices.SortStableFunc(bars, func(a, b domain.PriceBar) int { return a.Date.Time().Compare(b.Date.Time()) })
	return bars, nil
}

// lastFinishedSession returns the latest trading day whose session is over
// at the time of c.now.
func (c *Client) lastFinishedSession() domain.Date {
	now := c.now().In(c.newYork)
	d := domain.DateOf(now)
	if now.Hour() < prices.SessionOverHour {
		d = d.AddDays(-1)
	}
	for !prices.IsTradingDay(d) {
		d = d.AddDays(-1)
	}
	return d
}

// finishedOnly leaves out the bars dated after last. Massive marks the bars
// of a session that is not over DELAYED, and they can be incomplete.
func finishedOnly(bars []domain.PriceBar, last domain.Date) []domain.PriceBar {
	return slices.DeleteFunc(bars, func(b domain.PriceBar) bool { return b.Date.After(last) })
}

// Splits returns every split event of symbol, sorted by date. Massive's
// stock dividends paid in shares count as splits.
func (c *Client) Splits(ctx context.Context, symbol string) ([]domain.Split, error) {
	if symbol == "" {
		return nil, errors.New("massive: splits: empty symbol")
	}
	q := url.Values{"ticker": {symbol}, "limit": {"1000"}, "sort": {"execution_date.asc"}}
	splits, err := fetchAll(ctx, c, "/stocks/v1/splits?"+q.Encode(), "prices/massive/splits/"+url.PathEscape(symbol), ParseSplits)
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(splits, func(a, b domain.Split) int { return a.Date.Time().Compare(b.Date.Time()) })
	return splits, nil
}

// Dividends returns every cash dividend of symbol, sorted by ex-dividend
// date, including those announced for a later date.
func (c *Client) Dividends(ctx context.Context, symbol string) ([]domain.Dividend, error) {
	if symbol == "" {
		return nil, errors.New("massive: dividends: empty symbol")
	}
	q := url.Values{"ticker": {symbol}, "limit": {"1000"}, "sort": {"ex_dividend_date.asc"}}
	divs, err := fetchAll(ctx, c, "/stocks/v1/dividends?"+q.Encode(), "prices/massive/dividends/"+url.PathEscape(symbol), ParseDividends)
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(divs, func(a, b domain.Dividend) int { return a.ExDate.Time().Compare(b.ExDate.Time()) })
	return divs, nil
}

// Tickers returns the active tickers of the US stock market, sorted by
// symbol. The list includes funds and other types of security besides
// common stock; see ProviderTicker.Type.
func (c *Client) Tickers(ctx context.Context) ([]prices.ProviderTicker, error) {
	q := url.Values{"market": {"stocks"}, "active": {"true"}, "limit": {"1000"}, "sort": {"ticker"}, "order": {"asc"}}
	tickers, err := fetchAll(ctx, c, "/v3/reference/tickers?"+q.Encode(), "prices/massive/tickers/stocks-active", ParseTickers)
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(tickers, func(a, b prices.ProviderTicker) int { return strings.Compare(a.Symbol, b.Symbol) })
	return tickers, nil
}

// Ticker returns the details of one ticker. For a symbol that Massive does
// not know, it returns an *APIError with Status "NOT_FOUND".
func (c *Client) Ticker(ctx context.Context, symbol string) (prices.ProviderTicker, error) {
	if symbol == "" {
		return prices.ProviderTicker{}, errors.New("massive: ticker: empty symbol")
	}
	rel := "/v3/reference/tickers/" + url.PathEscape(symbol)
	body, err := c.get(ctx, rel, "prices/massive/ticker/"+url.PathEscape(symbol)+"/page-1.json")
	if err != nil {
		return prices.ProviderTicker{}, err
	}
	t, err := ParseTickerOverview(body)
	if err != nil {
		return prices.ProviderTicker{}, c.withPath(err, rel)
	}
	return t, nil
}

// fetchAll requests rel and every page that its next_url links lead to, and
// returns the items of all pages in order. keyBase is the recorder key
// without the page part.
func fetchAll[T any](ctx context.Context, c *Client, rel, keyBase string, parse func([]byte) (Page[T], error)) ([]T, error) {
	all := []T{}
	seen := map[string]bool{}
	for page := 1; ; page++ {
		if seen[rel] {
			return nil, fmt.Errorf("massive: GET %s: next_url repeats an earlier page", rel)
		}
		seen[rel] = true
		body, err := c.get(ctx, rel, fmt.Sprintf("%s/page-%d.json", keyBase, page))
		if err != nil {
			return nil, err
		}
		p, err := parse(body)
		if err != nil {
			return nil, c.withPath(err, rel)
		}
		all = append(all, p.Items...)
		if p.NextURL == "" {
			return all, nil
		}
		if page == maxPages {
			return nil, fmt.Errorf("massive: GET %s: more than %d pages", rel, maxPages)
		}
		if rel, err = nextRel(p.NextURL); err != nil {
			return nil, err
		}
	}
}

// withPath names the request in an error from a parse function, and removes
// the key from the message and the request ID that came from the response.
func (c *Client) withPath(err error, rel string) error {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Path == "" {
		apiErr.Path = rel
		apiErr.HTTPStatus = http.StatusOK
		apiErr.Status = c.scrub(apiErr.Status)
		apiErr.Message = c.scrub(apiErr.Message)
		apiErr.RequestID = c.scrub(apiErr.RequestID)
		return apiErr
	}
	return fmt.Errorf("massive: GET %s: %w", rel, c.scrubErr(err))
}

// nextRel turns a next_url into a path and query for the client's own base
// URL. Following it on the base URL keeps the bearer token on the
// configured host, and lets a fixture server serve the next pages. An
// apiKey query parameter is dropped, because the client authenticates with
// its header.
func nextRel(next string) (string, error) {
	u, err := url.Parse(next)
	if err != nil {
		// The parse error repeats the URL, which can carry a key.
		return "", errors.New("massive: next_url is not a valid URL")
	}
	q := u.Query()
	for k := range q {
		if strings.EqualFold(k, "apikey") {
			q.Del(k)
			u.RawQuery = q.Encode()
		}
	}
	if u.RawQuery == "" {
		return u.EscapedPath(), nil
	}
	return u.EscapedPath() + "?" + u.RawQuery, nil
}

// get returns the body of a successful response to rel, after recording it
// under key. It retries as Options describes.
func (c *Client) get(ctx context.Context, rel, key string) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		body, retryAfter, err := c.try(ctx, rel)
		if err == nil {
			if c.recorder != nil {
				if err := c.recorder.Record(ctx, key, body); err != nil {
					return nil, fmt.Errorf("massive: record %s: %w", key, err)
				}
			}
			return body, nil
		}
		if retryAfter < 0 || attempt >= c.maxRetries {
			return nil, err
		}
		wait := c.backoff(attempt, retryAfter)
		c.logger.WarnContext(ctx, "massive: retrying request", "path", rel, "attempt", attempt+1, "wait", wait, "error", err.Error())
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("massive: GET %s: %w", rel, ctx.Err())
		case <-timer.C:
		}
	}
}

// try sends one request. retryAfter is negative when the error is final, 0
// when it can be retried after the usual backoff, and positive when the
// server asked for that wait.
func (c *Client) try(ctx context.Context, rel string) (body []byte, retryAfter time.Duration, err error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, -1, fmt.Errorf("massive: GET %s: %w", rel, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+rel, http.NoBody)
	if err != nil {
		return nil, -1, fmt.Errorf("massive: GET %s: %w", rel, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, -1, fmt.Errorf("massive: GET %s: %w", rel, ctx.Err())
		}
		return nil, 0, fmt.Errorf("massive: GET %s: %w", rel, c.scrubErr(err))
	}
	defer func() { _ = resp.Body.Close() }()

	body, err = io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, 0, fmt.Errorf("massive: GET %s: read body: %w", rel, c.scrubErr(err))
	}
	if len(body) > maxBodyBytes {
		return nil, -1, fmt.Errorf("massive: GET %s: body larger than %d bytes", rel, maxBodyBytes)
	}
	if resp.StatusCode == http.StatusOK {
		return body, 0, nil
	}

	apiErr := &APIError{HTTPStatus: resp.StatusCode, Path: rel}
	var env envelope
	if decodeEnvelope(body, &env) == nil {
		apiErr.Status, apiErr.Message, apiErr.RequestID = c.scrub(env.Status), c.scrub(env.message()), c.scrub(env.RequestID)
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return nil, parseRetryAfter(resp.Header.Get("Retry-After"), c.now()), apiErr
	}
	return nil, -1, apiErr
}

// backoff returns the wait before retry number attempt+1: the server's
// Retry-After when it is longer, or a doubling delay with jitter.
func (c *Client) backoff(attempt int, retryAfter time.Duration) time.Duration {
	d := c.retryDelay << min(attempt, 30)
	if d <= 0 || d > maxBackoff {
		d = maxBackoff
	}
	// Jitter spreads the retries of concurrent calls.
	d = d/2 + rand.N(d/2+1) //nolint:gosec // jitter needs no cryptographic randomness
	return max(d, min(retryAfter, maxRetryAfter))
}

// parseRetryAfter reads a Retry-After header in seconds or as an HTTP date.
// It returns 0 when the header is absent or invalid.
func parseRetryAfter(header string, now time.Time) time.Duration {
	if header == "" {
		return 0
	}
	if secs, err := strconv.Atoi(header); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(header); err == nil && t.After(now) {
		return t.Sub(now)
	}
	return 0
}

// scrub removes the API key from text that came from outside the client,
// such as a server's error message.
func (c *Client) scrub(s string) string {
	return strings.ReplaceAll(s, c.key, "REDACTED")
}

func (c *Client) scrubErr(err error) error {
	if msg := err.Error(); strings.Contains(msg, c.key) {
		return errors.New(c.scrub(msg))
	}
	return err
}
