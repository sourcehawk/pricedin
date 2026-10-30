package stocks_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/engine"
	"github.com/sourcehawk/pricedin/server/internal/fundamentals"
	"github.com/sourcehawk/pricedin/server/internal/stocks"
	"github.com/sourcehawk/pricedin/server/internal/stocks/stockstest"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

func newService(t *testing.T, st *stockstest.Store, loader stocks.CompanyLoader) *stocks.Service {
	t.Helper()
	return stocks.New(st, stocks.Options{Now: func() time.Time { return stockstest.Now }, Loader: loader})
}

func near(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestProfileOfALoadedStock(t *testing.T) {
	svc := newService(t, stockstest.Fixture(t), nil)
	p, err := svc.Profile(context.Background(), "nvda")
	if err != nil {
		t.Fatalf("Profile() error = %v", err)
	}
	if !p.Loaded || p.Ticker.Symbol != "NVDA" || p.Ticker.Exchange != "Nasdaq" {
		t.Errorf("Profile() ticker = %+v loaded %v, want NVDA on Nasdaq, loaded", p.Ticker, p.Loaded)
	}
	if p.Company.CIK != "0001045810" || p.Company.Name == "" || p.Company.FiscalYearEnd == "" {
		t.Errorf("Profile().Company = %+v, want NVIDIA's CIK, name and fiscal year end", p.Company)
	}
	c := p.LatestClose
	if c == nil || c.Value != 309.11 || c.AsOf != date(t, "2026-10-29") || c.AsOfLabel != "as of close" {
		t.Fatalf("LatestClose = %+v, want 309.11 as of close 2026-10-29", c)
	}
	if src, ok := c.Source.(stocks.PriceSource); !ok || src.Provider != "massive" || src.CloseDate != c.AsOf || src.Symbol != "NVDA" {
		t.Errorf("LatestClose.Source = %#v, want the massive close of 2026-10-29", c.Source)
	}
	if p.TTMEPS == nil {
		t.Fatal("TTMEPS = nil, want the TTM diluted EPS")
	}
	ttm, ok := p.TTMEPS.Source.(stocks.DerivedSource)
	if !ok || ttm.Method != "ttm" || len(ttm.Inputs) != 4 {
		t.Fatalf("TTMEPS.Source = %#v, want a ttm of four quarters", p.TTMEPS.Source)
	}
	if p.PE == nil || !near(p.PE.Value, c.Value/p.TTMEPS.Value) {
		t.Errorf("PE = %+v, want close / TTM EPS = %v", p.PE, c.Value/p.TTMEPS.Value)
	}
	if p.Freshness.LatestClose == nil || *p.Freshness.LatestClose != date(t, "2026-10-29") || p.Freshness.LatestFiled == nil {
		t.Errorf("Freshness = %+v, want the latest close and filing", p.Freshness)
	}
}

func TestProfileTTMEPSListsTheFilingsOfItsQuarters(t *testing.T) {
	svc := newService(t, stockstest.Fixture(t), nil)
	p, err := svc.Profile(context.Background(), "NVDA")
	if err != nil {
		t.Fatal(err)
	}
	ttm, _ := p.TTMEPS.Source.(stocks.DerivedSource)
	var secs []stocks.SECSource
	var collect func(stocks.Source)
	collect = func(s stocks.Source) {
		switch s := s.(type) {
		case stocks.SECSource:
			secs = append(secs, s)
		case stocks.DerivedSource:
			for _, in := range s.Inputs {
				collect(in)
			}
		default:
			t.Errorf("input %#v, want filings only", s)
		}
	}
	collect(ttm)
	if len(secs) < 4 {
		t.Fatalf("filings = %d, want at least one per quarter", len(secs))
	}
	for _, s := range secs {
		if s.Accession == "" || s.Form == "" || s.Filed.IsZero() || s.URL == "" {
			t.Errorf("input %+v, want accession, form, filing date and URL", s)
		}
	}
}

func TestProfileOfAStockWithoutData(t *testing.T) {
	svc := newService(t, stockstest.Fixture(t), nil)
	p, err := svc.Profile(context.Background(), "MSFT")
	if err != nil {
		t.Fatalf("Profile() error = %v", err)
	}
	if p.Loaded || p.TTMEPS != nil || p.PE != nil || p.LatestClose != nil || p.Freshness.LatestFiled != nil {
		t.Errorf("Profile(MSFT) = %+v, want loaded false and no values", p)
	}
	if p.Company.Name != "MICROSOFT CORP" {
		t.Errorf("Company.Name = %q, want MICROSOFT CORP", p.Company.Name)
	}
}

func TestUnknownAndInvalidTickers(t *testing.T) {
	svc := newService(t, stockstest.Fixture(t), nil)
	ctx := context.Background()
	if _, err := svc.Profile(ctx, "ZZZZ"); !errors.Is(err, stocks.ErrUnknownTicker) {
		t.Errorf("Profile(ZZZZ) error = %v, want ErrUnknownTicker", err)
	}
	for _, bad := range []string{"", "N VDA", "TOOLONGSYMBOL", "../x", ".A"} {
		if _, err := svc.Profile(ctx, bad); !errors.Is(err, stocks.ErrInvalidTicker) {
			t.Errorf("Profile(%q) error = %v, want ErrInvalidTicker", bad, err)
		}
	}
}

func TestHistorySplitAdjustsNVIDIAClosesAndEPS(t *testing.T) {
	st := stockstest.Fixture(t)
	svc := newService(t, st, nil)
	h, err := svc.History(context.Background(), "NVDA", stocks.RangeMax)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	closes := map[domain.Date]float64{}
	for _, d := range h.History.Daily {
		closes[d.Date] = d.Close
	}
	for day, want := range map[string]float64{"2024-06-07": 76.76, "2024-06-10": 78.23, "2021-11-01": 15.498, "2026-10-29": 309.11} {
		if got := closes[date(t, day)]; !near(got, want) {
			t.Errorf("close on %s = %v, want %v (split-adjusted)", day, got, want)
		}
	}

	// The quarters are the derived ones, with the provider's splits.
	series, err := deriveNVDA(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.History.Quarters) != len(series.Quarters) || len(h.Quarters) != len(series.Quarters) {
		t.Fatalf("quarters = %d (sources %d), want %d", len(h.History.Quarters), len(h.Quarters), len(series.Quarters))
	}
	for i, q := range h.History.Quarters {
		dq := series.Quarters[i]
		if q.Label != dq.Label || q.PeriodEnd != dq.PeriodEnd {
			t.Fatalf("quarter %d = %s %s, want %s %s", i, q.Label, q.PeriodEnd, dq.Label, dq.PeriodEnd)
		}
		if v, ok := dq.Values[fundamentals.EPSDiluted]; ok != (q.EPS != nil) || (ok && !near(*q.EPS, v.V)) {
			t.Errorf("%s EPS = %v, want %v", q.Label, q.EPS, v.V)
		}
		if q.Close != nil {
			src := h.Quarters[i].Close
			if src == nil || src.CloseDate.After(q.PeriodEnd) || q.PeriodEnd.Time().Sub(src.CloseDate.Time()).Hours() > 7*24 {
				t.Errorf("%s close source = %+v, want the last close on or before %s", q.Label, src, q.PeriodEnd)
			} else if !near(*q.Close, closes[src.CloseDate]) {
				t.Errorf("%s close = %v, want the adjusted close %v of %s", q.Label, *q.Close, closes[src.CloseDate], src.CloseDate)
			}
		}
	}
}

func TestHistoryDailyTTMEPSIsTheOneKnownOnEachDay(t *testing.T) {
	st := stockstest.Fixture(t)
	svc := newService(t, st, nil)
	today := date(t, "2026-10-30")
	for _, c := range []struct {
		ticker string
		cik    domain.CIK
	}{{"NVDA", stockstest.CIKNVDA}, {"KO", stockstest.CIKKO}} {
		h, err := svc.History(context.Background(), c.ticker, stocks.RangeMax)
		if err != nil {
			t.Fatal(err)
		}
		series, err := deriveAsOf(st, c.cik, c.ticker, today)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range h.History.Daily {
			_, v, ok := fundamentals.TTMKnownOn(series, fundamentals.EPSDiluted, d.Date)
			if ok != (d.TTMEPS != nil) || (ok && !near(*d.TTMEPS, v.V)) {
				t.Fatalf("%s ttmEps on %s = %v, want %v (known %v)", c.ticker, d.Date, d.TTMEPS, v.V, ok)
			}
		}
	}
}

// deriveAsOf derives a company's quarters as the service does: its own
// facts filed by asOf and its symbol's provider splits.
func deriveAsOf(m *stockstest.Store, cik domain.CIK, symbol string, asOf domain.Date) (fundamentals.Series, error) {
	var facts []domain.Fact
	for i := range m.Facts {
		if m.Facts[i].CIK == cik {
			facts = append(facts, m.Facts[i])
		}
	}
	var splits []domain.Split
	for _, sp := range m.Splits {
		if sp.Symbol == symbol {
			splits = append(splits, sp)
		}
	}
	return fundamentals.Derive(cik, facts, fundamentals.Options{AsOf: &asOf, Splits: splits, ProviderChecked: true})
}

func TestHistoryRangeCutsTheDailyClosesWithAYearBefore(t *testing.T) {
	svc := newService(t, stockstest.Fixture(t), nil)
	tests := map[stocks.HistoryRange]string{
		stocks.Range6M: "2025-04-29",
		stocks.Range1Y: "2024-10-29",
		stocks.Range3Y: "2022-10-29",
	}
	for rng, from := range tests {
		h, err := svc.History(context.Background(), "NVDA", rng)
		if err != nil {
			t.Fatal(err)
		}
		// The range starts ten days before its 12-month look-back.
		first, start := h.History.Daily[0].Date, date(t, from).AddDays(-10)
		if first.Before(start) || first.After(start.AddDays(5)) {
			t.Errorf("range %s starts on %s, want the first trading day from %s", rng, first, start)
		}
	}
}

func TestHistoryOfAStockWithoutDataIsNotLoaded(t *testing.T) {
	svc := newService(t, stockstest.Fixture(t), nil)
	if _, err := svc.History(context.Background(), "MSFT", stocks.Range3Y); !errors.Is(err, stocks.ErrNotLoaded) {
		t.Errorf("History(MSFT) error = %v, want ErrNotLoaded", err)
	}
	if _, err := svc.Fundamentals(context.Background(), "MSFT"); !errors.Is(err, stocks.ErrNotLoaded) {
		t.Errorf("Fundamentals(MSFT) error = %v, want ErrNotLoaded", err)
	}
	if _, err := svc.Evaluate(context.Background(), "MSFT", stocks.LeversInput{}); !errors.Is(err, stocks.ErrNotLoaded) {
		t.Errorf("Evaluate(MSFT) error = %v, want ErrNotLoaded", err)
	}
}

// reusedSymbolStore has the symbol ACME, which belonged to company 1 until
// 2026-03-02 and to company 2 from then on.
func reusedSymbolStore(t *testing.T) *stockstest.Store {
	t.Helper()
	until := date(t, "2026-03-02")
	m := stockstest.Fixture(t)
	m.Companies = append(m.Companies, domain.Company{CIK: 1, Name: "Old Acme"}, domain.Company{CIK: 2, Name: "New Acme"})
	m.Tickers = append(m.Tickers,
		store.TickerRecord{Ticker: domain.Ticker{Symbol: "ACME", CIK: 1, ValidFrom: date(t, "2020-01-01"), ValidTo: &until}, Source: store.TickerSourceSEC},
		store.TickerRecord{Ticker: domain.Ticker{Symbol: "ACME", CIK: 2, ValidFrom: until}, Source: store.TickerSourceSEC},
	)
	for _, b := range []struct {
		day   string
		close float64
	}{{"2026-02-27", 500}, {"2026-03-02", 20}, {"2026-03-03", 21}} {
		m.Bars = append(m.Bars, domain.PriceBar{Symbol: "ACME", Date: date(t, b.day), Close: b.close, Source: "massive"})
	}
	m.Splits = append(m.Splits, domain.Split{Symbol: "ACME", Date: date(t, "2025-06-02"), From: 1, To: 2, Source: "massive"})
	m.Dividends = append(m.Dividends, domain.Dividend{Symbol: "ACME", ExDate: date(t, "2026-01-15"), CashAmount: 9, Type: "recurring", Source: "massive", ProviderID: "x"})
	m.DividendsLoadedAt["ACME"] = stockstest.Now
	return m
}

func TestProfileUsesOnlyTheBarsAndDividendsOfTheCurrentIssuer(t *testing.T) {
	svc := newService(t, reusedSymbolStore(t), nil)
	p, err := svc.Profile(context.Background(), "ACME")
	if err != nil {
		t.Fatal(err)
	}
	if p.LatestClose == nil || p.LatestClose.Value != 21 {
		t.Errorf("LatestClose = %+v, want 21", p.LatestClose)
	}
	if p.TTMDividend.Number == nil || p.TTMDividend.Number.Value != 0 {
		t.Errorf("TTMDividend = %+v, want 0: the dividend belongs to the old issuer", p.TTMDividend)
	}
}

func TestBarsOfTheOldIssuerAreNotInTheHistory(t *testing.T) {
	m := reusedSymbolStore(t)
	m.Loaded[2] = true
	m.Facts = append(m.Facts, nvdaFactsAs(t, m, 2)...)
	svc := newService(t, m, nil)
	h, err := svc.History(context.Background(), "ACME", stocks.RangeMax)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.History.Daily) != 2 || h.History.Daily[0].Date != date(t, "2026-03-02") || h.History.Daily[0].Close != 20 {
		t.Errorf("daily = %+v, want only the bars from 2026-03-02, unadjusted by the old split", h.History.Daily)
	}
}

