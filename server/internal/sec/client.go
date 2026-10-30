package sec

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"mime"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// MaxRequestsPerSecond is the SEC limit for one client. A Client never
// exceeds it.
const MaxRequestsPerSecond = 10

// The defaults of Options.
const (
	DefaultBaseURL     = "https://www.sec.gov"
	DefaultDataBaseURL = "https://data.sec.gov"
	// DefaultRequestsPerSecond stays below MaxRequestsPerSecond, because
	// network jitter can move requests that leave a second apart into one
	// second at SEC.
	DefaultRequestsPerSecond     = 9
	DefaultMaxAttempts           = 5
	DefaultMaxRetryAfter         = 5 * time.Minute
	DefaultResponseHeaderTimeout = 30 * time.Second
)

const (
	firstBackoff = time.Second
	maxBackoff   = 30 * time.Second
)

// ErrNotFound means that SEC has no such file, such as company facts for a
// company that files no XBRL, or a daily index for a day without one.
var ErrNotFound = errors.New("sec: not found")

// ErrForbidden means that SEC refused the request with status 403.
var ErrForbidden = errors.New("sec: forbidden (403): the likely cause is a User-Agent without a name and a contact email, or more than the SEC rate limit of 10 requests a second")

// StatusError is an HTTP status that the Client does not expect. For 429 and
// 5xx, it is the status of the last attempt.
type StatusError struct {
	StatusCode int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("status %d %s", e.StatusCode, http.StatusText(e.StatusCode))
}

// Recorder stores raw responses. The Client calls Record with the decoded
// body of every response before it parses the body. key is a base key such as
// "sec/companyfacts/CIK0001045810.json"; the recorder makes it unique. An
// error from Record fails the call.
type Recorder interface {
	Record(ctx context.Context, key string, body []byte) error
}

// Options configure a Client. Only UserAgent is required.
type Options struct {
	// UserAgent names the sender and gives a contact email, as SEC requires,
	// such as "PricedIn owner@example.com".
	UserAgent string
	// BaseURL replaces https://www.sec.gov, and DataBaseURL replaces
	// https://data.sec.gov, for tests and the fixture server.
	BaseURL     string
	DataBaseURL string
	// RequestsPerSecond is the rate limit: more than 0 and at most
	// MaxRequestsPerSecond. Default DefaultRequestsPerSecond. Redirects
	// count as requests.
	RequestsPerSecond float64
	// MaxAttempts is the number of tries of one request, including the
	// first. Default DefaultMaxAttempts.
	MaxAttempts int
	// MaxRetryAfter is the longest Retry-After that the Client waits for.
	// A longer one fails the call. Default DefaultMaxRetryAfter.
	MaxRetryAfter time.Duration
	// HTTPClient sends the requests. It must not have a total timeout that
	// is shorter than a bulk archive download. The Client uses a copy, so
	// the rate limit does not apply to other users of HTTPClient.
	HTTPClient *http.Client
	// ResponseHeaderTimeout limits the wait for the headers of a response
	// when HTTPClient is nil. Default DefaultResponseHeaderTimeout.
	ResponseHeaderTimeout time.Duration
	// Recorder receives every response body except the bulk archive. Nil
	// records nothing.
	Recorder Recorder
	// Clock defaults to the system clock.
	Clock  Clock
	Logger *slog.Logger
}

// Client fetches SEC files. It sends at most RequestsPerSecond requests in
// any one second across all its goroutines, and retries 429, 5xx and
// transport errors with exponential backoff. It is safe for concurrent use.
type Client struct {
	userAgent     string
	baseURL       string
	dataBaseURL   string
	maxAttempts   int
	maxRetryAfter time.Duration
	http          *http.Client
	recorder      Recorder
	clock         Clock
	logger        *slog.Logger
}

