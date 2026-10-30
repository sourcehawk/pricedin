//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

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

func TestFundamentalsPendingKeepsAMarkThatALaterChangeRaised(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme, globex)
	if _, err := s.SyncTickers(ctx, store.TickerSourceSEC, date(t, "2026-10-30"), []domain.Ticker{{Symbol: "GLBX", CIK: globex.CIK, Exchange: "NYSE"}}); err != nil {
		t.Fatal(err)
	}

	// A CIK that is not a stored company is skipped.
	if err := s.MarkFundamentalsPending(ctx, []domain.CIK{acme.CIK, 999999}); err != nil {
		t.Fatalf("MarkFundamentalsPending() error = %v", err)
	}
	if err := s.MarkFundamentalsPendingForSymbols(ctx, []string{"GLBX", "NOPE"}); err != nil {
		t.Fatalf("MarkFundamentalsPendingForSymbols() error = %v", err)
	}
	pending, err := s.ListPendingFundamentals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]domain.CIK{min(acme.CIK, globex.CIK), max(acme.CIK, globex.CIK)}, pending); diff != "" {
		t.Errorf("ListPendingFundamentals() (-want +got):\n%s", diff)
	}

	v, ok, err := s.GetFundamentalsPending(ctx, acme.CIK)
	if err != nil || !ok {
		t.Fatalf("GetFundamentalsPending() = %d, %v, %v", v, ok, err)
	}
	// A change after the rebuild read the version raises it.
	if err := s.MarkFundamentalsPending(ctx, []domain.CIK{acme.CIK}); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearFundamentalsPending(ctx, acme.CIK, v); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetFundamentalsPending(ctx, acme.CIK); !ok {
		t.Error("the mark of a later change was cleared")
	}
	v2, _, _ := s.GetFundamentalsPending(ctx, acme.CIK)
	if err := s.ClearFundamentalsPending(ctx, acme.CIK, v2); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetFundamentalsPending(ctx, acme.CIK); ok {
		t.Error("the mark stayed after a clear with its version")
	}
}

