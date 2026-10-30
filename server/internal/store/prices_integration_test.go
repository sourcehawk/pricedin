//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

func bar(t *testing.T, symbol, day string, closePrice float64) domain.PriceBar {
	t.Helper()
	return domain.PriceBar{
		Symbol: symbol, Date: date(t, day),
		Open: closePrice - 1, High: closePrice + 2, Low: closePrice - 2, Close: closePrice,
		Volume: 1_000_000.5, Source: "massive",
	}
}

func mustUpsertBars(t *testing.T, s *store.Store, bars ...domain.PriceBar) {
	t.Helper()
	if _, err := s.UpsertPriceBars(context.Background(), bars); err != nil {
		t.Fatalf("UpsertPriceBars() error = %v", err)
	}
}

func TestUpsertPriceBarsTwiceLeavesTheRowCountUnchanged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, pool := newStore(t)
	batch := []domain.PriceBar{bar(t, "ACME", "2026-01-02", 10), bar(t, "ACME", "2026-01-05", 11), bar(t, "GBX", "2026-01-02", 50)}

	n, err := s.UpsertPriceBars(ctx, batch)
	if err != nil || n != 3 {
		t.Fatalf("first UpsertPriceBars() = %d, %v; want 3, nil", n, err)
	}
	n, err = s.UpsertPriceBars(ctx, batch)
	if err != nil || n != 0 {
		t.Fatalf("second UpsertPriceBars() = %d, %v; want 0, nil", n, err)
	}
	if got := countRows(t, pool, "prices_daily"); got != 3 {
		t.Errorf("prices_daily rows = %d, want 3", got)
	}
}

func TestUpsertPriceBarsTakesTheProvidersCorrection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertBars(t, s, bar(t, "ACME", "2026-01-02", 10), bar(t, "ACME", "2026-01-05", 11))
	corrected := bar(t, "ACME", "2026-01-02", 10.5)

	n, err := s.UpsertPriceBars(ctx, []domain.PriceBar{corrected, bar(t, "ACME", "2026-01-05", 11)})
	if err != nil || n != 1 {
		t.Fatalf("UpsertPriceBars() with one correction = %d, %v; want 1, nil", n, err)
	}
	got, err := s.ListPriceBars(ctx, "ACME", date(t, "2026-01-02"), date(t, "2026-01-02"))
	if err != nil {
		t.Fatalf("ListPriceBars() error = %v", err)
	}
	if diff := cmp.Diff([]domain.PriceBar{corrected}, got); diff != "" {
		t.Errorf("ListPriceBars() (-want +got):\n%s", diff)
	}
}

func TestUpsertPriceBarsKeepsTheLastCopyOfADayInOneBatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	last := bar(t, "ACME", "2026-01-02", 12)

	n, err := s.UpsertPriceBars(ctx, []domain.PriceBar{bar(t, "ACME", "2026-01-02", 10), last})
	if err != nil || n != 1 {
		t.Fatalf("UpsertPriceBars() = %d, %v; want 1, nil", n, err)
	}
	got, err := s.ListPriceBars(ctx, "ACME", last.Date, last.Date)
	if err != nil {
		t.Fatalf("ListPriceBars() error = %v", err)
	}
	if diff := cmp.Diff([]domain.PriceBar{last}, got); diff != "" {
		t.Errorf("ListPriceBars() (-want +got):\n%s", diff)
	}
}

func TestListPriceBarsReturnsTheSymbolsRangeInDateOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	b1, b2, b3 := bar(t, "ACME", "2026-01-02", 10), bar(t, "ACME", "2026-01-05", 11), bar(t, "ACME", "2026-01-06", 12)
	mustUpsertBars(t, s, b3, bar(t, "GBX", "2026-01-05", 50), b1, b2)

	got, err := s.ListPriceBars(ctx, "ACME", date(t, "2026-01-02"), date(t, "2026-01-05"))
	if err != nil {
		t.Fatalf("ListPriceBars() error = %v", err)
	}
	if diff := cmp.Diff([]domain.PriceBar{b1, b2}, got); diff != "" {
		t.Errorf("ListPriceBars() (-want +got):\n%s", diff)
	}
	got, err = s.ListPriceBars(ctx, "NOPE", date(t, "2026-01-01"), date(t, "2026-12-31"))
	if err != nil || len(got) != 0 {
		t.Errorf("ListPriceBars(unknown symbol) = %v, %v; want empty, nil", got, err)
	}
}