// New returns a Client. It returns an error when UserAgent is empty or an
// option is out of range.
func New(opts Options) (*Client, error) {
	if strings.TrimSpace(opts.UserAgent) == "" {
		return nil, errors.New("sec: a User-Agent is required")
	}
	rate := opts.RequestsPerSecond
	if rate == 0 {
		rate = DefaultRequestsPerSecond
	}
	if rate < 0 || rate > MaxRequestsPerSecond {
		return nil, fmt.Errorf("sec: requests per second %v, want more than 0 and at most %d", opts.RequestsPerSecond, MaxRequestsPerSecond)
	}
	if opts.MaxAttempts < 0 || opts.MaxRetryAfter < 0 || opts.ResponseHeaderTimeout < 0 {
		return nil, errors.New("sec: max attempts, max Retry-After and response header timeout must not be negative")
	}
	baseURL, err := baseURLOrDefault(opts.BaseURL, DefaultBaseURL)
	if err != nil {
		return nil, err
	}
	dataBaseURL, err := baseURLOrDefault(opts.DataBaseURL, DefaultDataBaseURL)
	if err != nil {
		return nil, err
	}
	c := &Client{
		userAgent:     opts.UserAgent,
		baseURL:       baseURL,
		dataBaseURL:   dataBaseURL,
		maxAttempts:   opts.MaxAttempts,
		maxRetryAfter: opts.MaxRetryAfter,
		recorder:      opts.Recorder,
		clock:         opts.Clock,
		logger:        opts.Logger,
	}
	if c.maxAttempts == 0 {
		c.maxAttempts = DefaultMaxAttempts
	}
	if c.maxRetryAfter == 0 {
		c.maxRetryAfter = DefaultMaxRetryAfter
	}
	if c.clock == nil {
		c.clock = systemClock{}
	}
	if c.logger == nil {
		c.logger = slog.New(slog.DiscardHandler)
	}
	// Rounding up keeps the rate at or below the limit.
	interval := time.Duration(math.Ceil(float64(time.Second) / rate))
	c.http = limitedHTTPClient(opts, &limiter{clock: c.clock, interval: interval})
	return c, nil
}

// limitedHTTPClient returns a copy of opts.HTTPClient, or a new client, whose
// every request waits for lim. This includes the requests that follow a
// redirect, which http.Client sends without returning to the caller.
func limitedHTTPClient(opts Options, lim *limiter) *http.Client {
	var hc http.Client
	if opts.HTTPClient != nil {
		hc = *opts.HTTPClient
	} else {
		hc.Transport = defaultTransport(opts.ResponseHeaderTimeout)
	}
	next := hc.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	hc.Transport = limitedTransport{next: next, limiter: lim}
	return &hc
}

// defaultTransport has no limit on the whole response, because a bulk
// archive takes minutes to download. It limits each step before the body.
func defaultTransport(headerTimeout time.Duration) *http.Transport {
	if headerTimeout == 0 {
		headerTimeout = DefaultResponseHeaderTimeout
	}
	var t *http.Transport
	if dt, ok := http.DefaultTransport.(*http.Transport); ok {
		t = dt.Clone()
	} else {
		t = &http.Transport{Proxy: http.ProxyFromEnvironment, ForceAttemptHTTP2: true}
	}
	t.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	t.TLSHandshakeTimeout = 30 * time.Second
	t.ResponseHeaderTimeout = headerTimeout
	return t
}

type limitedTransport struct {
	next    http.RoundTripper
	limiter *limiter
}

func (t limitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	if err := t.limiter.wait(ctx); err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}
	// http.Transport sends a GET again on a new connection when a reused
	// one fails before the response, and it does not return here first.
	// Each connection after the first therefore waits for the limiter too.
	// A wait that ctx ends needs no handling: the transport then stops the
	// request itself.
	var conns atomic.Int32
	trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) {
		if conns.Add(1) > 1 {
			_ = t.limiter.wait(ctx)
		}
	}}
	return t.next.RoundTrip(req.WithContext(httptrace.WithClientTrace(ctx, trace)))
}

func baseURLOrDefault(raw, fallback string) (string, error) {
	if raw == "" {
		return fallback, nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("sec: base URL %q: want an absolute http or https URL", raw)
	}
	return strings.TrimRight(raw, "/"), nil
}

// Tickers fetches the ticker map. Each ticker's ValidFrom is the day of the
// download, in UTC.
func (c *Client) Tickers(ctx context.Context) ([]TickerEntry, error) {
	body, err := c.get(ctx, c.baseURL+"/files/company_tickers_exchange.json", "sec/tickers/company_tickers_exchange.json")
	if err != nil {
		return nil, err
	}
	return ParseTickers(body, domain.DateOf(c.clock.Now().UTC()))
}