// nvdaFactsAs copies NVIDIA's facts to another CIK.
func nvdaFactsAs(t *testing.T, m *stockstest.Store, cik domain.CIK) []domain.Fact {
	t.Helper()
	var out []domain.Fact
	for i := range m.Facts {
		if f := m.Facts[i]; f.CIK == stockstest.CIKNVDA {
			f.CIK = cik
			out = append(out, f)
		}
	}
	return out
}

func TestTTMDividendCountsRecurringDividendsOfTheLastTwelveMonths(t *testing.T) {
	svc := newService(t, stockstest.Fixture(t), nil)
	p, err := svc.Profile(context.Background(), "KO")
	if err != nil {
		t.Fatal(err)
	}
	d := p.TTMDividend.Number
	if d == nil || !near(d.Value, 0.5+0.52+0.52+0.52) {
		t.Fatalf("TTMDividend = %+v, want 2.06 (ex-dates 2025-12-01 to 2026-09-14; not the future 2026-11-30)", d)
	}
	src, ok := d.Source.(stocks.DerivedSource)
	if !ok || src.Method != "recurring_dividends_12m" || len(src.Inputs) != 4 {
		t.Fatalf("TTMDividend.Source = %#v, want four dividends", d.Source)
	}
	if first, _ := src.Inputs[0].(stocks.DividendSource); first.ExDate != date(t, "2025-12-01") || first.Provider != "massive" {
		t.Errorf("first input = %+v, want the massive dividend of 2025-12-01", first)
	}
	if want := fetchDate(t, stockstest.Fixture(t).DividendsLoadedAt["KO"]); d.AsOf != want {
		t.Errorf("TTMDividend.AsOf = %s, want the day the dividends were fetched, %s", d.AsOf, want)
	}
}

