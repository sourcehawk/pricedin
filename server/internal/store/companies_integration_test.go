//go:build integration

package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

func TestUpsertCompaniesCountsOnlyNewAndChangedRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)

	n, err := s.UpsertCompanies(ctx, []domain.Company{acme, globex})
	if err != nil || n != 2 {
		t.Fatalf("first UpsertCompanies() = %d, %v; want 2, nil", n, err)
	}
	n, err = s.UpsertCompanies(ctx, []domain.Company{acme, globex})
	if err != nil || n != 0 {
		t.Fatalf("repeated UpsertCompanies() = %d, %v; want 0, nil", n, err)
	}
	renamed := acme
	renamed.Name = "Acme Widgets Holdings"
	n, err = s.UpsertCompanies(ctx, []domain.Company{renamed, globex})
	if err != nil || n != 1 {
		t.Fatalf("UpsertCompanies() with one change = %d, %v; want 1, nil", n, err)
	}

	got, err := s.GetCompany(ctx, acme.CIK)
	if err != nil {
		t.Fatalf("GetCompany() error = %v", err)
	}
	if diff := cmp.Diff(renamed, got); diff != "" {
		t.Errorf("GetCompany() (-want +got):\n%s", diff)
	}
}

func TestUpsertCompaniesKeepsTheLastCopyOfACIKInOneBatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	first, last := acme, acme
	first.Name = "Old name"

	if _, err := s.UpsertCompanies(ctx, []domain.Company{first, last}); err != nil {
		t.Fatalf("UpsertCompanies() error = %v", err)
	}
	got, err := s.GetCompany(ctx, acme.CIK)
	if err != nil {
		t.Fatalf("GetCompany() error = %v", err)
	}
	if got.Name != last.Name {
		t.Errorf("Name = %q, want %q", got.Name, last.Name)
	}
}

func TestUpsertCompaniesWithAnEmptyBatchWritesNothing(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	n, err := s.UpsertCompanies(context.Background(), nil)
	if err != nil || n != 0 {
		t.Errorf("UpsertCompanies(nil) = %d, %v; want 0, nil", n, err)
	}
}

func TestGetCompanyOfAnUnknownCIKIsNotFound(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	_, err := s.GetCompany(context.Background(), 4242)
	wantNotFound(t, "GetCompany()", err)
}

func TestGetCompanyBySymbolFollowsTheCurrentTicker(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme, globex)
	mustSyncTickers(t, s, store.TickerSourceSEC, date(t, "2026-01-02"), domain.Ticker{Symbol: "ACME", CIK: acme.CIK})
	mustSyncTickers(t, s, store.TickerSourceSEC, date(t, "2026-02-02"), domain.Ticker{Symbol: "ACME", CIK: globex.CIK})

	got, err := s.GetCompanyBySymbol(ctx, "acme")
	if err != nil {
		t.Fatalf("GetCompanyBySymbol() error = %v", err)
	}
	if got.CIK != globex.CIK {
		t.Errorf("GetCompanyBySymbol() CIK = %d, want %d", got.CIK, globex.CIK)
	}
	_, err = s.GetCompanyBySymbol(ctx, "NOPE")
	wantNotFound(t, "GetCompanyBySymbol(NOPE)", err)
}

func TestWithTxRollsBackWhenTheFunctionFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	errStop := context.Canceled

	err := s.WithTx(ctx, func(tx *store.Store) error {
		if _, err := tx.UpsertCompanies(ctx, []domain.Company{acme}); err != nil {
			return err
		}
		return errStop
	})
	if !errors.Is(err, errStop) {
		t.Fatalf("WithTx() error = %v, want the function's error", err)
	}
	_, err = s.GetCompany(ctx, acme.CIK)
	wantNotFound(t, "GetCompany() after rollback", err)
}

func TestWithTxCommitsWhenTheFunctionSucceeds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)

	err := s.WithTx(ctx, func(tx *store.Store) error {
		if _, err := tx.UpsertCompanies(ctx, []domain.Company{acme}); err != nil {
			return err
		}
		// A second batch in the same transaction must not collide with the
		// first one's staging table.
		_, err := tx.UpsertCompanies(ctx, []domain.Company{globex})
		return err
	})
	if err != nil {
		t.Fatalf("WithTx() error = %v", err)
	}
	for _, c := range []domain.Company{acme, globex} {
		if _, err := s.GetCompany(ctx, c.CIK); err != nil {
			t.Errorf("GetCompany(%d) after commit error = %v", c.CIK, err)
		}
	}
}