func TestBarsThatReachASplitMarkTheCompanyPending(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme, globex)
	if _, err := s.SyncTickers(ctx, store.TickerSourceSEC, date(t, "2026-10-30"), []domain.Ticker{
		{Symbol: "ACME", CIK: acme.CIK, Exchange: "NYSE"}, {Symbol: "GLBX", CIK: globex.CIK, Exchange: "NYSE"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertPriceSplits(ctx, []domain.Split{
		{Symbol: "ACME", Date: date(t, "2026-10-28"), From: 1, To: 2, Source: "massive", Evidence: "a"},
		{Symbol: "GLBX", Date: date(t, "2024-06-10"), From: 1, To: 10, Source: "massive", Evidence: "b"},
	}); err != nil {
		t.Fatal(err)
	}
	// An SEC split that the provider does not list.
	if _, err := s.InsertStockSplits(ctx, []domain.Split{
		{CIK: globex.CIK, Date: date(t, "2026-10-29"), From: 1, To: 3, Source: "sec", Evidence: "0001002-26-000001"},
	}); err != nil {
		t.Fatal(err)
	}
	bar := func(symbol, d string) domain.PriceBar {
		return domain.PriceBar{Symbol: symbol, Date: date(t, d), Open: 1, High: 1, Low: 1, Close: 1, Volume: 1, Source: "massive"}
	}
	if _, err := s.UpsertPriceBars(ctx, []domain.PriceBar{bar("ACME", "2026-10-27"), bar("GLBX", "2026-10-27")}); err != nil {
		t.Fatal(err)
	}

	// The bars of 2026-10-29 reach ACME's provider split and GLBX's SEC
	// split, which the stored bars did not; GLBX's provider split is older
	// than its stored bars.
	if err := s.MarkFundamentalsPendingForSplitsReached(ctx, []domain.PriceBar{bar("ACME", "2026-10-29"), bar("GLBX", "2026-10-29")}); err != nil {
		t.Fatalf("MarkFundamentalsPendingForSplitsReached() error = %v", err)
	}
	pending, err := s.ListPendingFundamentals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]domain.CIK{min(acme.CIK, globex.CIK), max(acme.CIK, globex.CIK)}, pending); diff != "" {
		t.Errorf("pending (-want +got):\n%s", diff)
	}
	// Bars that stop before a split mark nothing.
	for _, c := range []domain.CIK{acme.CIK, globex.CIK} {
		if err := s.ClearFundamentalsPending(ctx, c, 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.MarkFundamentalsPendingForSplitsReached(ctx, []domain.PriceBar{bar("ACME", "2026-10-27"), bar("GLBX", "2026-10-28")}); err != nil {
		t.Fatal(err)
	}
	if pending, _ := s.ListPendingFundamentals(ctx); len(pending) != 0 {
		t.Errorf("pending after bars before the split = %v, want none", pending)
	}
}

func TestGetEarliestGroupedPriceDate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	_, err := s.GetEarliestGroupedPriceDate(ctx)
	wantNotFound(t, "GetEarliestGroupedPriceDate() without loads", err)
	for _, d := range []string{"2026-10-28", "2026-10-26"} {
		if err := s.MarkGroupedPricesLoaded(ctx, date(t, d), 1); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := s.GetEarliestGroupedPriceDate(ctx); err != nil || got != date(t, "2026-10-26") {
		t.Errorf("GetEarliestGroupedPriceDate() = %v, %v; want 2026-10-26", got, err)
	}
}

// A bar write waits for a rebuild that holds its company lock, so the
// rebuild cannot read the newest bar, store a split, and clear the mark
// around the write.
func TestABarWriteWaitsForARunningRebuild(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme)

	locked := make(chan struct{})
	release := make(chan struct{})
	rebuildDone := make(chan error, 1)
	go func() {
		rebuildDone <- s.WithTx(ctx, func(tx *store.Store) error {
			if err := tx.LockCompanyFundamentals(ctx, acme.CIK); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked

	written := make(chan error, 1)
	go func() {
		written <- s.WithTx(ctx, func(tx *store.Store) error { return tx.LockPriceBarWrites(ctx) })
	}()
	select {
	case err := <-written:
		t.Fatalf("the bar write ran during the rebuild (error %v)", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	if err := <-rebuildDone; err != nil {
		t.Fatal(err)
	}
	if err := <-written; err != nil {
		t.Errorf("bar write after the rebuild: %v", err)
	}

	// Rebuilds of two companies hold their locks at the same time.
	got := make(chan struct{}, 2)
	hold := make(chan struct{})
	done := make(chan error, 2)
	for _, cik := range []domain.CIK{acme.CIK, acme.CIK + 1} {
		go func() {
			done <- s.WithTx(ctx, func(tx *store.Store) error {
				if err := tx.LockCompanyFundamentals(ctx, cik); err != nil {
					return err
				}
				got <- struct{}{}
				<-hold
				return nil
			})
		}()
	}
	for range 2 {
		select {
		case <-got:
		case <-time.After(5 * time.Second):
			close(hold)
			t.Fatal("two rebuilds of different companies blocked each other")
		}
	}
	close(hold)
	for range 2 {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
}

func TestStartIngestRunForLockMarksAnOlderRunningRunAbandoned(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)

	dead, abandoned, err := s.StartIngestRunForLock(ctx, "prices-daily", "prices-daily")
	if err != nil || abandoned != 0 {
		t.Fatalf("first StartIngestRunForLock() = %d, %d, %v", dead, abandoned, err)
	}
	other, _, err := s.StartIngestRunForLock(ctx, "sec-company", "sec-company:0000000001")
	if err != nil {
		t.Fatal(err)
	}
	// The process of the first run died; the next holder of its lock starts.
	next, abandoned, err := s.StartIngestRunForLock(ctx, "prices-daily", "prices-daily")
	if err != nil || abandoned != 1 {
		t.Fatalf("second StartIngestRunForLock() = %d, %d, %v; want one abandoned run", next, abandoned, err)
	}
	got, err := s.GetIngestRun(ctx, dead)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.RunFailed || got.Error != "abandoned" || got.FinishedAt == nil {
		t.Errorf("abandoned run = %+v, want failed with error abandoned", got)
	}
	for _, id := range []int64{other, next} {
		if r, _ := s.GetIngestRun(ctx, id); r.Status != store.RunRunning {
			t.Errorf("run %d = %s, want running", id, r.Status)
		}
	}
}

func TestSetIngestCursorIfMissingKeepsAStoredCursor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	if ok, err := s.SetIngestCursorIfMissing(ctx, "sec-daily", date(t, "2026-10-30")); err != nil || !ok {
		t.Fatalf("SetIngestCursorIfMissing() = %v, %v; want stored", ok, err)
	}
	if ok, err := s.SetIngestCursorIfMissing(ctx, "sec-daily", date(t, "2026-11-02")); err != nil || ok {
		t.Errorf("second SetIngestCursorIfMissing() = %v, %v; want not stored", ok, err)
	}
	if got, _ := s.GetIngestCursor(ctx, "sec-daily"); got != date(t, "2026-10-30") {
		t.Errorf("cursor = %s, want 2026-10-30", got)
	}
}

func TestSECRetriesWaitAndStopAfterTheLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme, globex)
	t0 := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

	if n, err := s.RecordSECRetryFailure(ctx, acme.CIK, "boom", t0, 6*time.Hour, 24*time.Hour); err != nil || n != 1 {
		t.Fatalf("RecordSECRetryFailure() = %d, %v; want 1 attempt", n, err)
	}
	if n, _ := s.RecordSECRetryFailure(ctx, globex.CIK, "boom", t0, time.Hour, 24*time.Hour); n != 1 {
		t.Fatal("want 1 attempt")
	}
	// The second failure waits twice as long: 2 hours.
	if n, _ := s.RecordSECRetryFailure(ctx, globex.CIK, "boom again", t0.Add(time.Hour), time.Hour, 24*time.Hour); n != 2 {
		t.Fatal("want 2 attempts")
	}
	if due, _ := s.ListDueSECRetries(ctx, t0.Add(2*time.Hour+59*time.Minute), 3); len(due) != 0 {
		t.Errorf("due before the doubled wait = %v, want none", due)
	}

	due, err := s.ListDueSECRetries(ctx, t0.Add(3*time.Hour), 3)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]domain.CIK{globex.CIK}, due); diff != "" {
		t.Errorf("due after the doubled wait (-want +got):\n%s", diff)
	}
	// After the limit, a company is no longer due but stays listed.
	if due, _ := s.ListDueSECRetries(ctx, t0.Add(7*time.Hour), 2); len(due) != 1 || due[0] != acme.CIK {
		t.Errorf("due with a limit of 2 = %v, want only ACME", due)
	}
	waiting, exhausted, err := s.CountSECRetries(ctx, 2)
	if err != nil || waiting != 1 || exhausted != 1 {
		t.Errorf("CountSECRetries() = %d, %d, %v; want 1 waiting and 1 exhausted", waiting, exhausted, err)
	}
	if err := s.DeleteSECRetry(ctx, acme.CIK); err != nil {
		t.Fatal(err)
	}
	if waiting, _, _ := s.CountSECRetries(ctx, 2); waiting != 0 {
		t.Errorf("waiting after the delete = %d, want 0", waiting)
	}
}

func TestTrackedCompaniesListTheirCurrentSECTickers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme, globex)
	day := date(t, "2026-10-30")
	if _, err := s.SyncTickers(ctx, store.TickerSourceSEC, day, []domain.Ticker{
		{Symbol: "ACME", CIK: acme.CIK, Exchange: "OTC"}, {Symbol: "GLBX", CIK: globex.CIK, Exchange: "NYSE"},
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.AddTrackedCompany(ctx, acme.CIK); err != nil {
			t.Fatalf("AddTrackedCompany() error = %v", err)
		}
	}
	got, err := s.ListTrackedTickers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]domain.Ticker{{Symbol: "ACME", CIK: acme.CIK, Exchange: "OTC", ValidFrom: day}}, got); diff != "" {
		t.Errorf("ListTrackedTickers() (-want +got):\n%s", diff)
	}
}