func TestTTMDividendLeavesOutSpecialDividendsAndAdjustsForSplits(t *testing.T) {
	m := stockstest.Fixture(t)
	m.Dividends = []domain.Dividend{
		{Symbol: "KO", ExDate: date(t, "2026-01-15"), CashAmount: 1, Type: "recurring", Source: "massive", ProviderID: "a"},
		{Symbol: "KO", ExDate: date(t, "2026-02-15"), CashAmount: 5, Type: "special", Source: "massive", ProviderID: "b"},
		{Symbol: "KO", ExDate: date(t, "2026-05-15"), CashAmount: 0.5, Type: "recurring", Source: "massive", ProviderID: "c"},
	}
	m.Splits = append(m.Splits, domain.Split{Symbol: "KO", Date: date(t, "2026-04-01"), From: 1, To: 2, Source: "massive"})
	svc := newService(t, m, nil)
	p, err := svc.Profile(context.Background(), "KO")
	if err != nil {
		t.Fatal(err)
	}
	if p.TTMDividend.Number == nil || !near(p.TTMDividend.Number.Value, 0.5+0.5) {
		t.Errorf("TTMDividend = %+v, want 1/2 + 0.5 = 1", p.TTMDividend)
	}
}

func TestEvaluateWithDefaultsMatchesTheEngine(t *testing.T) {
	svc := newService(t, stockstest.Fixture(t), nil)
	ctx := context.Background()
	got, err := svc.Evaluate(ctx, "KO", stocks.LeversInput{})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	p, err := svc.Profile(ctx, "KO")
	if err != nil {
		t.Fatal(err)
	}
	h, err := svc.History(ctx, "KO", stocks.RangeMax)
	if err != nil {
		t.Fatal(err)
	}
	wantLevers := engine.Levers{
		Price: p.LatestClose.Value, TTMEPS: p.TTMEPS.Value, Dividend: p.TTMDividend.Number.Value,
		TargetReturn: 0.15, HoldingYears: 3,
		ExitPE:     engine.ExitPELever{Mode: engine.ExitPEEstimated},
		Baseline:   engine.BaselineLever{Mode: engine.BaselineWeighted, Custom: engine.CustomGrowth{Rates: []float64{}}},
		Scenarios:  []engine.ScenarioCase{},
		ChartRange: engine.Range3Y,
	}
	if diff := cmp.Diff(wantLevers, got.Levers); diff != "" {
		t.Errorf("Levers (-want +got):\n%s", diff)
	}
	want, err := engine.Evaluate(h.History, wantLevers)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got.Evaluation); diff != "" {
		t.Errorf("Evaluation differs from engine.Evaluate (-want +got):\n%s", diff)
	}
	price, _ := got.Sources.Price.Source.(stocks.PriceSource)
	ttm, _ := got.Sources.TTMEPS.Source.(stocks.DerivedSource)
	div, _ := got.Sources.Dividend.Source.(stocks.DerivedSource)
	if price.CloseDate != date(t, "2026-10-29") || ttm.Method != "ttm" || div.Method != "recurring_dividends_12m" {
		t.Errorf("Sources = %+v, want the close, the TTM and the dividends", got.Sources)
	}
	for name, s := range map[string]stocks.SourcedNumber{"price": got.Sources.Price, "ttmEps": got.Sources.TTMEPS, "dividend": got.Sources.Dividend} {
		if s.AsOf.IsZero() || s.AsOf.After(date(t, "2026-10-30")) {
			t.Errorf("Sources.%s.AsOf = %s, want a date up to today", name, s.AsOf)
		}
	}
	if got.AsOf != date(t, "2026-10-30") {
		t.Errorf("AsOf = %s, want the latest input date 2026-10-30", got.AsOf)
	}
}

