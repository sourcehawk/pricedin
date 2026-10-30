package massive_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/prices"
	"github.com/sourcehawk/pricedin/server/internal/prices/massive"
)

var _ prices.Provider = (*massive.Client)(nil)

const testKey = "test-key-0123456789"

func date(s string) domain.Date {
	d, err := domain.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

// newClient returns a client for srv with fast retries. change, when not
// nil, edits the options first.
func newClient(t *testing.T, srv *httptest.Server, change func(*massive.Options)) *massive.Client {
	t.Helper()
	opts := massive.Options{
		BaseURL:    srv.URL,
		APIKey:     testKey,
		RateLimit:  1000,
		RetryDelay: time.Millisecond,
		HTTPClient: srv.Client(),
	}
	if change != nil {
		change(&opts)
	}
	c, err := massive.New(opts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return c
}

func serveJSON(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func serveStatus(code int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}
}

// recorder keeps every recorded body by key, in call order.
type recorder struct {
	mu     sync.Mutex
	keys   []string
	bodies [][]byte
	err    error
}

func (r *recorder) Record(_ context.Context, key string, body []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys = append(r.keys, key)
	r.bodies = append(r.bodies, bytes.Clone(body))
	return r.err
}

func TestNewRequiresAnAPIKey(t *testing.T) {
	if _, err := massive.New(massive.Options{}); err == nil {
		t.Error("New() without a key error = nil, want an error")
	}
}

func TestNewRejectsABaseURLThatIsNotHTTP(t *testing.T) {
	for _, base := range []string{"ftp://api.massive.com", "api.massive.com", "https://"} {
		if _, err := massive.New(massive.Options{APIKey: testKey, BaseURL: base}); err == nil {
			t.Errorf("New(BaseURL %q) error = nil, want an error", base)
		}
	}
}

func TestNameIsMassive(t *testing.T) {
	srv := httptest.NewServer(serveJSON(`{}`))
	defer srv.Close()
	if got := newClient(t, srv, nil).Name(); got != "massive" {
		t.Errorf("Name() = %q, want %q", got, "massive")
	}
}

func TestRequestsCarryTheKeyAsABearerToken(t *testing.T) {
	var gotAuth, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		serveJSON(`{"status":"OK","results":[]}`)(w, r)
	}))
	defer srv.Close()

	if _, err := newClient(t, srv, nil).Splits(t.Context(), "NVDA"); err != nil {
		t.Fatalf("Splits() error = %v", err)
	}
	if want := "Bearer " + testKey; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
	if strings.Contains(gotQuery, testKey) {
		t.Errorf("query %q carries the key", gotQuery)
	}
}

func TestGroupedDailyReturnsRawBars(t *testing.T) {
	var gotPath, gotAdjusted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAdjusted = r.URL.Path, r.URL.Query().Get("adjusted")
		// 1718078400000 is 2024-06-11 00:00 in New York.
		serveJSON(`{"queryCount":2,"resultsCount":2,"adjusted":false,"status":"OK","request_id":"r1","results":[
			{"T":"NVDA","v":300000000.5,"vw":120.5,"o":121,"c":120.5,"h":122.5,"l":118,"t":1718078400000,"n":2000000},
			{"T":"AAPL","v":90000000,"vw":205,"o":200,"c":207,"h":208,"l":199,"t":1718078400000,"n":1000000}]}`)(w, r)
	}))
	defer srv.Close()

	got, err := newClient(t, srv, nil).GroupedDaily(t.Context(), date("2024-06-11"))
	if err != nil {
		t.Fatalf("GroupedDaily() error = %v", err)
	}
	if want := "/v2/aggs/grouped/locale/us/market/stocks/2024-06-11"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAdjusted != "false" {
		t.Errorf("adjusted = %q, want false", gotAdjusted)
	}
	want := []domain.PriceBar{
		{Symbol: "AAPL", Date: date("2024-06-11"), Open: 200, High: 208, Low: 199, Close: 207, Volume: 90000000, Source: "massive"},
		{Symbol: "NVDA", Date: date("2024-06-11"), Open: 121, High: 122.5, Low: 118, Close: 120.5, Volume: 300000000.5, Source: "massive"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("GroupedDaily() mismatch (-want +got):\n%s", diff)
	}
}

