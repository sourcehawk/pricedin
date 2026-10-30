// Package stockstest gives tests of the stocks service and the HTTP API an
// in-memory store filled from the repository's SEC and price fixtures, and
// a fake CompanyLoader.
package stockstest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/prices/massive"
	"github.com/sourcehawk/pricedin/server/internal/sec"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

// Store implements stocks.Store over slices, with the semantics of the
// store package that the service relies on. Tests may change its fields
// before they use it.
type Store struct {
	Companies []domain.Company
	Tickers   []store.TickerRecord
	Facts     []domain.Fact
	Filings   []domain.Filing
	// Loaded holds the CIKs that have quarterly fundamentals.
	Loaded    map[domain.CIK]bool
	Bars      []domain.PriceBar
	Splits    []domain.Split
	Dividends []domain.Dividend
	Runs      []store.IngestRun
	Coverage  store.Coverage
	// DividendsLoadedAt is the time each symbol's dividends were stored.
	DividendsLoadedAt map[string]time.Time
}

// GetCurrentTicker returns the open row of symbol, or store.ErrNotFound.
func (m *Store) GetCurrentTicker(_ context.Context, symbol string) (domain.Ticker, error) {
	for _, t := range m.Tickers {
		if strings.EqualFold(t.Symbol, symbol) && t.ValidTo == nil {
			return t.Ticker, nil
		}
	}
	return domain.Ticker{}, fmt.Errorf("get ticker: %w", store.ErrNotFound)
}

// GetCompany returns the company, or store.ErrNotFound.
func (m *Store) GetCompany(_ context.Context, cik domain.CIK) (domain.Company, error) {
	for _, c := range m.Companies {
		if c.CIK == cik {
			return c, nil
		}
	}
	return domain.Company{}, store.ErrNotFound
}

// HasQuarterlyFundamentals reports whether Loaded holds the CIK.
func (m *Store) HasQuarterlyFundamentals(_ context.Context, cik domain.CIK) (bool, error) {
	return m.Loaded[cik], nil
}

// ListSymbolTickers returns every row of symbol.
func (m *Store) ListSymbolTickers(_ context.Context, symbol string) ([]store.TickerRecord, error) {
	var out []store.TickerRecord
	for _, t := range m.Tickers {
		if strings.EqualFold(t.Symbol, symbol) {
			out = append(out, t)
		}
	}
	return out, nil
}

// ListFacts returns every fact of the CIK; it ignores the query.
func (m *Store) ListFacts(_ context.Context, cik domain.CIK, _ store.FactQuery) ([]domain.Fact, error) {
	var out []domain.Fact
	for i := range m.Facts {
		if m.Facts[i].CIK == cik {
			out = append(out, m.Facts[i])
		}
	}
	return out, nil
}

// GetLatestFiled returns the newest filing date of the CIK's facts, or store.ErrNotFound.
func (m *Store) GetLatestFiled(_ context.Context, cik domain.CIK) (domain.Date, error) {
	var latest domain.Date
	for i := range m.Facts {
		if f := &m.Facts[i]; f.CIK == cik && f.Filed.After(latest) {
			latest = f.Filed
		}
	}
	if latest.IsZero() {
		return domain.Date{}, store.ErrNotFound
	}
	return latest, nil
}

// ListFilings returns the filings of the CIK.
func (m *Store) ListFilings(_ context.Context, cik domain.CIK) ([]domain.Filing, error) {
	var out []domain.Filing
	for _, f := range m.Filings {
		if f.CIK == cik {
			out = append(out, f)
		}
	}
	return out, nil
}

// ListPriceBars returns the bars of symbol from from to to, oldest first.
func (m *Store) ListPriceBars(_ context.Context, symbol string, from, to domain.Date) ([]domain.PriceBar, error) {
	var out []domain.PriceBar
	for _, b := range m.Bars {
		if b.Symbol == symbol && !b.Date.Before(from) && !b.Date.After(to) {
			out = append(out, b)
		}
	}
	slices.SortFunc(out, func(a, b domain.PriceBar) int { return a.Date.Time().Compare(b.Date.Time()) })
	return out, nil
}