func TestListAdjustedPriceBarsDividesByEveryLaterSplit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	// The NVDA closes around its 10-for-1 split, plus an earlier 4-for-1.
	beforeBoth := domain.PriceBar{Symbol: "NVDA", Date: date(t, "2021-07-19"), Open: 800, High: 820, Low: 780, Close: 800, Volume: 100, Source: "massive"}
	beforeSplit := domain.PriceBar{Symbol: "NVDA", Date: date(t, "2024-06-07"), Open: 1200, High: 1216, Low: 1180, Close: 1208.88, Volume: 40_000_000, Source: "massive"}
	splitDay := domain.PriceBar{Symbol: "NVDA", Date: date(t, "2024-06-10"), Open: 120, High: 123, Low: 117, Close: 121.79, Volume: 300_000_000, Source: "massive"}
	mustUpsertBars(t, s, beforeBoth, beforeSplit, splitDay, bar(t, "ACME", "2024-06-07", 10))
	if _, err := s.UpsertPriceSplits(ctx, []domain.Split{
		{Symbol: "NVDA", Date: date(t, "2024-06-10"), From: 1, To: 10, Source: "massive", Evidence: "E1"},
		{Symbol: "NVDA", Date: date(t, "2021-07-20"), From: 1, To: 4, Source: "massive", Evidence: "E2"},
	}); err != nil {
		t.Fatalf("UpsertPriceSplits() error = %v", err)
	}

	got, err := s.ListAdjustedPriceBars(ctx, "NVDA", date(t, "2021-01-01"), date(t, "2024-12-31"))
	if err != nil {
		t.Fatalf("ListAdjustedPriceBars() error = %v", err)
	}
	want := []store.AdjustedBar{
		{
			PriceBar:    domain.PriceBar{Symbol: "NVDA", Date: beforeBoth.Date, Open: 20, High: 20.5, Low: 19.5, Close: 20, Volume: 4000, Source: "massive"},
			RawClose:    800,
			SplitFactor: 40,
		},
		{
			PriceBar:    domain.PriceBar{Symbol: "NVDA", Date: beforeSplit.Date, Open: 120, High: 121.6, Low: 118, Close: 120.888, Volume: 400_000_000, Source: "massive"},
			RawClose:    1208.88,
			SplitFactor: 10,
		},
		{PriceBar: splitDay, RawClose: 121.79, SplitFactor: 1},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ListAdjustedPriceBars() (-want +got):\n%s", diff)
	}

	other, err := s.ListAdjustedPriceBars(ctx, "ACME", date(t, "2024-06-07"), date(t, "2024-06-07"))
	if err != nil || len(other) != 1 || other[0].SplitFactor != 1 {
		t.Errorf("ListAdjustedPriceBars(ACME) = %v, %v; want one bar with factor 1", other, err)
	}
}

func TestLatestPriceDate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)

	_, err := s.GetLatestPriceDateOverall(ctx)
	wantNotFound(t, "GetLatestPriceDateOverall() with no bars", err)

	mustUpsertBars(t, s, bar(t, "ACME", "2026-01-02", 10), bar(t, "ACME", "2026-01-05", 11), bar(t, "GBX", "2026-01-06", 50))

	got, err := s.GetLatestPriceDate(ctx, "ACME")
	if err != nil || got != date(t, "2026-01-05") {
		t.Errorf("GetLatestPriceDate(ACME) = %s, %v; want 2026-01-05, nil", got, err)
	}
	_, err = s.GetLatestPriceDate(ctx, "NOPE")
	wantNotFound(t, "GetLatestPriceDate(NOPE)", err)
	got, err = s.GetLatestPriceDateOverall(ctx)
	if err != nil || got != date(t, "2026-01-06") {
		t.Errorf("GetLatestPriceDateOverall() = %s, %v; want 2026-01-06, nil", got, err)
	}
}

