//go:build integration

package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

func mustReplaceOneQuarter(t *testing.T, s *store.Store, cik domain.CIK) {
	t.Helper()
	v := store.QuarterlyValue{
		FiscalYear: 2026, FiscalQuarter: 1, Metric: "eps_diluted", Value: 1, Method: "reported",
		PeriodStart: date(t, "2026-01-01"), PeriodEnd: date(t, "2026-03-31"), Filed: date(t, "2026-05-01"),
	}
	if err := s.ReplaceQuarterlyFundamentals(context.Background(), cik, []store.QuarterlyValue{v}); err != nil {
		t.Fatalf("ReplaceQuarterlyFundamentals() error = %v", err)
	}
}

func TestHasQuarterlyFundamentals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme, globex)
	mustReplaceOneQuarter(t, s, acme.CIK)

	for cik, want := range map[domain.CIK]bool{acme.CIK: true, globex.CIK: false, 999: false} {
		got, err := s.HasQuarterlyFundamentals(ctx, cik)
		if err != nil {
			t.Fatalf("HasQuarterlyFundamentals(%s) error = %v", cik, err)
		}
		if got != want {
			t.Errorf("HasQuarterlyFundamentals(%s) = %v, want %v", cik, got, want)
		}
	}
}

func TestGetCoverageCountsCompaniesByWhatIsLoaded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme, globex, initech)
	fact := domain.Fact{
		CIK: globex.CIK, Taxonomy: "us-gaap", Concept: "EarningsPerShareDiluted", Unit: "USD/shares",
		PeriodStart: datePtr(t, "2026-01-01"), PeriodEnd: date(t, "2026-03-31"), Value: 1, Form: "10-Q",
		FiscalYear: 2026, FiscalPeriod: "Q1", Filed: date(t, "2026-05-01"), Accession: "0001002-26-000001",
	}
	for _, cik := range []domain.CIK{acme.CIK, globex.CIK} {
		f := fact
		f.CIK = cik
		if _, err := s.InsertFacts(ctx, []domain.Fact{f}); err != nil {
			t.Fatalf("InsertFacts() error = %v", err)
		}
	}
	mustReplaceOneQuarter(t, s, acme.CIK)

	got, err := s.GetCoverage(ctx)
	if err != nil {
		t.Fatalf("GetCoverage() error = %v", err)
	}
	if diff := cmp.Diff(store.Coverage{Companies: 3, WithFacts: 2, WithFundamentals: 1}, got); diff != "" {
		t.Errorf("GetCoverage() (-want +got):\n%s", diff)
	}
}

func TestGetCoverageOfAnEmptyDatabaseIsZero(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	got, err := s.GetCoverage(context.Background())
	if err != nil {
		t.Fatalf("GetCoverage() error = %v", err)
	}
	if got != (store.Coverage{}) {
		t.Errorf("GetCoverage() = %+v, want zeros", got)
	}
}

func TestListSymbolTickersGivesEveryRowOfTheSymbol(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme, globex)
	// ACME belonged to acme until 2026-03-02, then to globex.
	mustSyncTickers(t, s, store.TickerSourceSEC, date(t, "2026-01-02"),
		domain.Ticker{Symbol: "ACME", CIK: acme.CIK, Exchange: "NYSE"},
		domain.Ticker{Symbol: "GBX", CIK: globex.CIK, Exchange: "Nasdaq"})
	mustSyncTickers(t, s, store.TickerSourceSEC, date(t, "2026-03-02"),
		domain.Ticker{Symbol: "ACME", CIK: globex.CIK, Exchange: "NYSE"},
		domain.Ticker{Symbol: "GBX", CIK: globex.CIK, Exchange: "Nasdaq"})

	got, err := s.ListSymbolTickers(ctx, "acme")
	if err != nil {
		t.Fatalf("ListSymbolTickers() error = %v", err)
	}
	want := []store.TickerRecord{
		{Ticker: domain.Ticker{Symbol: "ACME", CIK: acme.CIK, Exchange: "NYSE", ValidFrom: date(t, "2026-01-02"), ValidTo: datePtr(t, "2026-03-02")}, Source: store.TickerSourceSEC},
		{Ticker: domain.Ticker{Symbol: "ACME", CIK: globex.CIK, Exchange: "NYSE", ValidFrom: date(t, "2026-03-02")}, Source: store.TickerSourceSEC},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ListSymbolTickers() (-want +got):\n%s", diff)
	}
	none, err := s.ListSymbolTickers(ctx, "NOPE")
	if err != nil || len(none) != 0 {
		t.Errorf("ListSymbolTickers(unknown) = %v, %v; want no rows", none, err)
	}
}

func TestGetDividendsLoadedAt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	if _, err := s.GetDividendsLoadedAt(ctx, "KO"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetDividendsLoadedAt() before a load error = %v, want ErrNotFound", err)
	}
	before := time.Now().Add(-time.Minute)
	d := domain.Dividend{Symbol: "KO", ExDate: date(t, "2026-09-14"), CashAmount: 0.52, Currency: "USD", Type: "recurring", Source: "massive", ProviderID: "e1"}
	if _, err := s.UpsertDividends(ctx, []domain.Dividend{d}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetDividendsLoadedAt(ctx, "ko")
	if err != nil || got.Before(before) {
		t.Errorf("GetDividendsLoadedAt() = %v, %v; want the time of the load", got, err)
	}
}

func TestGetLatestSucceededIngestRun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	if _, err := s.GetLatestSucceededIngestRun(ctx, "price-reference"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetLatestSucceededIngestRun() without runs error = %v, want ErrNotFound", err)
	}
	ok, _ := s.StartIngestRun(ctx, "price-reference")
	if err := s.FinishIngestRun(ctx, ok, store.RunSucceeded, nil, ""); err != nil {
		t.Fatal(err)
	}
	failed, _ := s.StartIngestRun(ctx, "price-reference")
	if err := s.FinishIngestRun(ctx, failed, store.RunFailed, nil, "boom"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartIngestRun(ctx, "price-reference"); err != nil { // still running
		t.Fatal(err)
	}
	got, err := s.GetLatestSucceededIngestRun(ctx, "price-reference")
	if err != nil || got.ID != ok || got.FinishedAt == nil {
		t.Errorf("GetLatestSucceededIngestRun() = %+v, %v; want run %d", got, err, ok)
	}
}