// ListPriceSplits returns the splits of symbol.
func (m *Store) ListPriceSplits(_ context.Context, symbol string) ([]domain.Split, error) {
	var out []domain.Split
	for _, s := range m.Splits {
		if s.Symbol == symbol {
			out = append(out, s)
		}
	}
	return out, nil
}

// ListDividends returns the dividends of symbol.
func (m *Store) ListDividends(_ context.Context, symbol string) ([]domain.Dividend, error) {
	var out []domain.Dividend
	for i := range m.Dividends {
		if m.Dividends[i].Symbol == symbol {
			out = append(out, m.Dividends[i])
		}
	}
	return out, nil
}

// SearchTickers returns the open tickers that start with query; unlike the store, it does not match company names.
func (m *Store) SearchTickers(ctx context.Context, query string, limit int) ([]store.TickerMatch, error) {
	out := []store.TickerMatch{}
	for _, t := range m.Tickers {
		if t.ValidTo != nil || !strings.HasPrefix(t.Symbol, strings.ToUpper(query)) || len(out) == limit {
			continue
		}
		c, _ := m.GetCompany(ctx, t.CIK)
		out = append(out, store.TickerMatch{Ticker: t.Ticker, Name: c.Name, Loaded: m.Loaded[t.CIK]})
	}
	return out, nil
}

// ListLatestIngestRuns returns Runs.
func (m *Store) ListLatestIngestRuns(context.Context) ([]store.IngestRun, error) {
	return m.Runs, nil
}

// GetLatestPriceDateOverall returns the newest bar date, or store.ErrNotFound.
func (m *Store) GetLatestPriceDateOverall(context.Context) (domain.Date, error) {
	var latest domain.Date
	for _, b := range m.Bars {
		if b.Date.After(latest) {
			latest = b.Date
		}
	}
	if latest.IsZero() {
		return domain.Date{}, store.ErrNotFound
	}
	return latest, nil
}

// GetDividendsLoadedAt returns DividendsLoadedAt of symbol, or
// store.ErrNotFound.
func (m *Store) GetDividendsLoadedAt(_ context.Context, symbol string) (time.Time, error) {
	at, ok := m.DividendsLoadedAt[strings.ToUpper(symbol)]
	if !ok {
		return time.Time{}, store.ErrNotFound
	}
	return at, nil
}

// GetLatestSucceededIngestRun returns the succeeded run of job in Runs
// that finished last, or store.ErrNotFound.
func (m *Store) GetLatestSucceededIngestRun(_ context.Context, job string) (store.IngestRun, error) {
	var out *store.IngestRun
	for i := range m.Runs {
		r := &m.Runs[i]
		if r.Job == job && r.Status == store.RunSucceeded && r.FinishedAt != nil && (out == nil || r.FinishedAt.After(*out.FinishedAt)) {
			out = r
		}
	}
	if out == nil {
		return store.IngestRun{}, store.ErrNotFound
	}
	return *out, nil
}

// GetCoverage returns Coverage.
func (m *Store) GetCoverage(context.Context) (store.Coverage, error) {
	return m.Coverage, nil
}

// Now is the clock of tests on the fixtures: the evening after the last
// fixture session (2026-10-29), New York time.
var Now = time.Date(2026, 10, 30, 22, 0, 0, 0, time.UTC)

// The CIKs of the fixture companies.
const (
	CIKNVDA domain.CIK = 1045810
	CIKKO   domain.CIK = 21344
	CIKMSFT domain.CIK = 789019
)