func TestListMissingPriceDates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertBars(t, s, bar(t, "ACME", "2026-01-05", 11), bar(t, "GBX", "2026-01-06", 50))
	trading := []domain.Date{date(t, "2026-01-07"), date(t, "2026-01-02"), date(t, "2026-01-05"), date(t, "2026-01-06"), date(t, "2026-01-02")}

	got, err := s.ListMissingPriceDates(ctx, trading)
	if err != nil {
		t.Fatalf("ListMissingPriceDates() error = %v", err)
	}
	if diff := cmp.Diff([]domain.Date{date(t, "2026-01-02"), date(t, "2026-01-07")}, got); diff != "" {
		t.Errorf("ListMissingPriceDates() (-want +got):\n%s", diff)
	}

	got, err = s.ListMissingPriceDatesForSymbol(ctx, "ACME", trading)
	if err != nil {
		t.Fatalf("ListMissingPriceDatesForSymbol() error = %v", err)
	}
	if diff := cmp.Diff([]domain.Date{date(t, "2026-01-02"), date(t, "2026-01-06"), date(t, "2026-01-07")}, got); diff != "" {
		t.Errorf("ListMissingPriceDatesForSymbol(ACME) (-want +got):\n%s", diff)
	}

	got, err = s.ListMissingPriceDates(ctx, nil)
	if err != nil || len(got) != 0 {
		t.Errorf("ListMissingPriceDates(nil) = %v, %v; want empty, nil", got, err)
	}
}

func TestPriceSplits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	tenForOne := domain.Split{Symbol: "NVDA", Date: date(t, "2024-06-10"), From: 1, To: 10, Source: "massive", Evidence: "P1"}
	reverse := domain.Split{Symbol: "NVDA", Date: date(t, "2001-01-02"), From: 4, To: 1, Source: "massive", Evidence: "P0"}

	n, err := s.UpsertPriceSplits(ctx, []domain.Split{tenForOne, reverse, {Symbol: "ACME", Date: date(t, "2020-01-02"), From: 1, To: 2, Source: "massive"}})
	if err != nil || n != 3 {
		t.Fatalf("UpsertPriceSplits() = %d, %v; want 3, nil", n, err)
	}
	n, err = s.UpsertPriceSplits(ctx, []domain.Split{tenForOne})
	if err != nil || n != 0 {
		t.Fatalf("repeated UpsertPriceSplits() = %d, %v; want 0, nil", n, err)
	}
	corrected := tenForOne
	corrected.To = 8
	n, err = s.UpsertPriceSplits(ctx, []domain.Split{corrected})
	if err != nil || n != 1 {
		t.Fatalf("UpsertPriceSplits() with a correction = %d, %v; want 1, nil", n, err)
	}

	got, err := s.ListPriceSplits(ctx, "NVDA")
	if err != nil {
		t.Fatalf("ListPriceSplits() error = %v", err)
	}
	if diff := cmp.Diff([]domain.Split{reverse, corrected}, got); diff != "" {
		t.Errorf("ListPriceSplits() (-want +got):\n%s", diff)
	}

	if _, err := s.UpsertPriceSplits(ctx, []domain.Split{{Symbol: "BAD", Date: date(t, "2020-01-02"), From: 0, To: 2, Source: "massive"}}); err == nil {
		t.Error("UpsertPriceSplits() with From 0 error = nil, want an error")
	}
}

