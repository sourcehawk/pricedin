package sec_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/sec"
)

const testUserAgent = "PricedIn test test@example.com"

// fakeClock jumps to the wake time when a caller sleeps, so tests run
// without waiting. It records every wake time that a caller asks for, and
// every sleep that moved the clock.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
	wakes  []time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) SleepUntil(ctx context.Context, t time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wakes = append(c.wakes, t)
	if t.After(c.now) {
		c.sleeps = append(c.sleeps, t.Sub(c.now))
		c.now = t
	}
	return nil
}

func (c *fakeClock) Sleeps() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.sleeps)
}

// blockingClock never wakes a sleeper before its context ends.
type blockingClock struct{ *fakeClock }

func (c *blockingClock) SleepUntil(ctx context.Context, t time.Time) error {
	if !t.After(c.Now()) {
		return ctx.Err()
	}
	<-ctx.Done()
	return ctx.Err()
}

// recorder keeps every recorded body by key.
type recorder struct {
	mu     sync.Mutex
	keys   []string
	bodies map[string][]byte
	err    error
}

func (r *recorder) Record(_ context.Context, key string, body []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	if r.bodies == nil {
		r.bodies = map[string][]byte{}
	}
	r.keys = append(r.keys, key)
	r.bodies[key] = slices.Clone(body)
	return nil
}

// newClient starts a server with handler and returns a client that sends
// both SEC hosts to it.
func newClient(t *testing.T, handler http.Handler, opts sec.Options) *sec.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	if opts.UserAgent == "" {
		opts.UserAgent = testUserAgent
	}
	opts.BaseURL, opts.DataBaseURL = srv.URL, srv.URL
	if opts.Clock == nil {
		opts.Clock = newFakeClock()
	}
	c, err := sec.New(opts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return c
}

// requestLog counts the requests that reach a handler.
type requestLog struct {
	mu    sync.Mutex
	count int
}

func (l *requestLog) add() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.count++
	return l.count
}

func (l *requestLog) n() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.count
}

func gzipBytes(t *testing.T, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	tests := map[string]sec.Options{
		"no user agent":       {},
		"rate above 10":       {UserAgent: testUserAgent, RequestsPerSecond: 11},
		"negative rate":       {UserAgent: testUserAgent, RequestsPerSecond: -1},
		"negative attempts":   {UserAgent: testUserAgent, MaxAttempts: -1},
		"relative base URL":   {UserAgent: testUserAgent, BaseURL: "www.sec.gov"},
		"data URL not http":   {UserAgent: testUserAgent, DataBaseURL: "ftp://data.sec.gov"},
		"negative retryAfter": {UserAgent: testUserAgent, MaxRetryAfter: -time.Second},
	}
	for name, opts := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := sec.New(opts); err == nil {
				t.Error("New() error = nil, want an error")
			}
		})
	}
}

func TestNewAcceptsDefaults(t *testing.T) {
	if _, err := sec.New(sec.Options{UserAgent: testUserAgent}); err != nil {
		t.Errorf("New() error = %v", err)
	}
}

func TestClientSendsTheUserAgentAndDecodesGzip(t *testing.T) {
	tickers := readFixture(t, "www.sec.gov/files/company_tickers_exchange.json")
	var gotUA, gotEncoding, gotPath string
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA, gotEncoding, gotPath = r.UserAgent(), r.Header.Get("Accept-Encoding"), r.URL.Path
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(gzipBytes(t, tickers))
	}), sec.Options{})

	got, err := c.Tickers(t.Context())
	if err != nil {
		t.Fatalf("Tickers() error = %v", err)
	}
	if gotUA != testUserAgent {
		t.Errorf("User-Agent = %q, want %q", gotUA, testUserAgent)
	}
	if gotEncoding != "gzip" {
		t.Errorf("Accept-Encoding = %q, want gzip", gotEncoding)
	}
	if gotPath != "/files/company_tickers_exchange.json" {
		t.Errorf("path = %q", gotPath)
	}
	if len(got) != 13 {
		t.Errorf("len = %d, want 13", len(got))
	}
}

func TestClientDatesTheTickerMapWithTheRetrievalDay(t *testing.T) {
	tickers := readFixture(t, "www.sec.gov/files/company_tickers_exchange.json")
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(tickers)
	}), sec.Options{})
	got, err := c.Tickers(t.Context())
	if err != nil {
		t.Fatalf("Tickers() error = %v", err)
	}
	if want := date(t, "2026-10-30"); got[0].Ticker.ValidFrom != want {
		t.Errorf("ValidFrom = %v, want %v", got[0].Ticker.ValidFrom, want)
	}
}