// Testdata reads a file below the repository's testdata folder.
func Testdata(t testing.TB, path string) []byte {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "testdata")
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path))) //nolint:gosec // G304: test data below the repository root
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// Fixture holds NVDA and KO as the ingestion jobs would load them, and
// MSFT with a ticker and a company but no data.
func Fixture(t testing.TB) *Store {
	t.Helper()
	m := &Store{Loaded: map[domain.CIK]bool{}, DividendsLoadedAt: map[string]time.Time{}}
	entries, err := sec.ParseTickers(Testdata(t, "sec/www.sec.gov/files/company_tickers_exchange.json"), domain.NewDate(2026, 10, 1))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Ticker.CIK == CIKNVDA || e.Ticker.CIK == CIKKO || e.Ticker.CIK == CIKMSFT {
			m.Tickers = append(m.Tickers, store.TickerRecord{Ticker: e.Ticker, Source: store.TickerSourceSEC})
		}
	}
	m.Companies = append(m.Companies, domain.Company{CIK: CIKMSFT, Name: "MICROSOFT CORP", SIC: "7372", FiscalYearEnd: "0630"})
	for _, c := range []domain.CIK{CIKNVDA, CIKKO} {
		sub, err := sec.ParseSubmissions(Testdata(t, "sec/data.sec.gov/submissions/CIK"+c.String()+".json"))
		if err != nil {
			t.Fatal(err)
		}
		m.Companies = append(m.Companies, sub.Company)
		m.Filings = append(m.Filings, sub.Filings...)
		for _, name := range sub.Files {
			older, err := sec.ParseSubmissionsFile(c, Testdata(t, "sec/data.sec.gov/submissions/"+name))
			if err != nil {
				t.Fatal(err)
			}
			m.Filings = append(m.Filings, older...)
		}
		facts, err := sec.ParseCompanyFacts(Testdata(t, "sec/data.sec.gov/api/xbrl/companyfacts/CIK"+c.String()+".json"))
		if err != nil {
			t.Fatal(err)
		}
		m.Facts = append(m.Facts, facts...)
		m.Loaded[c] = true
	}
	for _, sym := range []string{"NVDA", "KO"} {
		bars, err := massive.ParseDailyBars(Testdata(t, "prices/massive/aggs/"+sym+".json"))
		if err != nil {
			t.Fatal(err)
		}
		m.Bars = append(m.Bars, bars.Items...)
		splits, err := massive.ParseSplits(Testdata(t, "prices/massive/splits/"+sym+".json"))
		if err != nil {
			t.Fatal(err)
		}
		m.Splits = append(m.Splits, splits.Items...)
		divs, err := massive.ParseDividends(Testdata(t, "prices/massive/dividends/"+sym+".json"))
		if err != nil {
			t.Fatal(err)
		}
		m.Dividends = append(m.Dividends, divs.Items...)
		// As in the store, only stored dividends have a load time. Seed
		// gives the stored rows the same one.
		if len(divs.Items) > 0 {
			m.DividendsLoadedAt[sym] = Now
		}
	}
	return m
}

// Loader is a stocks.CompanyLoader that records the symbols it was asked
// to load and returns Err.
type Loader struct {
	mu      sync.Mutex
	Symbols []string
	Err     error
}

// LoadCompany records symbol and returns l.Err.
func (l *Loader) LoadCompany(_ context.Context, symbol string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Symbols = append(l.Symbols, symbol)
	return l.Err
}

// SetQuarterValue sets the value of every three-month fact of the CIK and
// concept that ends on periodEnd, in every filing that reports it.
func (m *Store) SetQuarterValue(t testing.TB, cik domain.CIK, concept string, periodEnd domain.Date, value float64) {
	t.Helper()
	n := 0
	for i := range m.Facts {
		f := &m.Facts[i]
		if f.CIK != cik || f.Concept != concept || f.PeriodEnd != periodEnd || f.PeriodStart == nil {
			continue
		}
		if days := f.PeriodEnd.Time().Sub(f.PeriodStart.Time()).Hours() / 24; days < 80 || days > 100 {
			continue
		}
		f.Value = value
		n++
	}
	if n == 0 {
		t.Fatalf("no three-month %s fact of %s ends on %s", concept, cik, periodEnd)
	}
}
