package stocks_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/engine"
	"github.com/sourcehawk/pricedin/server/internal/fundamentals"
	"github.com/sourcehawk/pricedin/server/internal/stocks"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

func newService(t *testing.T, st *memStore, loader stocks.CompanyLoader) *stocks.Service {
	t.Helper()
	return stocks.New(st, stocks.Options{Now: func() time.Time { return now }, Loader: loader})
}

func near(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestProfileOfALoadedStock(t *testing.T) {
	svc := newService(t, fixtureStore(t), nil)
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
	if c == nil || c.Value != 161.36 || c.AsOf != date(t, "2026-10-29") || c.AsOfLabel != "as of close" {
		t.Fatalf("LatestClose = %+v, want 161.36 as of close 2026-10-29", c)
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
	svc := newService(t, fixtureStore(t), nil)
	p, err := svc.Profile(context.Background(), "NVDA")
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range p.TTMEPS.Source.(stocks.DerivedSource).Inputs {
		var secs []stocks.SECSource
		switch s := in.(type) {
		case stocks.SECSource:
			secs = []stocks.SECSource{s}
		case stocks.DerivedSource:
			for _, i := range s.Inputs {
				secs = append(secs, i.(stocks.SECSource))
			}
		}
		for _, s := range secs {
			if s.Accession == "" || s.Form == "" || s.Filed.IsZero() || s.URL == "" {
				t.Errorf("input %+v, want accession, form, filing date and URL", s)
			}
		}
	}
}

func TestProfileOfAStockWithoutData(t *testing.T) {
	svc := newService(t, fixtureStore(t), nil)
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
	svc := newService(t, fixtureStore(t), nil)
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
	st := fixtureStore(t)
	svc := newService(t, st, nil)
	h, err := svc.History(context.Background(), "NVDA", stocks.RangeMax)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	closes := map[domain.Date]float64{}
	for _, d := range h.History.Daily {
		closes[d.Date] = d.Close
	}
	for day, want := range map[string]float64{"2024-06-07": 73.13, "2024-06-10": 73.98, "2021-11-01": 28, "2026-10-29": 161.36} {
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
	st := fixtureStore(t)
	svc := newService(t, st, nil)
	h, err := svc.History(context.Background(), "NVDA", stocks.RangeMax)
	if err != nil {
		t.Fatal(err)
	}
	series, err := deriveNVDA(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range h.History.Daily {
		_, v, ok := fundamentals.TTMKnownOn(series, fundamentals.EPSDiluted, d.Date)
		if ok != (d.TTMEPS != nil) || (ok && !near(*d.TTMEPS, v.V)) {
			t.Fatalf("ttmEps on %s = %v, want %v (known %v)", d.Date, d.TTMEPS, v.V, ok)
		}
	}
}

func TestHistoryRangeCutsTheDailyClosesWithAYearBefore(t *testing.T) {
	svc := newService(t, fixtureStore(t), nil)
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
		first := h.History.Daily[0].Date
		if first.Before(date(t, from)) || first.After(date(t, from).AddDays(5)) {
			t.Errorf("range %s starts on %s, want the first trading day from %s", rng, first, from)
		}
	}
}

func TestHistoryOfAStockWithoutDataIsNotLoaded(t *testing.T) {
	svc := newService(t, fixtureStore(t), nil)
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
func reusedSymbolStore(t *testing.T) *memStore {
	t.Helper()
	until := date(t, "2026-03-02")
	m := fixtureStore(t)
	m.companies = append(m.companies, domain.Company{CIK: 1, Name: "Old Acme"}, domain.Company{CIK: 2, Name: "New Acme"})
	m.tickers = append(m.tickers,
		store.TickerRecord{Ticker: domain.Ticker{Symbol: "ACME", CIK: 1, ValidFrom: date(t, "2020-01-01"), ValidTo: &until}, Source: store.TickerSourceSEC},
		store.TickerRecord{Ticker: domain.Ticker{Symbol: "ACME", CIK: 2, ValidFrom: until}, Source: store.TickerSourceSEC},
	)
	for _, b := range []struct {
		day   string
		close float64
	}{{"2026-02-27", 500}, {"2026-03-02", 20}, {"2026-03-03", 21}} {
		m.bars = append(m.bars, domain.PriceBar{Symbol: "ACME", Date: date(t, b.day), Close: b.close, Source: "massive"})
	}
	m.splits = append(m.splits, domain.Split{Symbol: "ACME", Date: date(t, "2025-06-02"), From: 1, To: 2, Source: "massive"})
	m.dividends = append(m.dividends, domain.Dividend{Symbol: "ACME", ExDate: date(t, "2026-01-15"), CashAmount: 9, Type: "recurring", Source: "massive", ProviderID: "x"})
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
	if p.TTMDividend.Value != 0 {
		t.Errorf("TTMDividend = %+v, want 0: the dividend belongs to the old issuer", p.TTMDividend)
	}
}

func TestBarsOfTheOldIssuerAreNotInTheHistory(t *testing.T) {
	m := reusedSymbolStore(t)
	m.loaded[2] = true
	m.facts = append(m.facts, nvdaFactsAs(t, m, 2)...)
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
func nvdaFactsAs(t *testing.T, m *memStore, cik domain.CIK) []domain.Fact {
	t.Helper()
	var out []domain.Fact
	for _, f := range m.facts {
		if f.CIK == cikNVDA {
			f.CIK = cik
			out = append(out, f)
		}
	}
	return out
}

func TestTTMDividendCountsRecurringDividendsOfTheLastTwelveMonths(t *testing.T) {
	svc := newService(t, fixtureStore(t), nil)
	p, err := svc.Profile(context.Background(), "KO")
	if err != nil {
		t.Fatal(err)
	}
	d := p.TTMDividend
	if !near(d.Value, 0.5+0.52+0.52+0.52) {
		t.Fatalf("TTMDividend = %+v, want 2.06 (ex-dates 2025-12-01 to 2026-09-14; not the future 2026-11-30)", d)
	}
	src, ok := d.Source.(stocks.DerivedSource)
	if !ok || src.Method != "recurring_dividends_12m" || len(src.Inputs) != 4 {
		t.Fatalf("TTMDividend.Source = %#v, want four dividends", d.Source)
	}
	if first := src.Inputs[0].(stocks.DividendSource); first.ExDate != date(t, "2025-12-01") || first.Provider != "massive" {
		t.Errorf("first input = %+v, want the massive dividend of 2025-12-01", first)
	}
	if d.AsOf != date(t, "2026-10-30") {
		t.Errorf("TTMDividend.AsOf = %s, want today 2026-10-30", d.AsOf)
	}
}

func TestTTMDividendLeavesOutSpecialDividendsAndAdjustsForSplits(t *testing.T) {
	m := fixtureStore(t)
	m.dividends = []domain.Dividend{
		{Symbol: "KO", ExDate: date(t, "2026-01-15"), CashAmount: 1, Type: "recurring", Source: "massive", ProviderID: "a"},
		{Symbol: "KO", ExDate: date(t, "2026-02-15"), CashAmount: 5, Type: "special", Source: "massive", ProviderID: "b"},
		{Symbol: "KO", ExDate: date(t, "2026-05-15"), CashAmount: 0.5, Type: "recurring", Source: "massive", ProviderID: "c"},
	}
	m.splits = append(m.splits, domain.Split{Symbol: "KO", Date: date(t, "2026-04-01"), From: 1, To: 2, Source: "massive"})
	svc := newService(t, m, nil)
	p, err := svc.Profile(context.Background(), "KO")
	if err != nil {
		t.Fatal(err)
	}
	if !near(p.TTMDividend.Value, 0.5+0.5) {
		t.Errorf("TTMDividend = %+v, want 1/2 + 0.5 = 1", p.TTMDividend)
	}
}

func TestEvaluateWithDefaultsMatchesTheEngine(t *testing.T) {
	svc := newService(t, fixtureStore(t), nil)
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
		Price: p.LatestClose.Value, TTMEPS: p.TTMEPS.Value, Dividend: p.TTMDividend.Value,
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
	if got.Sources.Price.Source.(stocks.PriceSource).CloseDate != date(t, "2026-10-29") ||
		got.Sources.TTMEPS.Source.(stocks.DerivedSource).Method != "ttm" ||
		got.Sources.Dividend.Source.(stocks.DerivedSource).Method != "recurring_dividends_12m" {
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
	svc := newService(t, fixtureStore(t), nil)
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
	svc := newService(t, fixtureStore(t), nil)
	target := 2.0
	_, err := svc.Evaluate(context.Background(), "KO", stocks.LeversInput{TargetReturn: &target})
	if !errors.Is(err, engine.ErrInvalidInput) {
		t.Errorf("Evaluate(target 200%%) error = %v, want engine.ErrInvalidInput", err)
	}
}

func TestEvaluateWithoutACloseNeedsAPrice(t *testing.T) {
	m := fixtureStore(t)
	m.bars = nil
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
	st := fixtureStore(t)
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
	loader := &fakeLoader{}
	svc := newService(t, fixtureStore(t), loader)
	p, err := svc.Load(context.Background(), "msft")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if diff := cmp.Diff([]string{"MSFT"}, loader.symbols); diff != "" {
		t.Errorf("loaded symbols (-want +got):\n%s", diff)
	}
	if p.Ticker.Symbol != "MSFT" {
		t.Errorf("Load() profile = %+v, want MSFT", p.Ticker)
	}
}

func TestLoadOfAnUnknownTickerDoesNotCallTheLoader(t *testing.T) {
	loader := &fakeLoader{}
	svc := newService(t, fixtureStore(t), loader)
	if _, err := svc.Load(context.Background(), "ZZZZ"); !errors.Is(err, stocks.ErrUnknownTicker) {
		t.Errorf("Load(ZZZZ) error = %v, want ErrUnknownTicker", err)
	}
	if len(loader.symbols) != 0 {
		t.Errorf("loader called with %v, want no call", loader.symbols)
	}
}

func TestLoadWithoutALoaderIsUnavailable(t *testing.T) {
	svc := newService(t, fixtureStore(t), nil)
	if _, err := svc.Load(context.Background(), "MSFT"); !errors.Is(err, stocks.ErrLoaderUnavailable) {
		t.Errorf("Load() error = %v, want ErrLoaderUnavailable", err)
	}
}

func TestStatusReportsRunsPricesAndCoverage(t *testing.T) {
	m := fixtureStore(t)
	m.runs = []store.IngestRun{{ID: 7, Job: "prices-daily", Status: store.RunSucceeded, StartedAt: now, Stats: map[string]int64{"bars": 3}}}
	m.coverage = store.Coverage{Companies: 3, WithFacts: 2, WithFundamentals: 2}
	svc := newService(t, m, nil)
	got, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	latest := date(t, "2026-10-29")
	want := stocks.Status{
		IngestRuns:      []stocks.IngestRun{{ID: 7, Job: "prices-daily", Status: "succeeded", StartedAt: now, Stats: map[string]int64{"bars": 3}}},
		LatestPriceDate: &latest,
		Companies:       stocks.Coverage{Total: 3, WithFacts: 2, WithFundamentals: 2},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Status() (-want +got):\n%s", diff)
	}
}

func TestSearchGivesSymbolNameExchangeAndLoaded(t *testing.T) {
	svc := newService(t, fixtureStore(t), nil)
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
func deriveNVDA(m *memStore) (fundamentals.Series, error) {
	var facts []domain.Fact
	for _, f := range m.facts {
		if f.CIK == cikNVDA {
			facts = append(facts, f)
		}
	}
	var splits []domain.Split
	for _, sp := range m.splits {
		if sp.Symbol == "NVDA" {
			splits = append(splits, sp)
		}
	}
	return fundamentals.Derive(cikNVDA, facts, fundamentals.Options{Splits: splits, ProviderChecked: true})
}