func TestEvaluateMarksTypedValuesAsTheUsers(t *testing.T) {
	svc := newService(t, stockstest.Fixture(t), nil)
	price, eps, div, target, years := 100.0, 4.0, 1.0, 0.1, 5
	got, err := svc.Evaluate(context.Background(), "KO", stocks.LeversInput{
		Price: &price, TTMEPS: &eps, Dividend: &div, TargetReturn: &target, HoldingYears: &years,
		ExitPE: &engine.ExitPELever{Mode: engine.ExitPECustom, Custom: 20},
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if got.Levers.Price != 100 || got.Levers.TTMEPS != 4 || got.Levers.Dividend != 1 || got.Levers.TargetReturn != 0.1 || got.Levers.HoldingYears != 5 || got.Levers.ExitPE.Custom != 20 {
		t.Errorf("Levers = %+v, want the typed values", got.Levers)
	}
	for name, s := range map[string]stocks.SourcedNumber{"price": got.Sources.Price, "ttmEps": got.Sources.TTMEPS, "dividend": got.Sources.Dividend} {
		if _, ok := s.Source.(stocks.UserSource); !ok || s.AsOf != date(t, "2026-10-30") {
			t.Errorf("Sources.%s = %+v, want the user's value as of today", name, s)
		}
	}
	if req := got.Evaluation.Required.Growth; !req.Ok() {
		t.Errorf("Required.Growth = %+v, want a value", req)
	}
}

func TestEvaluateRejectsInvalidLevers(t *testing.T) {
	svc := newService(t, stockstest.Fixture(t), nil)
	target := 2.0
	_, err := svc.Evaluate(context.Background(), "KO", stocks.LeversInput{TargetReturn: &target})
	if !errors.Is(err, engine.ErrInvalidInput) {
		t.Errorf("Evaluate(target 200%%) error = %v, want engine.ErrInvalidInput", err)
	}
}

func TestEvaluateWithoutACloseNeedsAPrice(t *testing.T) {
	m := stockstest.Fixture(t)
	m.Bars = nil
	svc := newService(t, m, nil)
	_, err := svc.Evaluate(context.Background(), "KO", stocks.LeversInput{})
	var missing *stocks.MissingInputError
	if !errors.As(err, &missing) || missing.Input != "price" {
		t.Fatalf("Evaluate() error = %v, want a missing price", err)
	}
	price := 70.0
	if _, err := svc.Evaluate(context.Background(), "KO", stocks.LeversInput{Price: &price}); err != nil {
		t.Errorf("Evaluate(price) error = %v, want nil", err)
	}
}

func TestFundamentalsListsTheDerivedQuartersWithTheirFilings(t *testing.T) {
	st := stockstest.Fixture(t)
	svc := newService(t, st, nil)
	got, err := svc.Fundamentals(context.Background(), "NVDA")
	if err != nil {
		t.Fatalf("Fundamentals() error = %v", err)
	}
	series, err := deriveNVDA(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Quarters) != len(series.Quarters) || got.CIK != "0001045810" {
		t.Fatalf("quarters = %d, CIK %s; want %d, 0001045810", len(got.Quarters), got.CIK, len(series.Quarters))
	}
	last := got.Quarters[len(got.Quarters)-1]
	want := series.Quarters[len(series.Quarters)-1]
	for m, v := range want.Values {
		g, ok := last.Values[string(m)]
		if !ok || !near(g.Value, v.V) || g.Method != string(v.Method) || g.Estimate != v.Estimate || g.FirstFiled != v.FirstFiled || len(g.Inputs) != len(v.Inputs) {
			t.Errorf("%s %s = %+v, want %+v", last.Label, m, g, v)
		}
		for _, in := range g.Inputs {
			if in.URL == "" || in.Accession == "" {
				t.Errorf("%s input %+v has no filing link", m, in)
			}
		}
	}
	if len(got.Splits) == 0 || got.Splits[len(got.Splits)-1].Date != date(t, "2024-06-10") || got.Splits[len(got.Splits)-1].Ratio != 10 {
		t.Errorf("Splits = %+v, want the 10-for-1 split of 2024-06-10 last", got.Splits)
	}
	if got.Conflicts == nil {
		t.Error("Conflicts = nil, want an empty list at least")
	}
}

func TestLoadCallsTheLoaderAndReturnsTheProfile(t *testing.T) {
	loader := &stockstest.Loader{}
	svc := newService(t, stockstest.Fixture(t), loader)
	p, err := svc.Load(context.Background(), "msft")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if diff := cmp.Diff([]string{"MSFT"}, loader.Symbols); diff != "" {
		t.Errorf("loaded symbols (-want +got):\n%s", diff)
	}
	if p.Ticker.Symbol != "MSFT" {
		t.Errorf("Load() profile = %+v, want MSFT", p.Ticker)
	}
}

// The loader can find a ticker that the store does not know yet, from a
// fresh SEC ticker map.
func TestLoadAsksTheLoaderForATickerTheStoreDoesNotKnow(t *testing.T) {
	loader := &stockstest.Loader{Err: fmt.Errorf("ingest: %w", stocks.ErrUnknownTicker)}
	svc := newService(t, stockstest.Fixture(t), loader)
	if _, err := svc.Load(context.Background(), "zzzz"); !errors.Is(err, stocks.ErrUnknownTicker) {
		t.Errorf("Load(ZZZZ) error = %v, want ErrUnknownTicker", err)
	}
	if diff := cmp.Diff([]string{"ZZZZ"}, loader.Symbols); diff != "" {
		t.Errorf("loaded symbols (-want +got):\n%s", diff)
	}
}

func TestLoadReturnsTheProfileOfANewlyLoadedTicker(t *testing.T) {
	m := stockstest.Fixture(t)
	loader := &loadingLoader{m: m}
	p, err := newService(t, m, loader).Load(context.Background(), "NEW")
	if err != nil {
		t.Fatalf("Load(NEW) error = %v", err)
	}
	if p.Ticker.Symbol != "NEW" || p.Company.Name != "New Co" {
		t.Errorf("profile = %+v, want the new company", p)
	}
}

// loadingLoader stores a new company and ticker, as the ingestion jobs do.
type loadingLoader struct{ m *stockstest.Store }

func (l *loadingLoader) LoadCompany(_ context.Context, symbol string) error {
	l.m.Companies = append(l.m.Companies, domain.Company{CIK: 42, Name: "New Co"})
	l.m.Tickers = append(l.m.Tickers, store.TickerRecord{Ticker: domain.Ticker{Symbol: symbol, CIK: 42, ValidFrom: domain.NewDate(2026, 10, 30)}, Source: store.TickerSourceSEC})
	return nil
}

func TestLoadOfAnInvalidTickerDoesNotCallTheLoader(t *testing.T) {
	loader := &stockstest.Loader{}
	if _, err := newService(t, stockstest.Fixture(t), loader).Load(context.Background(), "../x"); !errors.Is(err, stocks.ErrInvalidTicker) {
		t.Errorf("Load(../x) error = %v, want ErrInvalidTicker", err)
	}
	if len(loader.Symbols) != 0 {
		t.Errorf("loader called with %v, want no call", loader.Symbols)
	}
}

func TestLoadWithoutALoaderIsUnavailable(t *testing.T) {
	svc := newService(t, stockstest.Fixture(t), nil)
	if _, err := svc.Load(context.Background(), "MSFT"); !errors.Is(err, stocks.ErrLoaderUnavailable) {
		t.Errorf("Load() error = %v, want ErrLoaderUnavailable", err)
	}
}

func TestStatusReportsRunsPricesAndCoverage(t *testing.T) {
	m := stockstest.Fixture(t)
	finished := stockstest.Now
	m.Runs = []store.IngestRun{
		{ID: 7, Job: "prices-daily", Status: store.RunSucceeded, StartedAt: stockstest.Now, Stats: map[string]int64{"bars": 3}},
		{ID: 8, Job: "sec-daily", Status: store.RunFailed, StartedAt: stockstest.Now, FinishedAt: &finished, Error: "abandoned"},
	}
	m.Coverage = store.Coverage{Companies: 3, WithFacts: 2, WithFundamentals: 2}
	m.SECRetryAttempts = map[domain.CIK]int{1: 2, 2: 6, 3: 7}
	m.Tracked = []domain.Ticker{{Symbol: "KO", CIK: stockstest.CIKKO}}
	svc := stocks.New(m, stocks.Options{Now: func() time.Time { return stockstest.Now }, SECRetryAttempts: 6})
	got, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	latest := date(t, "2026-10-29")
	want := stocks.Status{
		IngestRuns: []stocks.IngestRun{
			{ID: 7, Job: "prices-daily", Status: "succeeded", StartedAt: stockstest.Now, Stats: map[string]int64{"bars": 3}},
			{ID: 8, Job: "sec-daily", Status: "failed", Abandoned: true, StartedAt: stockstest.Now, FinishedAt: &finished, Stats: map[string]int64{}, Error: "abandoned"},
		},
		LatestPriceDate:  &latest,
		Companies:        stocks.Coverage{Total: 3, WithFacts: 2, WithFundamentals: 2},
		SECRetries:       stocks.SECRetries{Waiting: 1, Exhausted: 2},
		TrackedCompanies: 1,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Status() (-want +got):\n%s", diff)
	}
}

func TestSearchGivesSymbolNameExchangeAndLoaded(t *testing.T) {
	svc := newService(t, stockstest.Fixture(t), nil)
	got, err := svc.Search(context.Background(), "KO", 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []stocks.SearchResult{{Symbol: "KO", Name: "COCA COLA CO", Exchange: "NYSE", CIK: "0000021344", Loaded: true}}
	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("Search() (-want +got):\n%s", diff)
	}
}

// deriveNVDA derives NVIDIA's quarters the way the ingestion jobs do: its
// own facts and the provider's splits.
func deriveNVDA(m *stockstest.Store) (fundamentals.Series, error) {
	var facts []domain.Fact
	for i := range m.Facts {
		if m.Facts[i].CIK == stockstest.CIKNVDA {
			facts = append(facts, m.Facts[i])
		}
	}
	var splits []domain.Split
	for _, sp := range m.Splits {
		if sp.Symbol == "NVDA" {
			splits = append(splits, sp)
		}
	}
	return fundamentals.Derive(stockstest.CIKNVDA, facts, fundamentals.Options{Splits: splits, ProviderChecked: true})
}

func date(t *testing.T, s string) domain.Date {
	t.Helper()
	d, err := domain.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// The browser fetches the 3Y history and runs the engine on it; it must
// get what the evaluation endpoint computes from the full history.
func TestTheThreeYearHistoryEvaluatesLikeTheFullOne(t *testing.T) {
	svc := newService(t, stockstest.Fixture(t), nil)
	ctx := context.Background()
	for _, ticker := range []string{"NVDA", "KO"} {
		full, err := svc.History(ctx, ticker, stocks.RangeMax)
		if err != nil {
			t.Fatal(err)
		}
		short, err := svc.History(ctx, ticker, stocks.Range3Y)
		if err != nil {
			t.Fatal(err)
		}
		if len(short.History.Daily) >= len(full.History.Daily) {
			t.Fatalf("%s: 3Y has %d days, max %d; want fewer", ticker, len(short.History.Daily), len(full.History.Daily))
		}
		for _, rng := range []engine.ChartRange{engine.Range6M, engine.Range1Y, engine.Range3Y} {
			ev, err := svc.Evaluate(ctx, ticker, stocks.LeversInput{ChartRange: &rng})
			if err != nil {
				t.Fatal(err)
			}
			got, err := engine.Evaluate(short.History, ev.Levers)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(ev.Evaluation, got); diff != "" {
				t.Errorf("%s %s: the 3Y history evaluates differently (-endpoint +3Y):\n%.2000s", ticker, rng, diff)
			}
		}
	}
}

func TestEvaluateRejectsCasesWithMissingFields(t *testing.T) {
	svc := newService(t, stockstest.Fixture(t), nil)
	name, growth, pe := "Bear", 0.05, 15.0
	for field, c := range map[string]stocks.ScenarioInput{
		"name":   {Growth: &growth, ExitPE: &pe},
		"growth": {Name: &name, ExitPE: &pe},
		"exitPe": {Name: &name, Growth: &growth},
	} {
		_, err := svc.Evaluate(context.Background(), "KO", stocks.LeversInput{Scenarios: []stocks.ScenarioInput{c}})
		if !errors.Is(err, engine.ErrInvalidInput) || !strings.Contains(err.Error(), "scenarios[0]."+field) {
			t.Errorf("a case without %s: error = %v, want an invalid input naming it", field, err)
		}
	}
	got, err := svc.Evaluate(context.Background(), "KO", stocks.LeversInput{Scenarios: []stocks.ScenarioInput{{Name: &name, Growth: &growth, ExitPE: &pe}}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []engine.ScenarioCase{{Name: "Bear", Growth: 0.05, ExitPE: 15}}; !cmp.Equal(want, got.Levers.Scenarios) {
		t.Errorf("Levers.Scenarios = %+v, want %+v", got.Levers.Scenarios, want)
	}
}

// When only the provider's mapping shows an earlier issuer of the symbol,
// the earlier bars are not the current company's either.
func TestTheProvidersMappingAlsoBoundsTheBars(t *testing.T) {
	m := stockstest.Fixture(t)
	until := date(t, "2026-03-02")
	m.Companies = append(m.Companies, domain.Company{CIK: 1, Name: "Old Foo"}, domain.Company{CIK: 2, Name: "Foo Class A"})
	m.Tickers = append(m.Tickers,
		store.TickerRecord{Ticker: domain.Ticker{Symbol: "FOO-A", CIK: 2, ValidFrom: date(t, "2026-01-01")}, Source: store.TickerSourceSEC},
		store.TickerRecord{Ticker: domain.Ticker{Symbol: "FOO-A", CIK: 1, ValidFrom: date(t, "2020-01-01"), ValidTo: &until}, Source: "massive"},
	)
	for _, b := range []struct {
		day   string
		close float64
	}{{"2026-02-27", 500}, {"2026-03-02", 20}} {
		m.Bars = append(m.Bars, domain.PriceBar{Symbol: "FOO-A", Date: date(t, b.day), Close: b.close, Source: "massive"})
	}
	p, err := newService(t, m, nil).Profile(context.Background(), "FOO-A")
	if err != nil {
		t.Fatal(err)
	}
	if p.LatestClose == nil || p.LatestClose.Value != 20 || p.Freshness.LatestClose == nil {
		t.Fatalf("LatestClose = %+v, want 20", p.LatestClose)
	}
	h := reusedHistory(t, m, "FOO-A", 2)
	if len(h.History.Daily) != 1 {
		t.Errorf("daily = %+v, want only the bar from 2026-03-02", h.History.Daily)
	}
}

// reusedHistory loads NVIDIA's facts as the company's and returns its
// history.
func reusedHistory(t *testing.T, m *stockstest.Store, ticker string, cik domain.CIK) stocks.History {
	t.Helper()
	m.Loaded[cik] = true
	m.Facts = append(m.Facts, nvdaFactsAs(t, m, cik)...)
	h, err := newService(t, m, nil).History(context.Background(), ticker, stocks.RangeMax)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// Without the provider's event, NVIDIA's 2024 split is known from its
// filings only. The closes must follow the split that the EPS follows.
func TestASplitKnownOnlyFromFilingsAdjustsTheClosesToo(t *testing.T) {
	m := stockstest.Fixture(t)
	m.Splits = slices.DeleteFunc(m.Splits, func(s domain.Split) bool { return s.Symbol == "NVDA" })
	h, err := newService(t, m, nil).History(context.Background(), "NVDA", stocks.RangeMax)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range h.History.Daily {
		if d.Date == date(t, "2024-05-01") && d.Close > 200 {
			t.Errorf("close on 2024-05-01 = %v, want it divided by 10 for the split that the filings give", d.Close)
		}
	}
}

// Facts filed after today are not known today: a restatement filed
// tomorrow does not change today's quarter.
func TestFactsFiledAfterTodayAreNotUsed(t *testing.T) {
	m := stockstest.Fixture(t)
	restated := domain.Fact{
		CIK: stockstest.CIKKO, Taxonomy: "us-gaap", Concept: "EarningsPerShareDiluted", Unit: "USD/shares",
		PeriodStart: ptr(date(t, "2026-07-04")), PeriodEnd: date(t, "2026-10-02"), Value: 0.5, Form: "10-Q/A",
		FiscalYear: 2026, FiscalPeriod: "Q3", Filed: date(t, "2026-11-05"), Accession: "0001628280-26-999999",
	}
	m.Facts = append(m.Facts, restated)
	got := mustFundamentals(t, newService(t, m, nil), "KO")
	last := got.Quarters[len(got.Quarters)-1]
	if v := last.Values["eps_diluted"]; last.PeriodEnd != date(t, "2026-10-02") || !near(v.Value, 0.93) {
		t.Errorf("%s EPS = %v, want 0.93 as filed on 2026-10-28: the restatement of 2026-11-05 is not known yet", last.Label, v.Value)
	}
}

func mustFundamentals(t *testing.T, svc *stocks.Service, ticker string) stocks.Fundamentals {
	t.Helper()
	f, err := svc.Fundamentals(context.Background(), ticker)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func ptr[T any](v T) *T { return &v }

// Stored data outside the engine's bounds must never fail an evaluation
// that uses the default levers: only levers the user sends can.
func TestStoredDataOutsideTheEngineBoundsStillEvaluates(t *testing.T) {
	koLast := []string{"2026-04-03", "2026-07-03", "2026-10-02"}
	tests := map[string]struct {
		edit  func(t *testing.T, m *stockstest.Store)
		check func(t *testing.T, ev stocks.Evaluation, h stocks.History)
	}{
		"a quarter EPS of 1e-7": {
			edit: func(t *testing.T, m *stockstest.Store) {
				m.SetQuarterValue(t, stockstest.CIKKO, "EarningsPerShareDiluted", date(t, koLast[1]), 1e-7)
			},
			check: func(t *testing.T, _ stocks.Evaluation, h stocks.History) {
				for i, q := range h.History.Quarters {
					if q.PeriodEnd == date(t, koLast[1]) && (q.EPS != nil || h.Quarters[i].EPS != nil) {
						t.Errorf("%s EPS = %v, want null: 1e-7 is below the engine's smallest EPS", q.Label, *q.EPS)
					}
				}
			},
		},
		"a TTM EPS of 0.00005": {
			// Q4 FY25 is 0.53 (3.04 for the year less 2.51 for nine months).
			edit: func(t *testing.T, m *stockstest.Store) {
				for i, v := range []float64{-0.2, -0.2, -0.12995} {
					m.SetQuarterValue(t, stockstest.CIKKO, "EarningsPerShareDiluted", date(t, koLast[i]), v)
				}
			},
			check: func(t *testing.T, ev stocks.Evaluation, h stocks.History) {
				if ev.Levers.TTMEPS != 0 || ev.Evaluation.Required.Growth.Reason != engine.ReasonNeedsPositiveEPS {
					t.Errorf("ttmEps %v, required growth %+v; want 0 and needs_positive_eps", ev.Levers.TTMEPS, ev.Evaluation.Required.Growth)
				}
				if last := h.History.Daily[len(h.History.Daily)-1]; last.TTMEPS == nil || *last.TTMEPS != 0 {
					t.Errorf("last daily ttmEps = %v, want 0", last.TTMEPS)
				}
			},
		},
		"a close of 2e6": {
			edit: func(t *testing.T, m *stockstest.Store) {
				m.Bars = append(m.Bars, domain.PriceBar{Symbol: "KO", Date: date(t, "2026-10-30"), Close: 2e6, Source: "massive"})
				for i := range m.Bars {
					if m.Bars[i].Symbol == "KO" && m.Bars[i].Date == date(t, "2025-03-03") {
						m.Bars[i].Close = 2e6
					}
				}
			},
			check: func(t *testing.T, ev stocks.Evaluation, h stocks.History) {
				if ev.Levers.Price != 82.14 {
					t.Errorf("price = %v, want the last close within bounds, 82.14", ev.Levers.Price)
				}
				for _, d := range h.History.Daily {
					if d.Close > 1e6 {
						t.Errorf("daily close %v on %s, want it left out", d.Close, d.Date)
					}
				}
			},
		},
		"gaps in the quarters and the closes": {
			edit: func(_ *testing.T, m *stockstest.Store) {
				m.Facts = slices.DeleteFunc(m.Facts, func(f domain.Fact) bool {
					return f.CIK == stockstest.CIKKO && f.PeriodEnd.Year == 2024
				})
				m.Bars = slices.DeleteFunc(m.Bars, func(b domain.PriceBar) bool {
					return b.Symbol == "KO" && b.Date.Year == 2025 && b.Date.Month <= 6
				})
			},
			check: func(t *testing.T, ev stocks.Evaluation, _ stocks.History) {
				if !ev.Evaluation.PENow.Ok() {
					t.Errorf("peNow = %+v, want a value", ev.Evaluation.PENow)
				}
			},
		},
		"a TTM dividend above the lever bound": {
			edit: func(t *testing.T, m *stockstest.Store) {
				m.Dividends = append(m.Dividends, domain.Dividend{Symbol: "KO", ExDate: date(t, "2026-10-01"), CashAmount: 2e6, Currency: "USD", Type: "recurring", Source: "massive", ProviderID: "big"})
			},
			check: func(t *testing.T, ev stocks.Evaluation, _ stocks.History) {
				if ev.Levers.Dividend != 1e6 {
					t.Errorf("dividend = %v, want it clamped to 1e6", ev.Levers.Dividend)
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			m := stockstest.Fixture(t)
			tt.edit(t, m)
			svc := newService(t, m, nil)
			ev, err := svc.Evaluate(context.Background(), "KO", stocks.LeversInput{})
			if err != nil {
				t.Fatalf("Evaluate() error = %v, want an evaluation with reasons", err)
			}
			h, err := svc.History(context.Background(), "KO", stocks.RangeMax)
			if err != nil {
				t.Fatal(err)
			}
			if err := h.History.Validate(); err != nil {
				t.Errorf("History.Validate() = %v, want a history the engine accepts", err)
			}
			if _, err := svc.Profile(context.Background(), "KO"); err != nil {
				t.Errorf("Profile() error = %v", err)
			}
			tt.check(t, ev, h)
		})
	}
}

// fetchDate is the New York date of a fetch time.
func fetchDate(t *testing.T, at time.Time) domain.Date {
	t.Helper()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	return domain.DateOf(at.In(ny))
}

func TestDividendWithoutDividendDataIsMissing(t *testing.T) {
	m := stockstest.Fixture(t)
	delete(m.DividendsLoadedAt, "NVDA")
	delete(m.PriceReferenceCheckedAt, "NVDA")
	svc := newService(t, m, nil)
	p, err := svc.Profile(context.Background(), "NVDA")
	if err != nil {
		t.Fatal(err)
	}
	if p.TTMDividend.Number != nil || p.TTMDividend.Reason != stocks.ReasonNoDividendData {
		t.Errorf("TTMDividend = %+v, want missing with reason no_dividend_data", p.TTMDividend)
	}
	ev, err := svc.Evaluate(context.Background(), "NVDA", stocks.LeversInput{})
	if err != nil {
		t.Fatal(err)
	}
	src, _ := ev.Sources.Dividend.Source.(stocks.DerivedSource)
	if ev.Levers.Dividend != 0 || src.Method != "not_available" || ev.Sources.Dividend.AsOf != date(t, "2026-10-30") {
		t.Errorf("dividend lever %v from %+v, want 0 from a not_available source as of today", ev.Levers.Dividend, ev.Sources.Dividend)
	}
}

func TestDividendFetchedWithoutRowsIsZeroAsOfTheFetch(t *testing.T) {
	m := stockstest.Fixture(t)
	delete(m.DividendsLoadedAt, "NVDA")
	m.PriceReferenceCheckedAt["NVDA"] = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	m.Dividends = slices.DeleteFunc(m.Dividends, func(d domain.Dividend) bool { return d.Symbol == "NVDA" })
	finished := time.Date(2026, 10, 29, 23, 0, 0, 0, time.UTC)
	m.Runs = []store.IngestRun{{ID: 1, Job: "price-reference", Status: store.RunSucceeded, StartedAt: finished.Add(-time.Hour), FinishedAt: &finished}}
	p, err := newService(t, m, nil).Profile(context.Background(), "NVDA")
	if err != nil {
		t.Fatal(err)
	}
	if d := p.TTMDividend.Number; d == nil || d.Value != 0 || d.AsOf != date(t, "2026-10-29") {
		t.Errorf("TTMDividend = %+v, want 0 as of the run's day 2026-10-29", p.TTMDividend)
	}
}

// countingHandler counts the warnings logged.
type countingHandler struct{ warnings *int }

func (h countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h countingHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level == slog.LevelWarn {
		*h.warnings++
	}
	return nil
}
func (h countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h countingHandler) WithGroup(string) slog.Handler      { return h }

func TestAValueOutsideTheBoundsIsLoggedOnce(t *testing.T) {
	m := stockstest.Fixture(t)
	m.SetQuarterValue(t, stockstest.CIKKO, "EarningsPerShareDiluted", date(t, "2026-07-03"), 1e-7)
	n := 0
	svc := stocks.New(m, stocks.Options{Now: func() time.Time { return stockstest.Now }, Logger: slog.New(countingHandler{&n})})
	for range 3 {
		if _, err := svc.History(context.Background(), "KO", stocks.RangeMax); err != nil {
			t.Fatal(err)
		}
	}
	if n != 1 {
		t.Errorf("warnings = %d, want 1 for one ticker and field", n)
	}
}

// A price-reference run that fetched the dividends again without a change
// still dates them.
func TestDividendAsOfIsTheLastFetch(t *testing.T) {
	m := stockstest.Fixture(t)
	changed := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	m.DividendsLoadedAt["KO"] = changed
	m.PriceReferenceCheckedAt["KO"] = changed
	finished := time.Date(2026, 10, 29, 23, 0, 0, 0, time.UTC)
	m.Runs = []store.IngestRun{{ID: 1, Job: "price-reference", Status: store.RunSucceeded, StartedAt: finished.Add(-time.Hour), FinishedAt: &finished}}
	p, err := newService(t, m, nil).Profile(context.Background(), "KO")
	if err != nil {
		t.Fatal(err)
	}
	if d := p.TTMDividend.Number; d == nil || d.AsOf != date(t, "2026-10-29") {
		t.Errorf("TTMDividend = %+v, want as of the run's day 2026-10-29", p.TTMDividend)
	}
}

// A price-reference run that never fetched the symbol does not make its
// dividend known.
func TestARunThatNeverCoveredTheSymbolLeavesItsDividendMissing(t *testing.T) {
	m := stockstest.Fixture(t)
	delete(m.DividendsLoadedAt, "NVDA")
	delete(m.PriceReferenceCheckedAt, "NVDA")
	finished := time.Date(2026, 10, 29, 23, 0, 0, 0, time.UTC)
	m.Runs = []store.IngestRun{{ID: 1, Job: "price-reference", Status: store.RunSucceeded, StartedAt: finished.Add(-time.Hour), FinishedAt: &finished}}
	p, err := newService(t, m, nil).Profile(context.Background(), "NVDA")
	if err != nil {
		t.Fatal(err)
	}
	if p.TTMDividend.Number != nil || p.TTMDividend.Reason != stocks.ReasonNoDividendData {
		t.Errorf("TTMDividend = %+v, want missing with no_dividend_data", p.TTMDividend)
	}
}