// Submissions fetches the submissions of the company cik, with the filings
// of every extra file. It returns ErrNotFound when SEC knows no such company.
func (c *Client) Submissions(ctx context.Context, cik domain.CIK) (Submissions, error) {
	if cik <= 0 {
		return Submissions{}, fmt.Errorf("sec: invalid cik %d", int64(cik))
	}
	name := "CIK" + cik.String() + ".json"
	body, err := c.get(ctx, c.dataBaseURL+"/submissions/"+name, "sec/submissions/"+name)
	if err != nil {
		return Submissions{}, err
	}
	sub, err := ParseSubmissions(body)
	if err != nil {
		return Submissions{}, err
	}
	for _, extra := range sub.Files {
		if strings.ContainsAny(extra, `/\`) || !strings.HasSuffix(extra, ".json") {
			return Submissions{}, fmt.Errorf("sec: extra submissions file %q: want a file name", extra)
		}
		body, err := c.get(ctx, c.dataBaseURL+"/submissions/"+extra, "sec/submissions/"+extra)
		if err != nil {
			return Submissions{}, err
		}
		filings, err := ParseSubmissionsFile(cik, body)
		if err != nil {
			return Submissions{}, fmt.Errorf("%s: %w", extra, err)
		}
		sub.Filings = append(sub.Filings, filings...)
	}
	return sub, nil
}

// CompanyFacts fetches the facts of the company cik (see ParseCompanyFacts).
// It returns ErrNotFound when SEC has no facts for the company.
func (c *Client) CompanyFacts(ctx context.Context, cik domain.CIK) ([]domain.Fact, error) {
	if cik <= 0 {
		return nil, fmt.Errorf("sec: invalid cik %d", int64(cik))
	}
	name := "CIK" + cik.String() + ".json"
	body, err := c.get(ctx, c.dataBaseURL+"/api/xbrl/companyfacts/"+name, "sec/companyfacts/"+name)
	if err != nil {
		return nil, err
	}
	return ParseCompanyFacts(body)
}

// DailyIndex fetches the daily master index of day: every filing that SEC
// disseminated that day. It returns ErrNotFound for a day without an index,
// such as a weekend, a holiday, or a day that SEC has not finished.
func (c *Client) DailyIndex(ctx context.Context, day domain.Date) ([]domain.Filing, error) {
	name := "master." + day.Time().Format("20060102") + ".idx"
	quarter := (int(day.Month)-1)/3 + 1
	u := fmt.Sprintf("%s/Archives/edgar/daily-index/%d/QTR%d/%s", c.baseURL, day.Year, quarter, name)
	body, err := c.get(ctx, u, "sec/daily-index/"+name)
	if err != nil {
		return nil, err
	}
	return ParseDailyIndex(body)
}

// DownloadCompanyFactsArchive streams the bulk company facts archive
// (companyfacts.zip, more than 1 GB) to w and returns the number of bytes
// written. The Client does not try again after a byte has reached w. On an
// error, w can hold part of the archive, which the caller must discard.
func (c *Client) DownloadCompanyFactsArchive(ctx context.Context, w io.Writer) (int64, error) {
	return c.download(ctx, c.baseURL+"/Archives/edgar/daily-index/xbrl/companyfacts.zip", w)
}

// get returns the decoded body of u after the recorder stored it under key.
func (c *Client) get(ctx context.Context, u, key string) ([]byte, error) {
	var body []byte
	err := c.fetch(ctx, u, func(r io.Reader) error {
		var err error
		if body, err = io.ReadAll(r); err != nil {
			return &bodyError{err: err}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if c.recorder != nil {
		if err := c.recorder.Record(ctx, key, body); err != nil {
			return nil, fmt.Errorf("sec: record %s: %w", key, err)
		}
	}
	return body, nil
}

func (c *Client) download(ctx context.Context, u string, w io.Writer) (int64, error) {
	var n int64
	err := c.fetch(ctx, u, func(r io.Reader) error {
		cw := &countingWriter{w: w}
		_, err := io.Copy(cw, r)
		n += cw.n
		if err != nil && cw.n == 0 && cw.err == nil {
			return &bodyError{err: err}
		}
		return err
	})
	return n, err
}

// countingWriter counts the bytes that reach w and keeps the first error of
// w.
type countingWriter struct {
	w   io.Writer
	n   int64
	err error
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.n += int64(n)
	if err != nil && cw.err == nil {
		cw.err = err
	}
	return n, err
}

// bodyError is a failure to read a response body that left nothing in the
// output, so the request can be tried again.
type bodyError struct{ err error }

func (e *bodyError) Error() string { return e.err.Error() }
func (e *bodyError) Unwrap() error { return e.err }

// fetch sends GET u until it gets a 200 response, and passes the decoded
// body to consume. A *bodyError from consume counts as a transport error, and
// the request is tried again.
func (c *Client) fetch(ctx context.Context, u string, consume func(io.Reader) error) error {
	for attempt := 1; ; attempt++ {
		retry, wait, err := c.try(ctx, u, consume)
		if err == nil {
			return nil
		}
		if !retry {
			return fmt.Errorf("sec: get %s: %w", u, err)
		}
		if attempt >= c.maxAttempts {
			return fmt.Errorf("sec: get %s: gave up after %d attempts: %w", u, attempt, err)
		}
		if wait == 0 {
			wait = backoff(attempt)
		}
		c.logger.WarnContext(ctx, "sec: retrying", "url", u, "attempt", attempt, "wait", wait, "error", err)
		if err := c.clock.SleepUntil(ctx, c.clock.Now().Add(wait)); err != nil {
			return fmt.Errorf("sec: get %s: %w", u, err)
		}
	}
}

// try sends one request. It reports whether a failure is worth another try
// and how long the server asked to wait first (0 when it did not ask).
func (c *Client) try(ctx context.Context, u string, consume func(io.Reader) error) (retry bool, wait time.Duration, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return false, 0, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	// With this header set by hand, net/http leaves the body compressed.
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := c.http.Do(req)
	if err != nil {
		return ctx.Err() == nil, 0, err
	}
	defer resp.Body.Close()

	switch code := resp.StatusCode; {
	case code == http.StatusOK:
		body, closeBody, err := decodedBody(resp)
		if err != nil {
			return ctx.Err() == nil, 0, err
		}
		defer closeBody()
		if err := consume(body); err != nil {
			var bodyErr *bodyError
			return errors.As(err, &bodyErr) && ctx.Err() == nil, 0, err
		}
		return false, 0, nil
	case code == http.StatusTooManyRequests || code >= 500:
		statusErr := &StatusError{StatusCode: code}
		wait := c.retryAfter(resp.Header.Get("Retry-After"))
		if wait > c.maxRetryAfter {
			return false, 0, fmt.Errorf("%w: the server asks to wait %v (Retry-After), more than the limit of %v", statusErr, wait, c.maxRetryAfter)
		}
		return true, wait, statusErr
	case code == http.StatusNotFound:
		return false, 0, ErrNotFound
	case code == http.StatusForbidden:
		if isMissingFile(resp) {
			return false, 0, ErrNotFound
		}
		return false, 0, ErrForbidden
	default:
		return false, 0, &StatusError{StatusCode: code}
	}
}

// retryAfter reads a Retry-After value in seconds or as an HTTP date. It
// returns 0 for an empty or invalid value.
func (c *Client) retryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if secs, err := strconv.Atoi(value); err == nil {
		return max(time.Duration(secs)*time.Second, 0)
	}
	if t, err := http.ParseTime(value); err == nil {
		return max(t.Sub(c.clock.Now()), 0)
	}
	return 0
}

// backoff returns the wait before retry attempt+1: between half and all of
// 2^(attempt-1) seconds, at most maxBackoff.
func backoff(attempt int) time.Duration {
	d := maxBackoff
	if attempt < 6 {
		d = min(firstBackoff<<(attempt-1), maxBackoff)
	}
	half := d / 2
	return half + rand.N(half+1) //nolint:gosec // jitter needs no secure randomness
}

func decodedBody(resp *http.Response) (io.Reader, func(), error) {
	if !strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		return resp.Body, func() {}, nil
	}
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("gzip: %w", err)
	}
	return zr, func() { _ = zr.Close() }, nil
}

// isMissingFile reports whether a 403 comes from the file store behind
// www.sec.gov, which answers AccessDenied for a file that does not exist. The
// 403 for a missing User-Agent or too many requests is an HTML page.
func isMissingFile(resp *http.Response) bool {
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !strings.HasSuffix(mediaType, "xml") {
		return false
	}
	body, closeBody, err := decodedBody(resp)
	if err != nil {
		return false
	}
	defer closeBody()
	head, err := io.ReadAll(io.LimitReader(body, 4096))
	if err != nil {
		return false
	}
	return bytes.Contains(head, []byte("<Code>AccessDenied</Code>")) || bytes.Contains(head, []byte("<Code>NoSuchKey</Code>"))
}
