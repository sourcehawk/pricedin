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

func datePtr(s string) *domain.Date {
	d := date(s)
	return &d
}

func TestParseDailyBarsTakesTheSymbolFromTheResponse(t *testing.T) {
	page, err := massive.ParseDailyBars([]byte(`{"ticker":"KO","status":"OK","adjusted":false,
		"results":[{"v":12000000,"vw":62.1,"o":62,"c":62.5,"h":63,"l":61.5,"t":1718078400000,"n":90000}],
		"next_url":"https://api.massive.com/v2/aggs/ticker/KO/range/1/day/1718078400000/2024-06-30?cursor=abc"}`))
	if err != nil {
		t.Fatalf("ParseDailyBars() error = %v", err)
	}
	want := massive.Page[domain.PriceBar]{
		Items:   []domain.PriceBar{{Symbol: "KO", Date: date("2024-06-11"), Open: 62, High: 63, Low: 61.5, Close: 62.5, Volume: 12000000, Source: "massive"}},
		NextURL: "https://api.massive.com/v2/aggs/ticker/KO/range/1/day/1718078400000/2024-06-30?cursor=abc",
	}
	if diff := cmp.Diff(want, page); diff != "" {
		t.Errorf("ParseDailyBars() mismatch (-want +got):\n%s", diff)
	}
}

func TestParseSplits(t *testing.T) {
	page, err := massive.ParseSplits([]byte(`{"status":"OK","request_id":"r1","results":[
		{"id":"Eabc","execution_date":"2024-06-10","split_from":1.0,"split_to":10.0,"ticker":"NVDA","adjustment_type":"forward_split","historical_adjustment_factor":0.1},
		{"id":"Edef","execution_date":"2023-01-05","split_from":20,"split_to":1,"ticker":"XYZ","adjustment_type":"reverse_split"}]}`))
	if err != nil {
		t.Fatalf("ParseSplits() error = %v", err)
	}
	want := []domain.Split{
		{Symbol: "NVDA", Date: date("2024-06-10"), From: 1, To: 10, Source: "massive", Evidence: "Eabc"},
		{Symbol: "XYZ", Date: date("2023-01-05"), From: 20, To: 1, Source: "massive", Evidence: "Edef"},
	}
	if diff := cmp.Diff(want, page.Items); diff != "" {
		t.Errorf("ParseSplits() mismatch (-want +got):\n%s", diff)
	}
}

func TestParseDividends(t *testing.T) {
	page, err := massive.ParseDividends([]byte(`{"status":"OK","request_id":"r1","results":[
		{"id":"E1","ticker":"KO","record_date":"2024-06-14","pay_date":"2024-07-01","declaration_date":"2024-04-24","ex_dividend_date":"2024-06-14",
		 "frequency":4,"cash_amount":0.485,"currency":"USD","distribution_type":"recurring","historical_adjustment_factor":0.99,"split_adjusted_cash_amount":0.485},
		{"id":"E2","ticker":"KO","ex_dividend_date":"2027-03-12","frequency":0,"cash_amount":1.5,"currency":"USD","distribution_type":"special"}]}`))
	if err != nil {
		t.Fatalf("ParseDividends() error = %v", err)
	}
	want := []domain.Dividend{
		{
			Symbol: "KO", ExDate: date("2024-06-14"), PayDate: datePtr("2024-07-01"), DeclarationDate: datePtr("2024-04-24"),
			CashAmount: 0.485, Currency: "USD", Frequency: 4, Type: "recurring", Source: "massive", ProviderID: "E1",
		},
		{Symbol: "KO", ExDate: date("2027-03-12"), CashAmount: 1.5, Currency: "USD", Type: "special", Source: "massive", ProviderID: "E2"},
	}
	if diff := cmp.Diff(want, page.Items); diff != "" {
		t.Errorf("ParseDividends() mismatch (-want +got):\n%s", diff)
	}
}

