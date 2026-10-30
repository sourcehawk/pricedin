package sec_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/sec"
)

func TestParseTickersReadsEveryRow(t *testing.T) {
	retrieved := date(t, "2026-10-30")
	got, err := sec.ParseTickers(readFixture(t, "www.sec.gov/files/company_tickers_exchange.json"), retrieved)
	if err != nil {
		t.Fatalf("ParseTickers() error = %v", err)
	}
	if len(got) != 13 {
		t.Fatalf("len = %d, want 13", len(got))
	}
	byTicker := map[string]sec.TickerEntry{}
	for _, e := range got {
		byTicker[e.Ticker.Symbol] = e
	}
	tests := []sec.TickerEntry{
		{Name: "NVIDIA CORP", Ticker: domain.Ticker{Symbol: "NVDA", CIK: cikNVDA, Exchange: "Nasdaq", ValidFrom: retrieved}},
		{Name: "GARMIN LTD", Ticker: domain.Ticker{Symbol: "GRMN", CIK: cikGRMN, Exchange: "NYSE", ValidFrom: retrieved}},
		{Name: "Alphabet Inc.", Ticker: domain.Ticker{Symbol: "GOOG", CIK: 1652044, Exchange: "Nasdaq", ValidFrom: retrieved}},
		{Name: "Alphabet Inc.", Ticker: domain.Ticker{Symbol: "GOOGL", CIK: 1652044, Exchange: "Nasdaq", ValidFrom: retrieved}},
		{Name: "Energy Transfer LP", Ticker: domain.Ticker{Symbol: "ET", CIK: 1276187, Exchange: "TXSE", ValidFrom: retrieved}},
		// SEC lists some tickers with a null exchange.
		{Name: "InoBat B.V.", Ticker: domain.Ticker{Symbol: "INBT", CIK: 2156068, Exchange: "", ValidFrom: retrieved}},
	}
	for _, want := range tests {
		if diff := cmp.Diff(want, byTicker[want.Ticker.Symbol]); diff != "" {
			t.Errorf("%s mismatch (-want +got):\n%s", want.Ticker.Symbol, diff)
		}
	}
}

func TestParseTickersFindsColumnsByName(t *testing.T) {
	body := `{"fields":["exchange","ticker","name","cik"],"data":[["NYSE","KO","COCA COLA CO",21344]]}`
	got, err := sec.ParseTickers([]byte(body), date(t, "2026-10-30"))
	if err != nil {
		t.Fatalf("ParseTickers() error = %v", err)
	}
	want := []sec.TickerEntry{{Name: "COCA COLA CO", Ticker: domain.Ticker{Symbol: "KO", CIK: cikKO, Exchange: "NYSE", ValidFrom: date(t, "2026-10-30")}}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}

func TestParseTickersRejectsInvalidInput(t *testing.T) {
	tests := map[string]string{
		"not json":       `{"fields":`,
		"missing field":  `{"fields":["cik","name","ticker"],"data":[]}`,
		"short row":      `{"fields":["cik","name","ticker","exchange"],"data":[[1,"A","A"]]}`,
		"zero cik":       `{"fields":["cik","name","ticker","exchange"],"data":[[0,"A","A","NYSE"]]}`,
		"empty ticker":   `{"fields":["cik","name","ticker","exchange"],"data":[[1,"A","","NYSE"]]}`,
		"cik as text":    `{"fields":["cik","name","ticker","exchange"],"data":[["x","A","A","NYSE"]]}`,
		"name as number": `{"fields":["cik","name","ticker","exchange"],"data":[[1,2,"A","NYSE"]]}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := sec.ParseTickers([]byte(body), date(t, "2026-10-30")); err == nil {
				t.Error("ParseTickers() error = nil, want an error")
			}
		})
	}
}
