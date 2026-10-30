package fixtureserver_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/fixtureserver"
	"github.com/sourcehawk/pricedin/server/internal/prices"
	"github.com/sourcehawk/pricedin/server/internal/prices/massive"
	"github.com/sourcehawk/pricedin/server/internal/sec"
)

const (
	testdata = "../../../testdata"
	key      = "test-key"
)

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	h, err := fixtureserver.New(fixtureserver.Options{Root: testdata, MassiveAPIKey: key})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string, header http.Header) (int, string, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header = header
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(body)
}

func day(t *testing.T, s string) domain.Date {
	t.Helper()
	d, err := domain.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestNewRequiresAMassiveKeyAndTheFixtures(t *testing.T) {
	if _, err := fixtureserver.New(fixtureserver.Options{Root: testdata}); err == nil {
		t.Error("New() without a key: want an error")
	}
	if _, err := fixtureserver.New(fixtureserver.Options{Root: t.TempDir(), MassiveAPIKey: key}); err == nil {
		t.Error("New() on an empty folder: want an error")
	}
}

func TestSECPathsRequireAUserAgentWithAnEmail(t *testing.T) {
	srv := newServer(t)
	url := srv.URL + fixtureserver.PrefixSECWWW + "/files/company_tickers_exchange.json"
	for _, ua := range []string{"", "PricedIn"} {
		code, _, body := get(t, url, http.Header{"User-Agent": {ua}})
		if code != http.StatusForbidden || !strings.Contains(body, "Undeclared Automated Tool") {
			t.Errorf("User-Agent %q: got %d %q, want 403 with the SEC page", ua, code, body)
		}
	}
	code, ctype, _ := get(t, url, http.Header{"User-Agent": {"PricedIn test@example.com"}})
	if code != http.StatusOK || ctype != "application/json" {
		t.Errorf("with a User-Agent: got %d %s, want 200 application/json", code, ctype)
	}
}

func TestMissingSECFilesAnswerAsEachHostDoes(t *testing.T) {
	srv := newServer(t)
	ua := http.Header{"User-Agent": {"PricedIn test@example.com"}}
	tests := []struct {
		path     string
		wantCode int
	}{
		{fixtureserver.PrefixSECWWW + "/Archives/edgar/daily-index/2026/QTR3/master.20260829.idx", http.StatusForbidden},
		{fixtureserver.PrefixSECWWW + "/files/", http.StatusForbidden},
		{fixtureserver.PrefixSECData + "/submissions/CIK0000000001.json", http.StatusNotFound},
	}
	for _, tt := range tests {
		code, _, body := get(t, srv.URL+tt.path, ua)
		if code != tt.wantCode {
			t.Errorf("%s: got %d, want %d", tt.path, code, tt.wantCode)
		}
		if tt.wantCode == http.StatusForbidden && !strings.Contains(body, "<Code>AccessDenied</Code>") {
			t.Errorf("%s: body %q, want an AccessDenied error", tt.path, body)
		}
	}
}

func secClient(t *testing.T, srv *httptest.Server) *sec.Client {
	t.Helper()
	c, err := sec.New(sec.Options{
		UserAgent:   "PricedIn test@example.com",
		BaseURL:     srv.URL + fixtureserver.PrefixSECWWW,
		DataBaseURL: srv.URL + fixtureserver.PrefixSECData,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTheSECClientReadsTheFixtures(t *testing.T) {
	srv := newServer(t)
	c := secClient(t, srv)
	ctx := t.Context()

	tickers, err := c.Tickers(ctx)
	if err != nil || len(tickers) == 0 {
		t.Fatalf("Tickers() = %d entries, %v; want the fixture map", len(tickers), err)
	}
	sub, err := c.Submissions(ctx, 1045810)
	if err != nil || sub.Company.Name == "" || len(sub.Filings) == 0 {
		t.Fatalf("Submissions(NVDA) = %+v, %v; want the company and its filings", sub.Company, err)
	}
	facts, err := c.CompanyFacts(ctx, 1045810)
	if err != nil || len(facts) == 0 {
		t.Fatalf("CompanyFacts(NVDA) = %d facts, %v", len(facts), err)
	}
	filings, err := c.DailyIndex(ctx, day(t, "2026-08-26"))
	if err != nil || len(filings) == 0 {
		t.Fatalf("DailyIndex(2026-08-26) = %d filings, %v", len(filings), err)
	}
	if _, err := c.DailyIndex(ctx, day(t, "2026-08-29")); !errors.Is(err, sec.ErrNotFound) {
		t.Errorf("DailyIndex(a Saturday) error = %v, want sec.ErrNotFound", err)
	}
	if _, err := c.CompanyFacts(ctx, 1); !errors.Is(err, sec.ErrNotFound) {
		t.Errorf("CompanyFacts(unknown) error = %v, want sec.ErrNotFound", err)
	}
	var archive strings.Builder
	if n, err := c.DownloadCompanyFactsArchive(ctx, &archive); err != nil || n == 0 {
		t.Errorf("DownloadCompanyFactsArchive() = %d bytes, %v", n, err)
	}
}

// massiveClient returns a client whose clock is just after the last
// fixture session.
func massiveClient(t *testing.T, srv *httptest.Server, apiKey string) *massive.Client {
	t.Helper()
	c, err := massive.New(massive.Options{
		BaseURL: srv.URL + fixtureserver.PrefixMassive, APIKey: apiKey, RateLimit: 1000, MaxRetries: -1,
		Now: func() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMassivePathsRequireTheBearerToken(t *testing.T) {
	srv := newServer(t)
	_, err := massiveClient(t, srv, "wrong").Splits(t.Context(), "NVDA")
	var apiErr *massive.APIError
	if !errors.As(err, &apiErr) || apiErr.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("Splits() with a wrong key error = %v, want HTTP 401", err)
	}
	code, _, _ := get(t, srv.URL+fixtureserver.PrefixMassive+"/stocks/v1/splits?ticker=NVDA", http.Header{})
	if code != http.StatusUnauthorized {
		t.Errorf("without a token: got %d, want 401", code)
	}
}

func TestGroupedDailyWithoutAFileComesFromTheAggregates(t *testing.T) {
	srv := newServer(t)
	c := massiveClient(t, srv, key)
	ctx := t.Context()

	// 2026-10-29 has a grouped file; 2025-03-14 has none.
	for _, d := range []string{"2026-10-29", "2025-03-14"} {
		grouped, err := c.GroupedDaily(ctx, day(t, d))
		if err != nil {
			t.Fatalf("GroupedDaily(%s) error = %v", d, err)
		}
		if len(grouped) < 4 {
			t.Fatalf("GroupedDaily(%s) = %d bars, want one for each fixture symbol", d, len(grouped))
		}
		for _, g := range grouped {
			bars, err := c.DailyBars(ctx, g.Symbol, day(t, d), day(t, d))
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff([]domain.PriceBar{g}, bars); diff != "" {
				t.Errorf("%s on %s: the grouped bar differs from the aggregates (-grouped +aggs):\n%s", g.Symbol, d, diff)
			}
		}
	}
}

func TestGroupedDailyOnAHolidayIsEmptyAndBeforeTheWindowNotAuthorized(t *testing.T) {
	srv := newServer(t)
	c := massiveClient(t, srv, key)
	bars, err := c.GroupedDaily(t.Context(), day(t, "2025-12-25"))
	if err != nil || len(bars) != 0 {
		t.Errorf("GroupedDaily(Christmas) = %d bars, %v; want none and no error", len(bars), err)
	}
	if _, err := c.GroupedDaily(t.Context(), day(t, "2020-03-02")); !errors.Is(err, prices.ErrNotAuthorized) {
		t.Errorf("GroupedDaily(before the window) error = %v, want prices.ErrNotAuthorized", err)
	}
}

func TestDailyBarsAreCutToTheRange(t *testing.T) {
	srv := newServer(t)
	c := massiveClient(t, srv, key)
	bars, err := c.DailyBars(t.Context(), "NVDA", day(t, "2024-06-05"), day(t, "2024-06-11"))
	if err != nil {
		t.Fatalf("DailyBars() error = %v", err)
	}
	dates := make([]string, 0, len(bars))
	for _, b := range bars {
		dates = append(dates, b.Date.String())
	}
	want := []string{"2024-06-05", "2024-06-06", "2024-06-07", "2024-06-10", "2024-06-11"}
	if diff := cmp.Diff(want, dates); diff != "" {
		t.Errorf("DailyBars() dates (-want +got):\n%s", diff)
	}
	if none, err := c.DailyBars(t.Context(), "ZZZZ", day(t, "2024-06-05"), day(t, "2024-06-11")); err != nil || len(none) != 0 {
		t.Errorf("DailyBars(unknown) = %d bars, %v; want none", len(none), err)
	}
}

func TestAdjustedDailyBarsDivideByLaterSplits(t *testing.T) {
	srv := newServer(t)
	c := massiveClient(t, srv, key)
	from, to := day(t, "2024-06-07"), day(t, "2024-06-10")
	raw, err := c.DailyBars(t.Context(), "NVDA", from, to)
	if err != nil {
		t.Fatal(err)
	}
	adj, err := c.AdjustedDailyBars(t.Context(), "NVDA", from, to)
	if err != nil || len(adj) != 2 || len(raw) != 2 {
		t.Fatalf("got %d raw and %d adjusted bars, %v; want 2 each", len(raw), len(adj), err)
	}
	if got := raw[0].Close / adj[0].Close; got < 9.999 || got > 10.001 {
		t.Errorf("raw / adjusted close before the split = %v, want 10", got)
	}
	if raw[1].Close != adj[1].Close {
		t.Errorf("close on the split day: raw %v, adjusted %v; want equal", raw[1].Close, adj[1].Close)
	}
}

func TestReferenceEndpoints(t *testing.T) {
	srv := newServer(t)
	c := massiveClient(t, srv, key)
	ctx := context.Background()

	splits, err := c.Splits(ctx, "NVDA")
	if err != nil || len(splits) != 2 || splits[1].Date != day(t, "2024-06-10") {
		t.Errorf("Splits(NVDA) = %v, %v; want the 2021 and the 2024-06-10 split", splits, err)
	}
	if none, err := c.Splits(ctx, "ZZZZ"); err != nil || len(none) != 0 {
		t.Errorf("Splits(unknown) = %v, %v; want none", none, err)
	}
	divs, err := c.Dividends(ctx, "KO")
	if err != nil || len(divs) == 0 {
		t.Errorf("Dividends(KO) = %d, %v; want the fixture dividends", len(divs), err)
	}
	tickers, err := c.Tickers(ctx)
	if err != nil || len(tickers) < 4 {
		t.Errorf("Tickers() = %d, %v; want every page", len(tickers), err)
	}
	if tk, err := c.Ticker(ctx, "GRMN"); err != nil || tk.CIK != 1121788 {
		t.Errorf("Ticker(GRMN) = %+v, %v", tk, err)
	}
	_, err = c.Ticker(ctx, "ZZZZ")
	var apiErr *massive.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != "NOT_FOUND" {
		t.Errorf("Ticker(unknown) error = %v, want NOT_FOUND", err)
	}
}
