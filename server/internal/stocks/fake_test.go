package stocks_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/prices/massive"
	"github.com/sourcehawk/pricedin/server/internal/sec"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

// memStore implements stocks.Store over slices, with the semantics of the
// store package that the service relies on.
type memStore struct {
	companies []domain.Company
	tickers   []store.TickerRecord
	facts     []domain.Fact
	filings   []domain.Filing
	loaded    map[domain.CIK]bool
	bars      []domain.PriceBar
	splits    []domain.Split
	dividends []domain.Dividend
	runs      []store.IngestRun
	coverage  store.Coverage
}

func (m *memStore) GetCurrentTicker(_ context.Context, symbol string) (domain.Ticker, error) {
	for _, t := range m.tickers {
		if t.Symbol == strings.ToUpper(symbol) && t.ValidTo == nil {
			return t.Ticker, nil
		}
	}
	return domain.Ticker{}, fmt.Errorf("get ticker: %w", store.ErrNotFound)
}

func (m *memStore) GetCompany(_ context.Context, cik domain.CIK) (domain.Company, error) {
	for _, c := range m.companies {
		if c.CIK == cik {
			return c, nil
		}
	}
	return domain.Company{}, store.ErrNotFound
}

func (m *memStore) HasQuarterlyFundamentals(_ context.Context, cik domain.CIK) (bool, error) {
	return m.loaded[cik], nil
}