func TestClientNeverSendsMoreThanTenRequestsInOneSecond(t *testing.T) {
	clock := newFakeClock()
	var mu sync.Mutex
	var sent []time.Time
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		sent = append(sent, clock.Now())
		mu.Unlock()
		_, _ = w.Write([]byte("CIK|Company Name|Form Type|Date Filed|File Name\n-----\n"))
	}), sec.Options{Clock: clock})

	for range 35 {
		if _, err := c.DailyIndex(t.Context(), date(t, "2026-08-26")); err != nil {
			t.Fatalf("DailyIndex() error = %v", err)
		}
	}
	for i := 10; i < len(sent); i++ {
		if window := sent[i].Sub(sent[i-10]); window < time.Second {
			t.Fatalf("requests %d to %d sent within %v, want at most 10 in any second", i-10, i, window)
		}
	}
	// The limit must not slow the client further than it needs to.
	if total := sent[len(sent)-1].Sub(sent[0]); total != 3400*time.Millisecond {
		t.Errorf("35 requests took %v, want 3.4s", total)
	}
}

func TestClientSharesTheLimitAcrossGoroutines(t *testing.T) {
	clock := newFakeClock()
	start := clock.Now()
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("-----\n"))
	}), sec.Options{Clock: clock})

	var wg sync.WaitGroup
	for range 30 {
		wg.Go(func() {
			if _, err := c.DailyIndex(t.Context(), date(t, "2026-08-26")); err != nil {
				t.Errorf("DailyIndex() error = %v", err)
			}
		})
	}
	wg.Wait()

	// One request goes at once. Each other one waits for its own slot,
	// 100 ms after the slot before it.
	wakes := slices.Clone(clock.wakes)
	slices.SortFunc(wakes, func(a, b time.Time) int { return a.Compare(b) })
	want := make([]time.Time, 0, 29)
	for i := 1; i < 30; i++ {
		want = append(want, start.Add(time.Duration(i)*100*time.Millisecond))
	}
	if diff := cmp.Diff(want, wakes); diff != "" {
		t.Errorf("wake times mismatch (-want +got):\n%s", diff)
	}
}

func TestClientHonorsALowerRate(t *testing.T) {
	clock := newFakeClock()
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("-----\n"))
	}), sec.Options{Clock: clock, RequestsPerSecond: 4})
	for range 3 {
		if _, err := c.DailyIndex(t.Context(), date(t, "2026-08-26")); err != nil {
			t.Fatal(err)
		}
	}
	want := []time.Duration{250 * time.Millisecond, 250 * time.Millisecond}
	if diff := cmp.Diff(want, clock.Sleeps()); diff != "" {
		t.Errorf("sleeps mismatch (-want +got):\n%s", diff)
	}
}

func TestClientRetries5xxAnd429WithGrowingBackoff(t *testing.T) {
	clock := newFakeClock()
	var log requestLog
	statuses := []int{http.StatusServiceUnavailable, http.StatusTooManyRequests, http.StatusBadGateway}
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n := log.add(); n <= len(statuses) {
			w.WriteHeader(statuses[n-1])
			return
		}
		_, _ = w.Write([]byte("-----\n"))
	}), sec.Options{Clock: clock})

	if _, err := c.DailyIndex(t.Context(), date(t, "2026-08-26")); err != nil {
		t.Fatalf("DailyIndex() error = %v", err)
	}
	if log.n() != 4 {
		t.Errorf("requests = %d, want 4", log.n())
	}
	// The wait before retry n is between half and all of 2^(n-1) seconds.
	sleeps := clock.Sleeps()
	if len(sleeps) != 3 {
		t.Fatalf("sleeps = %v, want 3", sleeps)
	}
	for i, d := range sleeps {
		ceiling := time.Second << i
		if d < ceiling/2 || d > ceiling {
			t.Errorf("wait %d = %v, want between %v and %v", i+1, d, ceiling/2, ceiling)
		}
	}
}

