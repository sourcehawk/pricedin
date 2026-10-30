//go:build integration

package stocks_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/stocks"
	"github.com/sourcehawk/pricedin/server/internal/stocks/stockstest"
	"github.com/sourcehawk/pricedin/server/internal/store/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.Main(m)
}

// The service must give the same answers over Postgres as over the
// in-memory store, whose semantics the unit tests rely on.
func TestTheServiceOverPostgresMatchesTheFixtures(t *testing.T) {
	ctx := context.Background()
	st := pgtest.NewStore(t)
	stockstest.Seed(t, st)
	clock := func() time.Time { return stockstest.Now }
	pg := stocks.New(st, stocks.Options{Now: clock})
	fake := stocks.New(stockstest.Fixture(t), stocks.Options{Now: clock})

	for _, ticker := range []string{"NVDA", "KO", "MSFT"} {
		t.Run(ticker, func(t *testing.T) {
			compare(t, "Profile", func(s *stocks.Service) (any, error) { return s.Profile(ctx, ticker) }, pg, fake)
			if ticker == "MSFT" {
				return
			}
			compare(t, "History", func(s *stocks.Service) (any, error) { return s.History(ctx, ticker, stocks.Range3Y) }, pg, fake)
			compare(t, "Fundamentals", func(s *stocks.Service) (any, error) { return s.Fundamentals(ctx, ticker) }, pg, fake)
			compare(t, "Evaluate", func(s *stocks.Service) (any, error) { return s.Evaluate(ctx, ticker, stocks.LeversInput{}) }, pg, fake)
		})
	}
	// The fake matches ticker prefixes only, not company names.
	compare(t, "Search", func(s *stocks.Service) (any, error) { return s.Search(ctx, "NV", 10) }, pg, fake)
}

func compare(t *testing.T, what string, call func(*stocks.Service) (any, error), pg, fake *stocks.Service) {
	t.Helper()
	got, err := call(pg)
	if err != nil {
		t.Fatalf("%s over Postgres: %v", what, err)
	}
	want, err := call(fake)
	if err != nil {
		t.Fatalf("%s over the fixtures: %v", what, err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("%s over Postgres differs (-fixtures +postgres):\n%.4000s", what, diff)
	}
}

func TestNVIDIAAcrossItsSplitOverPostgres(t *testing.T) {
	ctx := context.Background()
	st := pgtest.NewStore(t)
	stockstest.Seed(t, st)
	svc := stocks.New(st, stocks.Options{Now: func() time.Time { return stockstest.Now }})
	h, err := svc.History(ctx, "NVDA", stocks.RangeMax)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range h.History.Daily {
		if d.Date == date(t, "2024-06-07") && !near(d.Close, 73.13) {
			t.Errorf("close on 2024-06-07 = %v, want 731.30 / 10", d.Close)
		}
	}
	for i, q := range h.History.Quarters {
		if q.Close != nil && h.Quarters[i].Close.CloseDate.Before(date(t, "2024-06-10")) && h.Quarters[i].Close.SplitFactor != 10 {
			t.Errorf("%s close split factor = %v, want 10 before the split", q.Label, h.Quarters[i].Close.SplitFactor)
		}
	}
}

func TestCocaColaDividendOverPostgres(t *testing.T) {
	ctx := context.Background()
	st := pgtest.NewStore(t)
	stockstest.Seed(t, st)
	svc := stocks.New(st, stocks.Options{Now: func() time.Time { return stockstest.Now }})
	p, err := svc.Profile(ctx, "KO")
	if err != nil {
		t.Fatal(err)
	}
	if !near(p.TTMDividend.Value, 2.06) {
		t.Errorf("TTM dividend = %v, want 2.06", p.TTMDividend.Value)
	}
}
