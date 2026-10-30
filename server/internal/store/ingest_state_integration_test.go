//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

func TestGetLastSucceededIngestRunSkipsFailedAndRunningRuns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)

	_, err := s.GetLastSucceededIngestRun(ctx, "prices-daily")
	wantNotFound(t, "GetLastSucceededIngestRun() before any run", err)

	ok, err := s.StartIngestRun(ctx, "prices-daily")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishIngestRun(ctx, ok, store.RunSucceeded, map[string]int64{"bars": 3}, ""); err != nil {
		t.Fatal(err)
	}
	failed, err := s.StartIngestRun(ctx, "prices-daily")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishIngestRun(ctx, failed, store.RunFailed, nil, "boom"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartIngestRun(ctx, "prices-daily"); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetLastSucceededIngestRun(ctx, "prices-daily")
	if err != nil {
		t.Fatalf("GetLastSucceededIngestRun() error = %v", err)
	}
	if got.ID != ok || got.Stats["bars"] != 3 {
		t.Errorf("GetLastSucceededIngestRun() = run %d with %v, want run %d", got.ID, got.Stats, ok)
	}
}

func TestIngestCursorKeepsTheLastPosition(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)

	_, err := s.GetIngestCursor(ctx, "sec-daily")
	wantNotFound(t, "GetIngestCursor() before any set", err)
	for _, d := range []string{"2026-08-26", "2026-08-27"} {
		if err := s.SetIngestCursor(ctx, "sec-daily", date(t, d)); err != nil {
			t.Fatalf("SetIngestCursor(%s) error = %v", d, err)
		}
	}
	got, err := s.GetIngestCursor(ctx, "sec-daily")
	if err != nil || got != date(t, "2026-08-27") {
		t.Errorf("GetIngestCursor() = %v, %v; want 2026-08-27", got, err)
	}
}