func TestClientRetriesTransportErrors(t *testing.T) {
	var log requestLog
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if log.add() == 1 {
			conn, _, err := http.NewResponseController(w).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		_, _ = w.Write([]byte("-----\n"))
	}), sec.Options{})
	if _, err := c.DailyIndex(t.Context(), date(t, "2026-08-26")); err != nil {
		t.Fatalf("DailyIndex() error = %v", err)
	}
	if log.n() != 2 {
		t.Errorf("requests = %d, want 2", log.n())
	}
}

func TestClientWaitsForRetryAfter(t *testing.T) {
	clock := newFakeClock()
	tests := map[string]string{
		"seconds":   "7",
		"http date": clock.Now().Add(7 * time.Second).Format(http.TimeFormat),
	}
	for name, header := range tests {
		t.Run(name, func(t *testing.T) {
			clock := newFakeClock()
			var log requestLog
			c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if log.add() == 1 {
					w.Header().Set("Retry-After", header)
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				_, _ = w.Write([]byte("-----\n"))
			}), sec.Options{Clock: clock})
			if _, err := c.DailyIndex(t.Context(), date(t, "2026-08-26")); err != nil {
				t.Fatalf("DailyIndex() error = %v", err)
			}
			if diff := cmp.Diff([]time.Duration{7 * time.Second}, clock.Sleeps()); diff != "" {
				t.Errorf("sleeps mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestClientGivesUpWhenRetryAfterIsTooLong(t *testing.T) {
	clock := newFakeClock()
	var log requestLog
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		log.add()
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusServiceUnavailable)
	}), sec.Options{Clock: clock, MaxRetryAfter: time.Minute})
	_, err := c.DailyIndex(t.Context(), date(t, "2026-08-26"))
	if err == nil || !strings.Contains(err.Error(), "Retry-After") {
		t.Errorf("error = %v, want one about Retry-After", err)
	}
	if log.n() != 1 || len(clock.Sleeps()) != 0 {
		t.Errorf("requests = %d, sleeps = %v, want 1 request and no wait", log.n(), clock.Sleeps())
	}
}

func TestClientGivesUpAfterMaxAttempts(t *testing.T) {
	var log requestLog
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		log.add()
		w.WriteHeader(http.StatusInternalServerError)
	}), sec.Options{MaxAttempts: 3})
	_, err := c.DailyIndex(t.Context(), date(t, "2026-08-26"))
	var statusErr *sec.StatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("error = %v, want a StatusError with status 500", err)
	}
	if log.n() != 3 {
		t.Errorf("requests = %d, want 3", log.n())
	}
}

func TestClientDoesNotRetryOther4xx(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{http.StatusBadRequest, nil},
		{http.StatusNotFound, sec.ErrNotFound},
		{http.StatusUnauthorized, nil},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			var log requestLog
			c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				log.add()
				w.WriteHeader(tt.status)
			}), sec.Options{})
			_, err := c.CompanyFacts(t.Context(), cikKO)
			if err == nil {
				t.Fatal("error = nil, want an error")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("error = %v, want %v", err, tt.want)
			}
			if log.n() != 1 {
				t.Errorf("requests = %d, want 1", log.n())
			}
		})
	}
}

func TestClientExplainsA403(t *testing.T) {
	var log requestLog
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		log.add()
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<title>SEC.gov | Request Rate Threshold Exceeded</title>"))
	}), sec.Options{})
	_, err := c.CompanyFacts(t.Context(), cikKO)
	if !errors.Is(err, sec.ErrForbidden) {
		t.Fatalf("error = %v, want ErrForbidden", err)
	}
	for _, word := range []string{"User-Agent", "rate limit"} {
		if !strings.Contains(err.Error(), word) {
			t.Errorf("error %q does not mention %q", err, word)
		}
	}
	if log.n() != 1 {
		t.Errorf("requests = %d, want 1", log.n())
	}
}