func TestGroupedDailyAcceptsTheDelayedStatus(t *testing.T) {
	srv := httptest.NewServer(serveJSON(`{"status":"DELAYED","results":[{"T":"KO","o":1,"h":1,"l":1,"c":1,"v":1,"t":1718078400000}]}`))
	defer srv.Close()
	got, err := newClient(t, srv, nil).GroupedDaily(t.Context(), date("2024-06-11"))
	if err != nil || len(got) != 1 {
		t.Errorf("GroupedDaily() = %v, %v; want one bar", got, err)
	}
}

func TestEmptyResultsAreNotAnError(t *testing.T) {
	tests := []struct {
		name string
		body string
		call func(context.Context, *massive.Client) (int, error)
	}{
		{
			name: "grouped daily on a holiday",
			body: `{"queryCount":0,"resultsCount":0,"adjusted":false,"status":"OK","request_id":"r1"}`,
			call: func(ctx context.Context, c *massive.Client) (int, error) {
				bars, err := c.GroupedDaily(ctx, date("2025-12-25"))
				return len(bars), err
			},
		},
		{
			name: "daily bars of an unknown symbol",
			body: `{"ticker":"ZZZZ","queryCount":0,"resultsCount":0,"adjusted":false,"status":"OK","request_id":"r1"}`,
			call: func(ctx context.Context, c *massive.Client) (int, error) {
				bars, err := c.DailyBars(ctx, "ZZZZ", date("2024-01-02"), date("2024-01-10"))
				return len(bars), err
			},
		},
		{
			name: "splits of a symbol that never split",
			body: `{"status":"OK","request_id":"r1","results":[]}`,
			call: func(ctx context.Context, c *massive.Client) (int, error) {
				splits, err := c.Splits(ctx, "KO")
				return len(splits), err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(serveJSON(tt.body))
			defer srv.Close()
			n, err := tt.call(t.Context(), newClient(t, srv, nil))
			if err != nil || n != 0 {
				t.Errorf("got %d results, error %v; want none and no error", n, err)
			}
		})
	}
}

const notAuthorizedBody = `{"status":"NOT_AUTHORIZED","request_id":"r1","message":"Your plan doesn't include this data timeframe."}`

func TestDataOutsideThePlanIsNotAuthorized(t *testing.T) {
	srv := httptest.NewServer(serveStatus(http.StatusForbidden, notAuthorizedBody))
	defer srv.Close()
	c := newClient(t, srv, func(o *massive.Options) {
		o.Now = func() time.Time { return time.Date(2026, time.October, 30, 12, 0, 0, 0, time.UTC) }
	})

	if _, err := c.GroupedDaily(t.Context(), date("2019-06-03")); !errors.Is(err, prices.ErrNotAuthorized) {
		t.Errorf("GroupedDaily(too old) error = %v, want ErrNotAuthorized", err)
	}
	if _, err := c.DailyBars(t.Context(), "AAPL", date("2019-06-03"), date("2019-06-10")); !errors.Is(err, prices.ErrNotAuthorized) {
		t.Errorf("DailyBars(too old) error = %v, want ErrNotAuthorized", err)
	}
}

