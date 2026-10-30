package massive_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/prices"
	"github.com/sourcehawk/pricedin/server/internal/prices/massive"
)

var _ prices.StoredReader = massive.Stored{}

func TestStoredNameIsTheProviderName(t *testing.T) {
	srv := httptest.NewServer(serveJSON(`{}`))
	defer srv.Close()
	if got, want := (massive.Stored{}).Name(), newClient(t, srv, nil).Name(); got != want {
		t.Errorf("Name() = %q, want the client's %q", got, want)
	}
}

// Every key that the client gives its recorder names its request.
func TestStoredRequestReadsTheKeysThatTheClientRecords(t *testing.T) {
	pages := map[string]string{
		"":   `{"status":"OK","results":[],"next_url":"https://api.massive.com/v3/reference/tickers?cursor=p2"}`,
		"p2": `{"status":"OK","results":[]}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := `{"status":"OK","results":[]}`
		if r.URL.Path == "/v3/reference/tickers" {
			body = pages[r.URL.Query().Get("cursor")]
		}
		serveJSON(body)(w, r)
	}))
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
	want := []prices.StoredRequest{
		{Kind: prices.StoredGroupedDaily, Date: date("2026-10-29"), Page: 1},
		{Kind: prices.StoredDailyBars, Symbol: "BRK.B", From: date("2024-06-05"), To: date("2024-06-12"), Page: 1},
		{Kind: prices.StoredSplits, Symbol: "NVDA", Page: 1},
		{Kind: prices.StoredDividends, Symbol: "KO", Page: 1},
		{Kind: prices.StoredTickers, Page: 1},
		{Kind: prices.StoredTickers, Page: 2},
	}
	got := make([]prices.StoredRequest, 0, len(rec.keys))
	for _, key := range rec.keys {
		req, ok, err := massive.Stored{}.StoredRequest(key)
		if err != nil || !ok {
			t.Fatalf("StoredRequest(%q) = %v, %v, want a request", key, ok, err)
		}
		got = append(got, req)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("requests mismatch (-want +got):\n%s", diff)
	}
}

func TestStoredRequestUnescapesTheSymbol(t *testing.T) {
	req, ok, err := massive.Stored{}.StoredRequest("prices/massive/splits/A%2FB/page-3.json")
	if err != nil || !ok {
		t.Fatalf("StoredRequest() = %v, %v, want a request", ok, err)
	}
	if want := (prices.StoredRequest{Kind: prices.StoredSplits, Symbol: "A/B", Page: 3}); req != want {
		t.Errorf("StoredRequest() = %+v, want %+v", req, want)
	}
}

// Split-adjusted bars and the details of one ticker are never stored, so
// their pages hold nothing to load.
func TestStoredRequestSkipsPagesThatHoldNothingToStore(t *testing.T) {
	for _, key := range []string{
		"prices/massive/aggs-adjusted/NVDA/2024-06-05_2024-06-12/page-1.json",
		"prices/massive/ticker/NVDA/page-1.json",
	} {
		if _, ok, err := (massive.Stored{}).StoredRequest(key); ok || err != nil {
			t.Errorf("StoredRequest(%q) = %v, %v, want no request and no error", key, ok, err)
		}
	}
}

func TestStoredRequestRejectsAKeyThatTheClientNeverMakes(t *testing.T) {
	for _, key := range []string{
		"prices/stooq/splits/NVDA/page-1.json",
		"prices/massive/quotes/NVDA/page-1.json",
		"prices/massive/splits/NVDA/page-0.json",
		"prices/massive/splits/NVDA/page-x.json",
		"prices/massive/splits/NVDA/first.json",
		"prices/massive/splits/page-1.json",
		"prices/massive/page-1.json",
		"prices/massive/grouped/2026-13-01/page-1.json",
		"prices/massive/aggs/NVDA/2024-06-05/page-1.json",
		"prices/massive/aggs/NVDA/2024-06-12_2024-06-05/page-1.json",
		"prices/massive/tickers/stocks-delisted/page-1.json",
	} {
		if _, _, err := (massive.Stored{}).StoredRequest(key); err == nil {
			t.Errorf("StoredRequest(%q) error = nil, want an error", key)
		}
	}
}

func TestDecodeStoredReturnsTheItemsOfThePage(t *testing.T) {
	body := `{"status":"OK","results":[{"id":"e1","ticker":"NVDA","execution_date":"2024-06-10","split_from":1,"split_to":10}],
		"next_url":"https://api.massive.com/stocks/v1/splits?cursor=p2"}`
	page, err := massive.Stored{}.DecodeStored(prices.StoredRequest{Kind: prices.StoredSplits, Symbol: "NVDA", Page: 1}, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	want := prices.StoredPage{Splits: []domain.Split{{
		Symbol: "NVDA", Date: date("2024-06-10"), From: 1, To: 10, Source: "massive", Evidence: "e1",
	}}}
	if diff := cmp.Diff(want, page); diff != "" {
		t.Errorf("DecodeStored() mismatch (-want +got):\n%s", diff)
	}
}

func TestDecodeStoredMarksTheLastPage(t *testing.T) {
	tests := []struct {
		kind prices.StoredKind
		body string
	}{
		{prices.StoredTickers, `{"status":"OK","results":[{"ticker":"KO","cik":"0000021344"}]}`},
		{prices.StoredDividends, `{"status":"OK","results":[{"id":"d1","ticker":"KO","ex_dividend_date":"2026-09-15","cash_amount":0.437}]}`},
		{prices.StoredGroupedDaily, `{"status":"OK","results":[{"T":"KO","t":1761710400000,"c":70}]}`},
	}
	for _, tt := range tests {
		page, err := massive.Stored{}.DecodeStored(prices.StoredRequest{Kind: tt.kind, Date: date("2025-10-29"), Page: 1}, []byte(tt.body))
		if err != nil {
			t.Fatalf("%s: %v", tt.kind, err)
		}
		if !page.Last || len(page.Tickers)+len(page.Dividends)+len(page.Bars) != 1 {
			t.Errorf("%s: DecodeStored() = %+v, want the last page with one item", tt.kind, page)
		}
	}
}

// The client asked for bars up to To, the last finished session at the
// time of the request.
func TestDecodeStoredKeepsDailyBarsUpToTheEndOfTheRequest(t *testing.T) {
	// 2024-06-11 and 2024-06-12, midnight in New York.
	body := `{"status":"DELAYED","ticker":"NVDA","results":[{"t":1718078400000,"c":120},{"t":1718164800000,"c":125}]}`
	req := prices.StoredRequest{Kind: prices.StoredDailyBars, Symbol: "NVDA", From: date("2024-06-05"), To: date("2024-06-11"), Page: 1}
	page, err := massive.Stored{}.DecodeStored(req, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Bars) != 1 || page.Bars[0].Date != date("2024-06-11") {
		t.Errorf("DecodeStored() bars = %+v, want the bar of 2024-06-11 only", page.Bars)
	}
}

func TestDecodeStoredGivesTheErrorOfAnErrorPage(t *testing.T) {
	_, err := massive.Stored{}.DecodeStored(prices.StoredRequest{Kind: prices.StoredGroupedDaily, Page: 1},
		[]byte(`{"status":"NOT_AUTHORIZED","message":"upgrade"}`))
	if !errors.Is(err, prices.ErrNotAuthorized) {
		t.Errorf("DecodeStored() error = %v, want ErrNotAuthorized", err)
	}
	if _, err := (massive.Stored{}).DecodeStored(prices.StoredRequest{Kind: "quotes", Page: 1}, []byte(`{"status":"OK"}`)); err == nil {
		t.Error("DecodeStored() of an unknown kind error = nil, want an error")
	}
}