func TestClientReportsAMissingArchiveFileAsNotFound(t *testing.T) {
	// SEC answers a request for a daily index that does not exist, such as
	// one for a weekend, with this 403 from its file store.
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`))
	}), sec.Options{})
	_, err := c.DailyIndex(t.Context(), date(t, "2026-10-25"))
	if !errors.Is(err, sec.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestClientStopsWaitingWhenTheContextEnds(t *testing.T) {
	t.Run("during a retry wait", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			cancel()
			w.WriteHeader(http.StatusServiceUnavailable)
		}), sec.Options{Clock: &blockingClock{fakeClock: newFakeClock()}})
		_, err := c.DailyIndex(ctx, date(t, "2026-08-26"))
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error = %v, want context.Canceled", err)
		}
	})
	t.Run("during a rate limit wait", func(t *testing.T) {
		var log requestLog
		c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			log.add()
			_, _ = w.Write([]byte("-----\n"))
		}), sec.Options{Clock: &blockingClock{fakeClock: newFakeClock()}})
		if _, err := c.DailyIndex(t.Context(), date(t, "2026-08-26")); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		_, err := c.DailyIndex(ctx, date(t, "2026-08-26"))
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("error = %v, want context.DeadlineExceeded", err)
		}
		if log.n() != 1 {
			t.Errorf("requests = %d, want 1", log.n())
		}
	})
}

// fixtureServer serves testdata/sec at the paths of both SEC hosts.
func fixtureServer(t *testing.T, opts sec.Options) *sec.Client {
	t.Helper()
	www := httptest.NewServer(http.FileServer(http.Dir(fixtureDir + "/www.sec.gov")))
	data := httptest.NewServer(http.FileServer(http.Dir(fixtureDir + "/data.sec.gov")))
	t.Cleanup(www.Close)
	t.Cleanup(data.Close)
	opts.UserAgent = testUserAgent
	opts.BaseURL, opts.DataBaseURL = www.URL, data.URL
	if opts.Clock == nil {
		opts.Clock = newFakeClock()
	}
	c, err := sec.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestClientEndpointsReadTheFixtures(t *testing.T) {
	rec := &recorder{}
	c := fixtureServer(t, sec.Options{Recorder: rec})
	ctx := t.Context()

	sub, err := c.Submissions(ctx, cikNVDA)
	if err != nil {
		t.Fatalf("Submissions() error = %v", err)
	}
	if len(sub.Filings) != 24+88 || sub.Company.Name != "NVIDIA CORP" {
		t.Errorf("Submissions() = %d filings of %q, want 112 of NVIDIA CORP", len(sub.Filings), sub.Company.Name)
	}

	facts, err := c.CompanyFacts(ctx, cikGRMN)
	if err != nil {
		t.Fatalf("CompanyFacts() error = %v", err)
	}
	if diff := cmp.Diff(companyFactsFixture(t, cikGRMN), facts); diff != "" {
		t.Errorf("CompanyFacts() mismatch (-want +got):\n%s", diff)
	}

	filings, err := c.DailyIndex(ctx, date(t, "2026-08-26"))
	if err != nil {
		t.Fatalf("DailyIndex() error = %v", err)
	}
	if len(filings) != 30 {
		t.Errorf("DailyIndex() = %d filings, want 30", len(filings))
	}

	if _, err := c.DailyIndex(ctx, date(t, "2026-08-29")); !errors.Is(err, sec.ErrNotFound) {
		t.Errorf("DailyIndex(no index) error = %v, want ErrNotFound", err)
	}

	wantKeys := []string{
		"sec/submissions/CIK0001045810.json",
		"sec/submissions/CIK0001045810-submissions-001.json",
		"sec/companyfacts/CIK0001121788.json",
		"sec/daily-index/master.20260826.idx",
	}
	if diff := cmp.Diff(wantKeys, rec.keys); diff != "" {
		t.Errorf("recorded keys mismatch (-want +got):\n%s", diff)
	}
	if want := readFixture(t, "data.sec.gov/api/xbrl/companyfacts/CIK0001121788.json"); !bytes.Equal(rec.bodies["sec/companyfacts/CIK0001121788.json"], want) {
		t.Error("recorded company facts differ from the response body")
	}
}

func TestClientRecordsTheDecodedBody(t *testing.T) {
	tickers := readFixture(t, "www.sec.gov/files/company_tickers_exchange.json")
	rec := &recorder{}
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(gzipBytes(t, tickers))
	}), sec.Options{Recorder: rec})
	if _, err := c.Tickers(t.Context()); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{"sec/tickers/company_tickers_exchange.json"}, rec.keys); diff != "" {
		t.Errorf("keys mismatch (-want +got):\n%s", diff)
	}
	if !bytes.Equal(rec.bodies["sec/tickers/company_tickers_exchange.json"], tickers) {
		t.Error("recorded body is not the decoded response")
	}
}

func TestClientFailsWhenTheRecorderFails(t *testing.T) {
	recErr := errors.New("disk full")
	c := fixtureServer(t, sec.Options{Recorder: &recorder{err: recErr}})
	_, err := c.CompanyFacts(t.Context(), cikKO)
	if !errors.Is(err, recErr) {
		t.Errorf("error = %v, want the recorder error", err)
	}
}

func TestClientRecordsABodyThatDoesNotParse(t *testing.T) {
	// The raw body is stored first, so a later fix of the parser can
	// reprocess it.
	rec := &recorder{}
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"cik":`))
	}), sec.Options{Recorder: rec})
	if _, err := c.CompanyFacts(t.Context(), cikKO); err == nil {
		t.Fatal("error = nil, want a parse error")
	}
	if len(rec.keys) != 1 {
		t.Errorf("recorded %d bodies, want 1", len(rec.keys))
	}
}