func TestParseTickers(t *testing.T) {
	page, err := massive.ParseTickers([]byte(`{"count":2,"status":"OK","request_id":"r1","results":[
		{"ticker":"GRMN","name":"Garmin Ltd","market":"stocks","locale":"us","primary_exchange":"XNYS","type":"CS","active":true,
		 "currency_name":"usd","cik":"0001121788","composite_figi":"BBG000TESTA1","share_class_figi":"BBG001TESTA2","last_updated_utc":"2026-10-29T06:09:01Z"},
		{"ticker":"SPYX","name":"A Fund","market":"stocks","locale":"us","primary_exchange":"ARCX","type":"ETF","active":true}],
		"next_url":"https://api.massive.com/v3/reference/tickers?cursor=abc"}`))
	if err != nil {
		t.Fatalf("ParseTickers() error = %v", err)
	}
	want := massive.Page[prices.ProviderTicker]{
		Items: []prices.ProviderTicker{
			{Symbol: "GRMN", Name: "Garmin Ltd", CIK: 1121788, Exchange: "XNYS", Type: "CS", CompositeFIGI: "BBG000TESTA1", Source: "massive"},
			{Symbol: "SPYX", Name: "A Fund", Exchange: "ARCX", Type: "ETF", Source: "massive"},
		},
		NextURL: "https://api.massive.com/v3/reference/tickers?cursor=abc",
	}
	if diff := cmp.Diff(want, page); diff != "" {
		t.Errorf("ParseTickers() mismatch (-want +got):\n%s", diff)
	}
}