func TestMissingGroupedPriceDatesIgnoreBarsOfOneSymbol(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	days := []domain.Date{date(t, "2026-10-27"), date(t, "2026-10-28"), date(t, "2026-10-29")}

	// A load of one symbol fills a day for that symbol only.
	if _, err := s.UpsertPriceBars(ctx, []domain.PriceBar{{
		Symbol: "NVDA", Date: days[0], Open: 1, High: 1, Low: 1, Close: 1, Volume: 1, Source: "massive",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkGroupedPricesLoaded(ctx, days[1], 12000); err != nil {
		t.Fatalf("MarkGroupedPricesLoaded() error = %v", err)
	}
	if err := s.MarkGroupedPricesLoaded(ctx, days[1], 1); err != nil {
		t.Fatalf("second MarkGroupedPricesLoaded() error = %v", err)
	}

	got, err := s.ListMissingGroupedPriceDates(ctx, days)
	if err != nil {
		t.Fatalf("ListMissingGroupedPriceDates() error = %v", err)
	}
	if diff := cmp.Diff([]domain.Date{days[0], days[2]}, got); diff != "" {
		t.Errorf("ListMissingGroupedPriceDates() (-want +got):\n%s", diff)
	}
	latest, err := s.GetLatestGroupedPriceDate(ctx)
	if err != nil || latest != days[1] {
		t.Errorf("GetLatestGroupedPriceDate() = %v, %v; want %v", latest, err, days[1])
	}
}

func TestGetLatestGroupedPriceDateWithoutLoads(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	_, err := s.GetLatestGroupedPriceDate(context.Background())
	wantNotFound(t, "GetLatestGroupedPriceDate()", err)
}

func TestPriceReferenceCheckedOnlyCountsTheFirstCheck(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)

	n, err := s.MarkPriceReferenceChecked(ctx, []string{"NVDA", "KO", "NVDA"})
	if err != nil || n != 2 {
		t.Fatalf("MarkPriceReferenceChecked() = %d, %v; want 2", n, err)
	}
	if n, err := s.MarkPriceReferenceChecked(ctx, []string{"KO"}); err != nil || n != 0 {
		t.Errorf("second MarkPriceReferenceChecked() = %d, %v; want 0", n, err)
	}
	for symbol, want := range map[string]bool{"NVDA": true, "KO": true, "AAPL": false} {
		got, err := s.IsPriceReferenceChecked(ctx, symbol)
		if err != nil || got != want {
			t.Errorf("IsPriceReferenceChecked(%s) = %v, %v; want %v", symbol, got, err, want)
		}
	}
}

func TestListCurrentTickersReturnsOneSourceOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme, globex)
	day := date(t, "2026-10-30")
	if _, err := s.SyncTickers(ctx, store.TickerSourceSEC, day, []domain.Ticker{
		{Symbol: "GLBX", CIK: globex.CIK, Exchange: "NYSE"},
		{Symbol: "ACME", CIK: acme.CIK, Exchange: "Nasdaq"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SyncTickers(ctx, "massive", day, []domain.Ticker{{Symbol: "ACME", CIK: acme.CIK, Exchange: "XNAS"}}); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListCurrentTickers(ctx, store.TickerSourceSEC)
	if err != nil {
		t.Fatalf("ListCurrentTickers() error = %v", err)
	}
	want := []domain.Ticker{
		{Symbol: "ACME", CIK: acme.CIK, Exchange: "Nasdaq", ValidFrom: day},
		{Symbol: "GLBX", CIK: globex.CIK, Exchange: "NYSE", ValidFrom: day},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ListCurrentTickers() (-want +got):\n%s", diff)
	}

	ciks, err := s.ListCompanyCIKs(ctx)
	if err != nil {
		t.Fatalf("ListCompanyCIKs() error = %v", err)
	}
	if diff := cmp.Diff([]domain.CIK{min(acme.CIK, globex.CIK), max(acme.CIK, globex.CIK)}, ciks); diff != "" {
		t.Errorf("ListCompanyCIKs() (-want +got):\n%s", diff)
	}
}

func TestQuarterlyFundamentalsKeepLabelEstimateAndRichSources(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme)
	v := store.QuarterlyValue{
		FiscalYear: 2026, FiscalQuarter: 4, Label: "Q4 FY26", Metric: "diluted_shares", Value: 2.4e9,
		Method: "weighted_average_q4", Estimate: true,
		PeriodStart: date(t, "2026-10-01"), PeriodEnd: date(t, "2026-12-31"),
		Filed: date(t, "2027-02-20"), FirstFiled: date(t, "2027-02-20"),
		Inputs: []store.SourceRef{{
			Accession: "0001001-27-000001", Form: "10-K", Filed: date(t, "2027-02-20"),
			Concept:     "WeightedAverageNumberOfDilutedSharesOutstanding",
			PeriodStart: datePtr(t, "2026-01-01"), PeriodEnd: date(t, "2026-12-31"),
			Value: 240e6, SplitRatio: 10, Scale: 1,
		}},
	}
	if err := s.ReplaceQuarterlyFundamentals(ctx, acme.CIK, []store.QuarterlyValue{v}); err != nil {
		t.Fatalf("ReplaceQuarterlyFundamentals() error = %v", err)
	}
	got, err := s.ListQuarterlyFundamentals(ctx, acme.CIK)
	if err != nil {
		t.Fatalf("ListQuarterlyFundamentals() error = %v", err)
	}
	if diff := cmp.Diff([]store.QuarterlyValue{v}, got); diff != "" {
		t.Errorf("ListQuarterlyFundamentals() (-want +got):\n%s", diff)
	}
}

func TestReplaceFundamentalConflictsKeepsOrderAndReplaces(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme)
	ref := store.SourceRef{Accession: "0001001-26-000001", Form: "10-Q", Filed: date(t, "2026-05-01"), Concept: "EarningsPerShareDiluted", Value: 4.2}
	restated := ref
	restated.Value = 0.42
	conflicts := []store.FundamentalConflict{
		{
			Kind: "unconfirmed_split", Concept: "EarningsPerShareDiluted",
			PeriodStart: datePtr(t, "2026-01-01"), PeriodEnd: date(t, "2026-03-31"),
			Original: &ref, Restated: &restated, Observed: 10, Expected: 1,
		},
		{
			Kind:  "split_evidence",
			Split: &store.ConflictSplit{Date: date(t, "2026-06-10"), From: 1, To: 10, Source: "sec", Evidence: "0001001-26-000002"},
		},
	}
	if err := s.ReplaceFundamentalConflicts(ctx, acme.CIK, conflicts); err != nil {
		t.Fatalf("ReplaceFundamentalConflicts() error = %v", err)
	}
	got, err := s.ListFundamentalConflicts(ctx, acme.CIK)
	if err != nil {
		t.Fatalf("ListFundamentalConflicts() error = %v", err)
	}
	if diff := cmp.Diff(conflicts, got); diff != "" {
		t.Errorf("ListFundamentalConflicts() (-want +got):\n%s", diff)
	}

	if err := s.ReplaceFundamentalConflicts(ctx, acme.CIK, nil); err != nil {
		t.Fatalf("ReplaceFundamentalConflicts(nil) error = %v", err)
	}
	if got, err := s.ListFundamentalConflicts(ctx, acme.CIK); err != nil || len(got) != 0 {
		t.Errorf("after replacing with none: %v, %v; want no conflicts", got, err)
	}
}

func TestAddCompaniesKeepsStoredDetails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme)

	nameOnly := domain.Company{CIK: acme.CIK, Name: "ACME (from the ticker map)"}
	other := domain.Company{CIK: globex.CIK, Name: globex.Name}
	n, err := s.AddCompanies(ctx, []domain.Company{nameOnly, other})
	if err != nil || n != 1 {
		t.Fatalf("AddCompanies() = %d, %v; want 1 added", n, err)
	}
	got, err := s.GetCompany(ctx, acme.CIK)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(acme, got); diff != "" {
		t.Errorf("stored company changed (-want +got):\n%s", diff)
	}
	if n, err := s.AddCompanies(ctx, []domain.Company{other}); err != nil || n != 0 {
		t.Errorf("second AddCompanies() = %d, %v; want 0", n, err)
	}
}