func TestClientRejectsAnExtraFileNameWithAPath(t *testing.T) {
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"cik":"0000021344","filings":{"recent":{},"files":[{"name":"../files/x.json"}]}}`))
	}), sec.Options{})
	if _, err := c.Submissions(t.Context(), cikKO); err == nil {
		t.Error("error = nil, want an error")
	}
}

func TestClientDownloadsTheBulkArchives(t *testing.T) {
	c := fixtureServer(t, sec.Options{Recorder: &recorder{err: errors.New("archives must not be recorded")}})
	tests := map[string]func(context.Context, *bytes.Buffer) (int64, error){
		companyFactsZip: func(ctx context.Context, w *bytes.Buffer) (int64, error) {
			return c.DownloadCompanyFactsArchive(ctx, w)
		},
		submissionsZip: func(ctx context.Context, w *bytes.Buffer) (int64, error) { return c.DownloadSubmissionsArchive(ctx, w) },
	}
	for path, download := range tests {
		t.Run(path, func(t *testing.T) {
			var buf bytes.Buffer
			n, err := download(t.Context(), &buf)
			if err != nil {
				t.Fatalf("download error = %v", err)
			}
			want := readFixture(t, path)
			if n != int64(len(want)) || !bytes.Equal(buf.Bytes(), want) {
				t.Errorf("downloaded %d bytes that differ from the fixture of %d bytes", n, len(want))
			}
		})
	}
}

func TestClientRetriesADownloadBeforeTheFirstByte(t *testing.T) {
	archive := readFixture(t, companyFactsZip)
	var log requestLog
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if log.add() == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path != "/Archives/edgar/daily-index/xbrl/companyfacts.zip" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(gzipBytes(t, archive))
	}), sec.Options{})
	var buf bytes.Buffer
	if _, err := c.DownloadCompanyFactsArchive(t.Context(), &buf); err != nil {
		t.Fatalf("DownloadCompanyFactsArchive() error = %v", err)
	}
	if !bytes.Equal(buf.Bytes(), archive) || log.n() != 2 {
		t.Errorf("requests = %d, body equal = %v, want 2 requests and the archive", log.n(), bytes.Equal(buf.Bytes(), archive))
	}
}

func TestClientDoesNotRetryADownloadThatBrokeMidway(t *testing.T) {
	var log requestLog
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		log.add()
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte("PK partial"))
	}), sec.Options{})
	var buf bytes.Buffer
	if _, err := c.DownloadSubmissionsArchive(t.Context(), &buf); err == nil {
		t.Error("error = nil, want an error")
	}
	if log.n() != 1 {
		t.Errorf("requests = %d, want 1", log.n())
	}
}

func TestClientRetriesABodyThatBrokeMidway(t *testing.T) {
	facts := readFixture(t, "data.sec.gov/api/xbrl/companyfacts/CIK0000021344.json")
	var log requestLog
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if log.add() == 1 {
			w.Header().Set("Content-Length", "1000000")
			_, _ = w.Write(facts[:100])
			return
		}
		_, _ = w.Write(facts)
	}), sec.Options{})
	if _, err := c.CompanyFacts(t.Context(), cikKO); err != nil {
		t.Fatalf("CompanyFacts() error = %v", err)
	}
	if log.n() != 2 {
		t.Errorf("requests = %d, want 2", log.n())
	}
}

func TestClientRejectsAnInvalidCIK(t *testing.T) {
	c := newClient(t, http.NotFoundHandler(), sec.Options{})
	if _, err := c.CompanyFacts(t.Context(), domain.CIK(0)); err == nil {
		t.Error("CompanyFacts(0) error = nil, want an error")
	}
	if _, err := c.Submissions(t.Context(), domain.CIK(-5)); err == nil {
		t.Error("Submissions(-5) error = nil, want an error")
	}
}