func TestParseTickerOverview(t *testing.T) {
	got, err := massive.ParseTickerOverview([]byte(`{"request_id":"r1","status":"OK","results":{"ticker":"GRMN","name":"Garmin Ltd",
		"market":"stocks","primary_exchange":"XNYS","type":"CS","active":true,"cik":"0001121788","market_cap":1,"address":{"city":"X"}}}`))
	if err != nil {
		t.Fatalf("ParseTickerOverview() error = %v", err)
	}
	want := prices.ProviderTicker{Symbol: "GRMN", Name: "Garmin Ltd", CIK: 1121788, Exchange: "XNYS", Type: "CS", Source: "massive"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ParseTickerOverview() mismatch (-want +got):\n%s", diff)
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	tests := []struct {
		name  string
		parse func([]byte) error
		body  string
	}{
		{"not JSON", parseErr(massive.ParseGroupedDaily), `<html>`},
		{"grouped bar without a ticker", parseErr(massive.ParseGroupedDaily), `{"status":"OK","results":[{"c":1,"t":1718078400000}]}`},
		{"bar without a timestamp", parseErr(massive.ParseDailyBars), `{"ticker":"KO","status":"OK","results":[{"c":1}]}`},
		{"error status", parseErr(massive.ParseSplits), `{"status":"ERROR","error":"bad"}`},
		{"unknown status", parseErr(massive.ParseSplits), `{"status":"MAYBE","results":[]}`},
		{"split without a date", parseErr(massive.ParseSplits), `{"status":"OK","results":[{"id":"a","ticker":"NVDA","split_from":1,"split_to":10}]}`},
		{"dividend with a bad ex-date", parseErr(massive.ParseDividends), `{"status":"OK","results":[{"id":"a","ticker":"KO","ex_dividend_date":"2024-02-30"}]}`},
		{"dividend with a bad pay date", parseErr(massive.ParseDividends), `{"status":"OK","results":[{"id":"a","ticker":"KO","ex_dividend_date":"2024-02-01","pay_date":"soon"}]}`},
		{"ticker with a bad CIK", parseErr(massive.ParseTickers), `{"status":"OK","results":[{"ticker":"GRMN","cik":"CIK1121788"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.parse([]byte(tt.body)); err == nil {
				t.Error("error = nil, want an error")
			}
		})
	}
}

func parseErr[T any](parse func([]byte) (massive.Page[T], error)) func([]byte) error {
	return func(body []byte) error {
		_, err := parse(body)
		return err
	}
}

func TestParseOfANotAuthorizedBodyWrapsErrNotAuthorized(t *testing.T) {
	_, err := massive.ParseGroupedDaily([]byte(notAuthorizedBody))
	if !errors.Is(err, prices.ErrNotAuthorized) {
		t.Errorf("ParseGroupedDaily() error = %v, want ErrNotAuthorized", err)
	}
}

func TestSplitsAndDividendsComeSortedByDate(t *testing.T) {
	var mux http.ServeMux
	mux.HandleFunc("/stocks/v1/splits", serveJSON(`{"status":"OK","results":[
		{"id":"b","execution_date":"2024-06-10","split_from":1,"split_to":10,"ticker":"NVDA"},
		{"id":"a","execution_date":"2021-07-20","split_from":1,"split_to":4,"ticker":"NVDA"}]}`))
	mux.HandleFunc("/stocks/v1/dividends", serveJSON(`{"status":"OK","results":[
		{"id":"b","ticker":"KO","ex_dividend_date":"2019-06-13","cash_amount":0.4},
		{"id":"a","ticker":"KO","ex_dividend_date":"2015-03-12","cash_amount":0.33}]}`))
	srv := httptest.NewServer(&mux)
	defer srv.Close()
	c := newClient(t, srv, nil)

	splits, err := c.Splits(t.Context(), "NVDA")
	if err != nil {
		t.Fatalf("Splits() error = %v", err)
	}
	if len(splits) != 2 || splits[0].Evidence != "a" {
		t.Errorf("Splits() = %+v, want the 2021 split first", splits)
	}
	divs, err := c.Dividends(t.Context(), "KO")
	if err != nil {
		t.Fatalf("Dividends() error = %v", err)
	}
	if len(divs) != 2 || divs[0].ProviderID != "a" {
		t.Errorf("Dividends() = %+v, want the 2015 dividend first", divs)
	}
}

func TestTickersRequestsActiveStocksOnEveryPage(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if r.URL.Path != "/v3/reference/tickers" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("cursor") == "" {
			serveJSON(`{"status":"OK","results":[{"ticker":"AAPL","cik":"0000320193"}],"next_url":"https://api.massive.com/v3/reference/tickers?cursor=p2"}`)(w, r)
			return
		}
		serveJSON(`{"status":"OK","results":[{"ticker":"KO","cik":"0000021344"}]}`)(w, r)
	}))
	defer srv.Close()

	got, err := newClient(t, srv, nil).Tickers(t.Context())
	if err != nil {
		t.Fatalf("Tickers() error = %v", err)
	}
	if len(got) != 2 || got[0].Symbol != "AAPL" || got[1].CIK != 21344 {
		t.Errorf("Tickers() = %+v, want AAPL and KO", got)
	}
	want := []string{"active=true&limit=1000&market=stocks&order=asc&sort=ticker", "cursor=p2"}
	if diff := cmp.Diff(want, queries); diff != "" {
		t.Errorf("queries mismatch (-want +got):\n%s", diff)
	}
}

func TestTickerReturnsOneTicker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/reference/tickers/GRMN" {
			serveStatus(http.StatusNotFound, `{"status":"NOT_FOUND","message":"Ticker not found."}`)(w, r)
			return
		}
		serveJSON(`{"status":"OK","results":{"ticker":"GRMN","cik":"0001121788"}}`)(w, r)
	}))
	defer srv.Close()
	c := newClient(t, srv, nil)

	got, err := c.Ticker(t.Context(), "GRMN")
	if err != nil || got.CIK != 1121788 {
		t.Errorf("Ticker(GRMN) = %+v, %v; want CIK 1121788", got, err)
	}
	_, err = c.Ticker(t.Context(), "ZZZZ")
	var apiErr *massive.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != "NOT_FOUND" {
		t.Errorf("Ticker(ZZZZ) error = %v, want an *APIError with status NOT_FOUND", err)
	}
}

func TestAdjustedDailyBarsAsksForAdjustedBars(t *testing.T) {
	var gotAdjusted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAdjusted = r.URL.Query().Get("adjusted")
		serveJSON(`{"ticker":"NVDA","status":"OK","adjusted":true,"results":[{"o":1,"h":1,"l":1,"c":115,"v":1,"t":1717732800000}]}`)(w, r)
	}))
	defer srv.Close()

	got, err := newClient(t, srv, nil).AdjustedDailyBars(t.Context(), "NVDA", date("2024-06-07"), date("2024-06-07"))
	if err != nil {
		t.Fatalf("AdjustedDailyBars() error = %v", err)
	}
	if gotAdjusted != "true" || len(got) != 1 || got[0].Close != 115 {
		t.Errorf("AdjustedDailyBars() = %+v with adjusted=%q, want one bar with adjusted=true", got, gotAdjusted)
	}
}