// newYorkTime returns a moment in New York.
func newYorkTime(t *testing.T, s string) time.Time {
	t.Helper()
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	tm, err := time.ParseInLocation("2006-01-02 15:04", s, newYork)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

// sessionServer serves a grouped daily answer and a per-ticker answer with
// one bar on each of 2026-10-28, 2026-10-29 and 2026-10-30, and records the
// paths it was asked for.
func sessionServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var paths []string
	// Midnight in New York of 2026-10-28, 2026-10-29 and 2026-10-30.
	bars := `[{"T":"AAPL","o":1,"h":1,"l":1,"c":1,"v":1,"t":1793160000000},{"T":"AAPL","o":1,"h":1,"l":1,"c":2,"v":1,"t":1793246400000},{"T":"AAPL","o":1,"h":1,"l":1,"c":3,"v":1,"t":1793332800000}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/v2/aggs/grouped/") {
			serveJSON(`{"status":"DELAYED","results":[{"T":"AAPL","o":1,"h":1,"l":1,"c":3,"v":1,"t":1793332800000}]}`)(w, r)
			return
		}
		serveJSON(`{"ticker":"AAPL","status":"DELAYED","results":`+bars+`}`)(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &paths
}

func TestASessionIsOverAt20InNewYork(t *testing.T) {
	tests := []struct {
		now       string
		wantLast  string // the date of the last bar that DailyBars returns
		wantGroup bool   // whether GroupedDaily(2026-10-30) asks Massive
	}{
		{"2026-10-30 19:59", "2026-10-29", false},
		{"2026-10-30 20:00", "2026-10-30", true},
		{"2026-10-31 10:00", "2026-10-30", true}, // a Saturday
		{"2026-10-30 09:00", "2026-10-29", false},
	}
	for _, tt := range tests {
		t.Run(tt.now, func(t *testing.T) {
			srv, paths := sessionServer(t)
			c := newClient(t, srv, func(o *massive.Options) { o.Now = func() time.Time { return newYorkTime(t, tt.now) } })

			bars, err := c.DailyBars(t.Context(), "AAPL", date("2026-10-28"), date("2026-11-06"))
			if err != nil {
				t.Fatalf("DailyBars() error = %v", err)
			}
			if got := bars[len(bars)-1].Date.String(); got != tt.wantLast {
				t.Errorf("last bar of DailyBars = %s, want %s", got, tt.wantLast)
			}
			if want := "/v2/aggs/ticker/AAPL/range/1/day/2026-10-28/" + tt.wantLast; (*paths)[0] != want {
				t.Errorf("DailyBars asked for %s, want %s (to cut to the last finished session)", (*paths)[0], want)
			}

			grouped, err := c.GroupedDaily(t.Context(), date("2026-10-30"))
			if err != nil {
				t.Fatalf("GroupedDaily() error = %v", err)
			}
			asked := len(*paths) == 2
			if asked != tt.wantGroup || (len(grouped) > 0) != tt.wantGroup {
				t.Errorf("GroupedDaily(2026-10-30): asked Massive %t with %d bars, want %t", asked, len(grouped), tt.wantGroup)
			}
		})
	}
}

func TestDataAfterTheLastFinishedSessionIsEmptyWithoutARequest(t *testing.T) {
	srv, paths := sessionServer(t)
	c := newClient(t, srv, func(o *massive.Options) { o.Now = func() time.Time { return newYorkTime(t, "2026-10-30 15:00") } })

	for _, d := range []string{"2026-10-30", "2026-11-05"} {
		bars, err := c.GroupedDaily(t.Context(), date(d))
		if err != nil || len(bars) != 0 {
			t.Errorf("GroupedDaily(%s) = %v, %v; want no bars and no error", d, bars, err)
		}
	}
	bars, err := c.DailyBars(t.Context(), "AAPL", date("2026-10-30"), date("2026-11-06"))
	if err != nil || len(bars) != 0 {
		t.Errorf("DailyBars(from today) = %v, %v; want no bars and no error", bars, err)
	}
	if len(*paths) != 0 {
		t.Errorf("requests = %v, want none", *paths)
	}
}

func TestBarsOfAnUnfinishedSessionAreDropped(t *testing.T) {
	// The server answers a request that ends on 2026-10-29 with a bar of
	// 2026-10-30 too, as a provider that does not respect the range could.
	srv, _ := sessionServer(t)
	c := newClient(t, srv, func(o *massive.Options) { o.Now = func() time.Time { return newYorkTime(t, "2026-10-30 15:00") } })

	bars, err := c.DailyBars(t.Context(), "AAPL", date("2026-10-28"), date("2026-10-30"))
	if err != nil {
		t.Fatalf("DailyBars() error = %v", err)
	}
	for _, b := range bars {
		if b.Date.After(date("2026-10-29")) {
			t.Errorf("DailyBars returned a bar of %s, a session that is not over", b.Date)
		}
	}
	if len(bars) != 2 {
		t.Errorf("DailyBars returned %d bars, want the 2 of the finished sessions", len(bars))
	}
}

func TestDailyBarsFollowsEveryNextURL(t *testing.T) {
	var calls atomic.Int32
	var mux http.ServeMux
	var authOnEveryPage atomic.Bool
	authOnEveryPage.Store(true)
	mux.HandleFunc("/v2/aggs/ticker/NVDA/range/1/day/2024-06-06/2024-06-11", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			authOnEveryPage.Store(false)
		}
		// The cursor of a next_url carries the other parameters.
		if r.URL.Query().Get("cursor") == "" && r.URL.Query().Get("adjusted") != "false" {
			http.Error(w, "want adjusted=false", http.StatusBadRequest)
			return
		}
		// The next_url points at the real host; the client must follow it
		// on its own base URL.
		switch r.URL.Query().Get("cursor") {
		case "":
			serveJSON(`{"ticker":"NVDA","status":"OK","results":[{"o":1,"h":1,"l":1,"c":1100,"v":1,"t":1717646400000}],
				"next_url":"https://api.massive.com/v2/aggs/ticker/NVDA/range/1/day/2024-06-06/2024-06-11?cursor=p2"}`)(w, r)
		case "p2":
			serveJSON(`{"ticker":"NVDA","status":"OK","results":[{"o":1,"h":1,"l":1,"c":1150,"v":1,"t":1717732800000}],
				"next_url":"https://api.massive.com/v2/aggs/ticker/NVDA/range/1/day/2024-06-06/2024-06-11?cursor=p3"}`)(w, r)
		case "p3":
			serveJSON(`{"ticker":"NVDA","status":"OK","results":[{"o":1,"h":1,"l":1,"c":116,"v":1,"t":1717992000000},{"o":1,"h":1,"l":1,"c":117,"v":1,"t":1718078400000}]}`)(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewServer(&mux)
	defer srv.Close()

	got, err := newClient(t, srv, nil).DailyBars(t.Context(), "NVDA", date("2024-06-06"), date("2024-06-11"))
	if err != nil {
		t.Fatalf("DailyBars() error = %v", err)
	}
	closes := make([]float64, 0, len(got))
	dates := make([]string, 0, len(got))
	for _, b := range got {
		closes = append(closes, b.Close)
		dates = append(dates, b.Date.String())
	}
	if diff := cmp.Diff([]float64{1100, 1150, 116, 117}, closes); diff != "" {
		t.Errorf("closes mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"2024-06-06", "2024-06-07", "2024-06-10", "2024-06-11"}, dates); diff != "" {
		t.Errorf("dates mismatch (-want +got):\n%s", diff)
	}
	if calls.Load() != 3 {
		t.Errorf("requests = %d, want 3", calls.Load())
	}
	if !authOnEveryPage.Load() {
		t.Error("a next_url request did not carry the bearer token")
	}
}

func TestANextURLThatRepeatsTheSamePageFails(t *testing.T) {
	srv := httptest.NewServer(serveJSON(`{"status":"OK","results":[],"next_url":"https://api.massive.com/stocks/v1/splits?cursor=same"}`))
	defer srv.Close()
	if _, err := newClient(t, srv, nil).Splits(t.Context(), "NVDA"); err == nil {
		t.Error("Splits() error = nil, want an error for a next_url loop")
	}
}

func TestDailyBarsRejectsARangeThatEndsBeforeItStarts(t *testing.T) {
	srv := httptest.NewServer(serveJSON(`{"status":"OK"}`))
	defer srv.Close()
	if _, err := newClient(t, srv, nil).DailyBars(t.Context(), "NVDA", date("2024-06-11"), date("2024-06-10")); err == nil {
		t.Error("DailyBars(from after to) error = nil, want an error")
	}
}

func TestARetryableResponseIsRetried(t *testing.T) {
	for _, code := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) < 3 {
					serveStatus(code, `{"status":"ERROR","error":"try later"}`)(w, r)
					return
				}
				serveJSON(`{"status":"OK","results":[{"id":"s1","execution_date":"2024-06-10","split_from":1,"split_to":10,"ticker":"NVDA"}]}`)(w, r)
			}))
			defer srv.Close()

			got, err := newClient(t, srv, nil).Splits(t.Context(), "NVDA")
			if err != nil {
				t.Fatalf("Splits() error = %v", err)
			}
			if len(got) != 1 || calls.Load() != 3 {
				t.Errorf("got %d splits after %d requests, want 1 split after 3 requests", len(got), calls.Load())
			}
		})
	}
}

func TestATransportErrorIsRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			conn, _, err := http.NewResponseController(w).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		serveJSON(`{"status":"OK","results":[]}`)(w, r)
	}))
	defer srv.Close()

	if _, err := newClient(t, srv, nil).Splits(t.Context(), "NVDA"); err != nil {
		t.Fatalf("Splits() error = %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("requests = %d, want 2", calls.Load())
	}
}

func TestRetriesStopAfterMaxRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		serveStatus(http.StatusTooManyRequests, `{"status":"ERROR","error":"slow down"}`)(w, r)
	}))
	defer srv.Close()

	_, err := newClient(t, srv, func(o *massive.Options) { o.MaxRetries = 2 }).Splits(t.Context(), "NVDA")
	var apiErr *massive.APIError
	if !errors.As(err, &apiErr) || apiErr.HTTPStatus != http.StatusTooManyRequests {
		t.Errorf("Splits() error = %v, want an *APIError with HTTP status 429", err)
	}
	if calls.Load() != 3 {
		t.Errorf("requests = %d, want 3 (the first and 2 retries)", calls.Load())
	}
}

func TestAClientErrorIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		serveStatus(http.StatusBadRequest, `{"status":"ERROR","request_id":"r9","error":"The path parameter date is invalid."}`)(w, r)
	}))
	defer srv.Close()

	_, err := newClient(t, srv, nil).GroupedDaily(t.Context(), date("2024-06-10"))
	var apiErr *massive.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("GroupedDaily() error = %v, want an *APIError", err)
	}
	want := massive.APIError{HTTPStatus: 400, Status: "ERROR", Message: "The path parameter date is invalid.", RequestID: "r9"}
	if diff := cmp.Diff(want, *apiErr, cmp.FilterPath(func(p cmp.Path) bool { return p.Last().String() == ".Path" }, cmp.Ignore())); diff != "" {
		t.Errorf("APIError mismatch (-want +got):\n%s", diff)
	}
	if calls.Load() != 1 {
		t.Errorf("requests = %d, want 1", calls.Load())
	}
}

func TestTheContextCancelsARetryWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3600")
		serveStatus(http.StatusTooManyRequests, `{"status":"ERROR"}`)(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := newClient(t, srv, func(o *massive.Options) { o.RetryDelay = time.Hour }).Splits(ctx, "NVDA")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Splits() error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Splits() returned after %v, want soon after the context ended", elapsed)
	}
}

func TestTheRateLimitSpacesRequests(t *testing.T) {
	srv := httptest.NewServer(serveJSON(`{"status":"OK","results":[]}`))
	defer srv.Close()
	c := newClient(t, srv, func(o *massive.Options) { o.RateLimit = 10 })

	start := time.Now()
	for range 4 {
		if _, err := c.Splits(t.Context(), "NVDA"); err != nil {
			t.Fatalf("Splits() error = %v", err)
		}
	}
	// At 10 requests a second, 4 requests take at least 3 intervals of
	// 100 ms.
	if elapsed := time.Since(start); elapsed < 290*time.Millisecond {
		t.Errorf("4 requests took %v, want at least 300ms at 10 a second", elapsed)
	}
}

func TestTheKeyNeverAppearsInAnErrorFromAnHTTP200Body(t *testing.T) {
	body := `{"status":"ERROR-` + testKey + `","request_id":"id-` + testKey + `","error":"bad key ` + testKey + `"}`
	srv := httptest.NewServer(serveJSON(body))
	defer srv.Close()
	c := newClient(t, srv, nil)

	_, err := c.Splits(t.Context(), "NVDA")
	var apiErr *massive.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Splits() error = %v, want an *APIError", err)
	}
	if strings.Contains(err.Error(), testKey) || strings.Contains(apiErr.Status, testKey) || strings.Contains(apiErr.Message, testKey) || strings.Contains(apiErr.RequestID, testKey) {
		t.Errorf("Splits() error %q carries the key", err)
	}
	if _, err := c.Ticker(t.Context(), "GRMN"); err == nil || strings.Contains(err.Error(), testKey) {
		t.Errorf("Ticker() error = %v, want an error without the key", err)
	}
}

func TestTheKeyNeverAppearsInErrorsOrLogs(t *testing.T) {
	var mux http.ServeMux
	mux.HandleFunc("/stocks/v1/dividends", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apiKey") != "" {
			http.Error(w, "apiKey reached the server", http.StatusTeapot)
			return
		}
		if r.URL.Query().Get("cursor") == "" {
			serveJSON(`{"status":"OK","results":[],"next_url":"https://api.massive.com/stocks/v1/dividends?cursor=p2&apiKey=`+testKey+`"}`)(w, r)
			return
		}
		serveStatus(http.StatusInternalServerError, `{"status":"ERROR","error":"failed for key `+testKey+`"}`)(w, r)
	})
	srv := httptest.NewServer(&mux)
	defer srv.Close()

	var logs bytes.Buffer
	c := newClient(t, srv, func(o *massive.Options) {
		o.MaxRetries = 1
		o.Logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	_, err := c.Dividends(t.Context(), "KO")
	if err == nil {
		t.Fatal("Dividends() error = nil, want the HTTP 500 error")
	}
	if strings.Contains(err.Error(), testKey) {
		t.Errorf("error %q contains the key", err)
	}
	if !strings.Contains(err.Error(), "cursor=p2") {
		t.Errorf("error %q does not name the failing page", err)
	}
	if logs.Len() == 0 {
		t.Error("no log line for the retry")
	}
	if strings.Contains(logs.String(), testKey) {
		t.Errorf("logs contain the key:\n%s", logs.String())
	}
}

func TestTheRecorderGetsEveryPageBeforeDecoding(t *testing.T) {
	pages := map[string]string{
		"":   `{"status":"OK","results":[{"id":"a","execution_date":"2024-06-10"}],"next_url":"https://api.massive.com/stocks/v1/splits?cursor=p2"}`,
		"p2": `{"status":"OK","results":[{"id":"b", "execution_date": "not a date"}]}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveJSON(pages[r.URL.Query().Get("cursor")])(w, r)
	}))
	defer srv.Close()

	rec := &recorder{}
	_, err := newClient(t, srv, func(o *massive.Options) { o.Recorder = rec }).Splits(t.Context(), "NVDA")
	if err == nil {
		t.Fatal("Splits() error = nil, want a decode error for page 2")
	}
	wantKeys := []string{"prices/massive/splits/NVDA/page-1.json", "prices/massive/splits/NVDA/page-2.json"}
	if diff := cmp.Diff(wantKeys, rec.keys); diff != "" {
		t.Errorf("recorded keys mismatch (-want +got):\n%s", diff)
	}
	if len(rec.bodies) == 2 && string(rec.bodies[1]) != pages["p2"] {
		t.Errorf("recorded body of page 2 = %s, want %s", rec.bodies[1], pages["p2"])
	}
}