func (m *memStore) ListSymbolTickers(_ context.Context, symbol string) ([]store.TickerRecord, error) {
	var out []store.TickerRecord
	for _, t := range m.tickers {
		if t.Symbol == strings.ToUpper(symbol) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *memStore) ListFacts(_ context.Context, cik domain.CIK, _ store.FactQuery) ([]domain.Fact, error) {
	var out []domain.Fact
	for _, f := range m.facts {
		if f.CIK == cik {
			out = append(out, f)
		}
	}
	return out, nil
}

func (m *memStore) GetLatestFiled(_ context.Context, cik domain.CIK) (domain.Date, error) {
	var latest domain.Date
	for _, f := range m.facts {
		if f.CIK == cik && f.Filed.After(latest) {
			latest = f.Filed
		}
	}
	if latest.IsZero() {
		return domain.Date{}, store.ErrNotFound
	}
	return latest, nil
}

func (m *memStore) ListFilings(_ context.Context, cik domain.CIK) ([]domain.Filing, error) {
	var out []domain.Filing
	for _, f := range m.filings {
		if f.CIK == cik {
			out = append(out, f)
		}
	}
	return out, nil
}

func (m *memStore) ListPriceBars(_ context.Context, symbol string, from, to domain.Date) ([]domain.PriceBar, error) {
	var out []domain.PriceBar
	for _, b := range m.bars {
		if b.Symbol == symbol && !b.Date.Before(from) && !b.Date.After(to) {
			out = append(out, b)
		}
	}
	slices.SortFunc(out, func(a, b domain.PriceBar) int { return a.Date.Time().Compare(b.Date.Time()) })
	return out, nil
}

func (m *memStore) ListPriceSplits(_ context.Context, symbol string) ([]domain.Split, error) {
	var out []domain.Split
	for _, s := range m.splits {
		if s.Symbol == symbol {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *memStore) ListDividends(_ context.Context, symbol string) ([]domain.Dividend, error) {
	var out []domain.Dividend
	for _, d := range m.dividends {
		if d.Symbol == symbol {
			out = append(out, d)
		}
	}
	return out, nil
}

func (m *memStore) SearchTickers(_ context.Context, query string, limit int) ([]store.TickerMatch, error) {
	out := []store.TickerMatch{}
	for _, t := range m.tickers {
		if t.ValidTo != nil || !strings.HasPrefix(t.Symbol, strings.ToUpper(query)) || len(out) == limit {
			continue
		}
		c, _ := m.GetCompany(context.Background(), t.CIK)
		out = append(out, store.TickerMatch{Ticker: t.Ticker, Name: c.Name, Loaded: m.loaded[t.CIK]})
	}
	return out, nil
}

func (m *memStore) ListLatestIngestRuns(context.Context) ([]store.IngestRun, error) {
	return m.runs, nil
}

func (m *memStore) GetLatestPriceDateOverall(context.Context) (domain.Date, error) {
	var latest domain.Date
	for _, b := range m.bars {
		if b.Date.After(latest) {
			latest = b.Date
		}
	}
	if latest.IsZero() {
		return domain.Date{}, store.ErrNotFound
	}
	return latest, nil
}

func (m *memStore) GetCoverage(context.Context) (store.Coverage, error) {
	return m.coverage, nil
}

// now is the clock of the tests: the evening after the last fixture
// session (2026-10-29), New York time.
var now = time.Date(2026, 10, 30, 22, 0, 0, 0, time.UTC)

const (
	cikNVDA domain.CIK = 1045810
	cikKO   domain.CIK = 21344
	cikMSFT domain.CIK = 789019
)

func testdata(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("../../../testdata", filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// fixtureStore holds NVDA and KO as the ingestion jobs would load them,
// and MSFT with a ticker and a company but no data.
func fixtureStore(t *testing.T) *memStore {
	t.Helper()
	m := &memStore{loaded: map[domain.CIK]bool{}}
	entries, err := sec.ParseTickers(testdata(t, "sec/www.sec.gov/files/company_tickers_exchange.json"), domain.NewDate(2026, 10, 1))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Ticker.CIK == cikNVDA || e.Ticker.CIK == cikKO || e.Ticker.CIK == cikMSFT {
			m.tickers = append(m.tickers, store.TickerRecord{Ticker: e.Ticker, Source: store.TickerSourceSEC})
		}
	}
	m.companies = append(m.companies, domain.Company{CIK: cikMSFT, Name: "MICROSOFT CORP", SIC: "7372", FiscalYearEnd: "0630"})
	for _, c := range []domain.CIK{cikNVDA, cikKO} {
		sub, err := sec.ParseSubmissions(testdata(t, "sec/data.sec.gov/submissions/CIK"+c.String()+".json"))
		if err != nil {
			t.Fatal(err)
		}
		m.companies = append(m.companies, sub.Company)
		m.filings = append(m.filings, sub.Filings...)
		for _, name := range sub.Files {
			older, err := sec.ParseSubmissionsFile(c, testdata(t, "sec/data.sec.gov/submissions/"+name))
			if err != nil {
				t.Fatal(err)
			}
			m.filings = append(m.filings, older...)
		}
		facts, err := sec.ParseCompanyFacts(testdata(t, "sec/data.sec.gov/api/xbrl/companyfacts/CIK"+c.String()+".json"))
		if err != nil {
			t.Fatal(err)
		}
		m.facts = append(m.facts, facts...)
		m.loaded[c] = true
	}
	for _, sym := range []string{"NVDA", "KO"} {
		bars, err := massive.ParseDailyBars(testdata(t, "prices/massive/aggs/"+sym+".json"))
		if err != nil {
			t.Fatal(err)
		}
		m.bars = append(m.bars, bars.Items...)
		splits, err := massive.ParseSplits(testdata(t, "prices/massive/splits/"+sym+".json"))
		if err != nil {
			t.Fatal(err)
		}
		m.splits = append(m.splits, splits.Items...)
		divs, err := massive.ParseDividends(testdata(t, "prices/massive/dividends/"+sym+".json"))
		if err != nil {
			t.Fatal(err)
		}
		m.dividends = append(m.dividends, divs.Items...)
	}
	return m
}

func date(t *testing.T, s string) domain.Date {
	t.Helper()
	d, err := domain.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// fakeLoader records the symbols it was asked to load.
type fakeLoader struct {
	symbols []string
	err     error
}

func (f *fakeLoader) LoadCompany(_ context.Context, symbol string) error {
	f.symbols = append(f.symbols, symbol)
	return f.err
}