func TestDividends(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	regular := domain.Dividend{
		Symbol: "KO", ExDate: date(t, "2026-03-13"), PayDate: datePtr(t, "2026-04-01"), DeclarationDate: datePtr(t, "2026-02-12"),
		CashAmount: 0.51, Currency: "USD", Frequency: 4, Type: "CD", Source: "massive", ProviderID: "D2",
	}
	special := domain.Dividend{Symbol: "KO", ExDate: date(t, "2025-12-01"), CashAmount: 1, Currency: "USD", Type: "SC", Source: "massive", ProviderID: "D1"}

	n, err := s.UpsertDividends(ctx, []domain.Dividend{regular, special, {Symbol: "PEP", ExDate: date(t, "2026-03-01"), CashAmount: 1.3, Source: "massive", ProviderID: "D3"}})
	if err != nil || n != 3 {
		t.Fatalf("UpsertDividends() = %d, %v; want 3, nil", n, err)
	}
	n, err = s.UpsertDividends(ctx, []domain.Dividend{regular, special})
	if err != nil || n != 0 {
		t.Fatalf("repeated UpsertDividends() = %d, %v; want 0, nil", n, err)
	}
	corrected := regular
	corrected.CashAmount = 0.52
	n, err = s.UpsertDividends(ctx, []domain.Dividend{corrected})
	if err != nil || n != 1 {
		t.Fatalf("UpsertDividends() with a correction = %d, %v; want 1, nil", n, err)
	}

	got, err := s.ListDividends(ctx, "KO")
	if err != nil {
		t.Fatalf("ListDividends() error = %v", err)
	}
	if diff := cmp.Diff([]domain.Dividend{special, corrected}, got); diff != "" {
		t.Errorf("ListDividends() (-want +got):\n%s", diff)
	}
}

func TestListAdjustedPriceBarsIgnoresASplitUntilABarOnOrAfterItExists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	before := domain.PriceBar{Symbol: "NVDA", Date: date(t, "2024-06-07"), Open: 1200, High: 1216, Low: 1180, Close: 1208.88, Volume: 100, Source: "massive"}
	mustUpsertBars(t, s, before)
	// An announced split whose execution date is after the newest bar.
	if _, err := s.UpsertPriceSplits(ctx, []domain.Split{{Symbol: "NVDA", Date: date(t, "2024-06-10"), From: 1, To: 10, Source: "massive"}}); err != nil {
		t.Fatalf("UpsertPriceSplits() error = %v", err)
	}

	factor := func() float64 {
		t.Helper()
		got, err := s.ListAdjustedPriceBars(ctx, "NVDA", before.Date, before.Date)
		if err != nil || len(got) != 1 {
			t.Fatalf("ListAdjustedPriceBars() = %v, %v; want one bar", got, err)
		}
		return got[0].SplitFactor
	}
	if f := factor(); f != 1 {
		t.Errorf("SplitFactor before the split day has a bar = %v, want 1", f)
	}
	mustUpsertBars(t, s, domain.PriceBar{Symbol: "NVDA", Date: date(t, "2024-06-10"), Open: 120, High: 123, Low: 117, Close: 121.79, Volume: 300, Source: "massive"})
	if f := factor(); f != 10 {
		t.Errorf("SplitFactor after the split day has a bar = %v, want 10", f)
	}
}

func TestUpsertDividendsRejectsAnEmptyProviderID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, pool := newStore(t)
	dividends := []domain.Dividend{
		{Symbol: "KO", ExDate: date(t, "2026-03-13"), CashAmount: 0.51, Source: "massive", ProviderID: "D1"},
		{Symbol: "PEP", ExDate: date(t, "2026-03-01"), CashAmount: 1.3, Source: "massive"},
	}
	if _, err := s.UpsertDividends(ctx, dividends); err == nil {
		t.Fatal("UpsertDividends() with an empty provider ID error = nil, want an error")
	}
	if n := countRows(t, pool, "dividends"); n != 0 {
		t.Errorf("dividends rows = %d, want 0", n)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO dividends (provider_id, symbol, ex_dividend_date, cash_amount, source)
		VALUES ('', 'PEP', '2026-03-01', 1.3, 'massive')`); err == nil {
		t.Error("INSERT of an empty provider_id error = nil, want a check violation")
	}
}