func TestRecorderKeys(t *testing.T) {
	srv := httptest.NewServer(serveJSON(`{"status":"OK","results":[]}`))
	defer srv.Close()
	rec := &recorder{}
	c := newClient(t, srv, func(o *massive.Options) { o.Recorder = rec })
	ctx := t.Context()

	calls := []func() error{
		func() error { _, err := c.GroupedDaily(ctx, date("2026-10-29")); return err },
		func() error { _, err := c.DailyBars(ctx, "BRK.B", date("2024-06-05"), date("2024-06-12")); return err },
		func() error { _, err := c.Splits(ctx, "NVDA"); return err },
		func() error { _, err := c.Dividends(ctx, "KO"); return err },
		func() error { _, err := c.Tickers(ctx); return err },
	}
	for i, call := range calls {
		if err := call(); err != nil {
			t.Fatalf("call %d error = %v", i, err)
		}
	}
	want := []string{
		"prices/massive/grouped/2026-10-29/page-1.json",
		"prices/massive/aggs/BRK.B/2024-06-05_2024-06-12/page-1.json",
		"prices/massive/splits/NVDA/page-1.json",
		"prices/massive/dividends/KO/page-1.json",
		"prices/massive/tickers/stocks-active/page-1.json",
	}
	if diff := cmp.Diff(want, rec.keys); diff != "" {
		t.Errorf("recorded keys mismatch (-want +got):\n%s", diff)
	}
}

