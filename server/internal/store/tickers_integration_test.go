//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

func mustSyncTickers(t *testing.T, s *store.Store, source string, asOf domain.Date, current ...domain.Ticker) store.TickerChanges {
	t.Helper()
	changes, err := s.SyncTickers(context.Background(), source, asOf, current)
	if err != nil {
		t.Fatalf("SyncTickers(%s, %s) error = %v", source, asOf, err)
	}
	return changes
}

func TestSyncTickersOpensACurrentRowPerSymbol(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme, globex)
	day := date(t, "2026-01-02")

	changes := mustSyncTickers(t, s, store.TickerSourceSEC, day,
		domain.Ticker{Symbol: "ACME", CIK: acme.CIK, Exchange: "NYSE"},
		domain.Ticker{Symbol: "GBX", CIK: globex.CIK, Exchange: "Nasdaq"},
	)
	if diff := cmp.Diff(store.TickerChanges{Opened: 2}, changes); diff != "" {
		t.Errorf("changes (-want +got):\n%s", diff)
	}
	got, err := s.GetCurrentTicker(ctx, "acme")
	if err != nil {
		t.Fatalf("GetCurrentTicker() error = %v", err)
	}
	want := domain.Ticker{Symbol: "ACME", CIK: acme.CIK, Exchange: "NYSE", ValidFrom: day}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("GetCurrentTicker() (-want +got):\n%s", diff)
	}
}

func TestSyncTickersTwiceChangesNothing(t *testing.T) {
	t.Parallel()
	s, pool := newStore(t)
	mustUpsertCompanies(t, s, acme)
	ticker := domain.Ticker{Symbol: "ACME", CIK: acme.CIK, Exchange: "NYSE"}
	mustSyncTickers(t, s, store.TickerSourceSEC, date(t, "2026-01-02"), ticker)

	for _, day := range []string{"2026-01-02", "2026-01-05"} {
		if changes := mustSyncTickers(t, s, store.TickerSourceSEC, date(t, day), ticker); changes != (store.TickerChanges{}) {
			t.Errorf("SyncTickers() on %s again = %+v, want no changes", day, changes)
		}
	}
	if n := countRows(t, pool, "tickers"); n != 1 {
		t.Errorf("tickers rows = %d, want 1", n)
	}
}

func TestSyncTickersClosesTheOldRowWhenASymbolMovesToAnotherCompany(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme, globex)
	day1, day2 := date(t, "2026-01-02"), date(t, "2026-03-02")
	mustSyncTickers(t, s, store.TickerSourceSEC, day1, domain.Ticker{Symbol: "ACME", CIK: acme.CIK})

	changes := mustSyncTickers(t, s, store.TickerSourceSEC, day2, domain.Ticker{Symbol: "ACME", CIK: globex.CIK})
	if diff := cmp.Diff(store.TickerChanges{Opened: 1, Closed: 1}, changes); diff != "" {
		t.Errorf("changes (-want +got):\n%s", diff)
	}
	history, err := s.ListTickers(ctx, acme.CIK)
	if err != nil {
		t.Fatalf("ListTickers() error = %v", err)
	}
	wantHistory := []store.TickerRecord{{
		Ticker: domain.Ticker{Symbol: "ACME", CIK: acme.CIK, ValidFrom: day1, ValidTo: &day2},
		Source: store.TickerSourceSEC,
	}}
	if diff := cmp.Diff(wantHistory, history); diff != "" {
		t.Errorf("ListTickers(acme) (-want +got):\n%s", diff)
	}
	current, err := s.GetCurrentTicker(ctx, "ACME")
	if err != nil {
		t.Fatalf("GetCurrentTicker() error = %v", err)
	}
	if current.CIK != globex.CIK || current.ValidFrom != day2 {
		t.Errorf("GetCurrentTicker() = %+v, want CIK %d from %s", current, globex.CIK, day2)
	}
}

func TestSyncTickersClosesASymbolThatIsNoLongerListed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme, globex)
	mustSyncTickers(t, s, store.TickerSourceSEC, date(t, "2026-01-02"),
		domain.Ticker{Symbol: "ACME", CIK: acme.CIK},
		domain.Ticker{Symbol: "GBX", CIK: globex.CIK},
	)

	changes := mustSyncTickers(t, s, store.TickerSourceSEC, date(t, "2026-01-05"), domain.Ticker{Symbol: "GBX", CIK: globex.CIK})
	if diff := cmp.Diff(store.TickerChanges{Closed: 1}, changes); diff != "" {
		t.Errorf("changes (-want +got):\n%s", diff)
	}
	_, err := s.GetCurrentTicker(ctx, "ACME")
	wantNotFound(t, "GetCurrentTicker(ACME)", err)
}