func TestARecorderErrorFailsTheCall(t *testing.T) {
	srv := httptest.NewServer(serveJSON(`{"status":"OK","results":[]}`))
	defer srv.Close()
	boom := errors.New("disk full")
	_, err := newClient(t, srv, func(o *massive.Options) { o.Recorder = &recorder{err: boom} }).Splits(t.Context(), "NVDA")
	if !errors.Is(err, boom) {
		t.Errorf("Splits() error = %v, want %v", err, boom)
	}
}

func TestBarDatesAreNewYorkSessionDates(t *testing.T) {
	// Each t is midnight in New York of the session, as Massive sends it,
	// except the last one, which is 16:00, the end of the session.
	tests := []struct {
		t    int64
		want string
	}{
		{1709874000000, "2024-03-08"}, // 05:00 UTC: EST, the Friday before the change to daylight saving time
		{1710129600000, "2024-03-11"}, // 04:00 UTC: EDT, the Monday after
		{1730433600000, "2024-11-01"}, // 04:00 UTC: EDT, the Friday before the change back
		{1730696400000, "2024-11-04"}, // 05:00 UTC: EST, the Monday after
		{1710187200000, "2024-03-11"}, // 20:00 UTC: 16:00 EDT
	}
	results := make([]string, 0, len(tests))
	for _, tt := range tests {
		results = append(results, fmt.Sprintf(`{"T":"X%d","o":1,"h":1,"l":1,"c":1,"v":1,"t":%d}`, len(results), tt.t))
	}
	srv := httptest.NewServer(serveJSON(`{"status":"OK","results":[` + strings.Join(results, ",") + `]}`))
	defer srv.Close()

	bars, err := newClient(t, srv, nil).GroupedDaily(t.Context(), date("2024-03-11"))
	if err != nil {
		t.Fatalf("GroupedDaily() error = %v", err)
	}
	got := map[string]string{}
	for _, b := range bars {
		got[b.Symbol] = b.Date.String()
	}
	for i, tt := range tests {
		if sym := fmt.Sprintf("X%d", i); got[sym] != tt.want {
			t.Errorf("t=%d gives %s, want %s", tt.t, got[sym], tt.want)
		}
	}
}