func TestSyncTickersOnTheSameDayUpdatesTheRowInPlace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, pool := newStore(t)
	mustUpsertCompanies(t, s, acme, globex)
	day := date(t, "2026-01-02")
	mustSyncTickers(t, s, store.TickerSourceSEC, day,
		domain.Ticker{Symbol: "ACME", CIK: acme.CIK, Exchange: "NYSE"},
		domain.Ticker{Symbol: "GBX", CIK: globex.CIK},
	)

	changes := mustSyncTickers(t, s, store.TickerSourceSEC, day, domain.Ticker{Symbol: "ACME", CIK: acme.CIK, Exchange: "Nasdaq"})
	if diff := cmp.Diff(store.TickerChanges{Updated: 1, Closed: 1}, changes); diff != "" {
		t.Errorf("changes (-want +got):\n%s", diff)
	}
	got, err := s.GetCurrentTicker(ctx, "ACME")
	if err != nil {
		t.Fatalf("GetCurrentTicker() error = %v", err)
	}
	if got.Exchange != "Nasdaq" || got.ValidFrom != day {
		t.Errorf("GetCurrentTicker() = %+v, want exchange Nasdaq from %s", got, day)
	}
	// A row that opened and closed on the same day never held, so it goes.
	if n := countRows(t, pool, "tickers"); n != 1 {
		t.Errorf("tickers rows = %d, want 1", n)
	}
}

func TestSyncTickersRejectsADateBeforeAnOpenRow(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme)
	mustSyncTickers(t, s, store.TickerSourceSEC, date(t, "2026-03-02"), domain.Ticker{Symbol: "ACME", CIK: acme.CIK})

	_, err := s.SyncTickers(context.Background(), store.TickerSourceSEC, date(t, "2026-01-02"), []domain.Ticker{{Symbol: "ACME", CIK: acme.CIK}})
	if err == nil {
		t.Error("SyncTickers() with an earlier date error = nil, want an error")
	}
}

func TestSyncTickersKeepsEachSourceApart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme, globex)
	day := date(t, "2026-01-02")
	mustSyncTickers(t, s, "massive", day, domain.Ticker{Symbol: "ACME", CIK: globex.CIK})
	mustSyncTickers(t, s, store.TickerSourceSEC, day, domain.Ticker{Symbol: "ACME", CIK: acme.CIK})

	// An empty list from one source closes only that source's rows.
	if changes := mustSyncTickers(t, s, "other", day); changes != (store.TickerChanges{}) {
		t.Errorf("SyncTickers(other, empty) = %+v, want no changes", changes)
	}
	got, err := s.GetCurrentTicker(ctx, "ACME")
	if err != nil {
		t.Fatalf("GetCurrentTicker() error = %v", err)
	}
	if got.CIK != acme.CIK {
		t.Errorf("GetCurrentTicker() CIK = %d, want the SEC mapping %d", got.CIK, acme.CIK)
	}
}

func TestSearchTickers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme, globex, initech)
	day := date(t, "2026-01-02")
	mustSyncTickers(t, s, store.TickerSourceSEC, day,
		domain.Ticker{Symbol: "AC", CIK: initech.CIK},
		domain.Ticker{Symbol: "ACME", CIK: acme.CIK},
		domain.Ticker{Symbol: "GBX", CIK: globex.CIK},
	)
	// Initech has derived quarters, so its data counts as loaded.
	if err := s.ReplaceQuarterlyFundamentals(ctx, initech.CIK, []store.QuarterlyValue{{
		FiscalYear: 2026, FiscalQuarter: 1, Metric: "eps_diluted", Value: 1, Method: "reported",
		PeriodStart: date(t, "2026-02-01"), PeriodEnd: date(t, "2026-04-30"), Filed: date(t, "2026-05-30"),
	}}); err != nil {
		t.Fatalf("ReplaceQuarterlyFundamentals() error = %v", err)
	}

	match := func(symbol string, c domain.Company, loaded bool) store.TickerMatch {
		return store.TickerMatch{Ticker: domain.Ticker{Symbol: symbol, CIK: c.CIK, ValidFrom: day}, Name: c.Name, Loaded: loaded}
	}
	tests := []struct {
		name  string
		query string
		limit int
		want  []store.TickerMatch
	}{
		{"exact symbol first, then prefix", "ac", 10, []store.TickerMatch{match("AC", initech, true), match("ACME", acme, false)}},
		{"name substring, any case", "WIDGET", 10, []store.TickerMatch{match("ACME", acme, false)}},
		{"limit", "ac", 1, []store.TickerMatch{match("AC", initech, true)}},
		{"like wildcards match literally", "%", 10, []store.TickerMatch{}},
		{"empty query", "  ", 10, []store.TickerMatch{}},
		{"no match", "zzz", 10, []store.TickerMatch{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.SearchTickers(ctx, tt.query, tt.limit)
			if err != nil {
				t.Fatalf("SearchTickers(%q) error = %v", tt.query, err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("SearchTickers(%q) (-want +got):\n%s", tt.query, diff)
			}
		})
	}
}