// recordWaits makes c record each retry wait in place of waiting it.
func recordWaits(c *massive.Client) *[]time.Duration {
	var waits []time.Duration
	massive.SetSleep(c, func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	})
	return &waits
}

// serveRetryAfter answers every request with HTTP 429 and the given
// Retry-After header, or none when it is empty.
func serveRetryAfter(header string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if header != "" {
			w.Header().Set("Retry-After", header)
		}
		serveStatus(http.StatusTooManyRequests, `{"status":"ERROR"}`)(w, r)
	}
}

func TestRetryWaitsDoubleWithJitter(t *testing.T) {
	srv := httptest.NewServer(serveRetryAfter(""))
	defer srv.Close()
	c := newClient(t, srv, func(o *massive.Options) { o.RetryDelay = time.Second; o.MaxRetries = 4 })
	waits := recordWaits(c)

	if _, err := c.Splits(t.Context(), "NVDA"); err == nil {
		t.Fatal("Splits() error = nil, want the HTTP 429 error")
	}
	if len(*waits) != 4 {
		t.Fatalf("waits = %v, want 4", *waits)
	}
	for k, w := range *waits {
		full := time.Second << k
		if w < full/2 || w > full {
			t.Errorf("wait %d = %v, want from %v to %v", k+1, w, full/2, full)
		}
	}
}

func TestRetryWaitsStopGrowingAtOneMinute(t *testing.T) {
	srv := httptest.NewServer(serveRetryAfter(""))
	defer srv.Close()
	c := newClient(t, srv, func(o *massive.Options) { o.RetryDelay = 40 * time.Second; o.MaxRetries = 3 })
	waits := recordWaits(c)

	if _, err := c.Splits(t.Context(), "NVDA"); err == nil {
		t.Fatal("Splits() error = nil, want the HTTP 429 error")
	}
	for k, w := range *waits {
		if w > time.Minute {
			t.Errorf("wait %d = %v, want at most 1m", k+1, w)
		}
	}
	if last := (*waits)[len(*waits)-1]; last < 30*time.Second {
		t.Errorf("last wait = %v, want at least 30s", last)
	}
}

func TestRetryAfterIsHonoredUpToTenMinutes(t *testing.T) {
	now := time.Date(2026, time.October, 30, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		header string
		want   time.Duration
	}{
		{"5", 5 * time.Second},
		{"3600", 10 * time.Minute},
		{now.Add(30 * time.Second).Format(http.TimeFormat), 30 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			srv := httptest.NewServer(serveRetryAfter(tt.header))
			defer srv.Close()
			c := newClient(t, srv, func(o *massive.Options) {
				o.MaxRetries = 1
				o.Now = func() time.Time { return now }
			})
			waits := recordWaits(c)

			if _, err := c.Splits(t.Context(), "NVDA"); err == nil {
				t.Fatal("Splits() error = nil, want the HTTP 429 error")
			}
			if len(*waits) != 1 || (*waits)[0] != tt.want {
				t.Errorf("waits = %v, want [%v]", *waits, tt.want)
			}
		})
	}
}

func TestANextURLWithoutALeadingSlashIsRejected(t *testing.T) {
	for _, next := range []string{".attacker.example/stolen?cursor=p2", "@attacker.example/x", "?cursor=p2"} {
		t.Run(next, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				serveJSON(`{"status":"OK","results":[],"next_url":"`+next+`"}`)(w, r)
			}))
			defer srv.Close()

			if _, err := newClient(t, srv, nil).Splits(t.Context(), "NVDA"); err == nil {
				t.Error("Splits() error = nil, want an error for the next_url")
			}
			if calls.Load() != 1 {
				t.Errorf("requests = %d, want 1: the client must not follow the next_url", calls.Load())
			}
		})
	}
}
