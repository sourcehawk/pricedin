//go:build integration

package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/store"
	"github.com/sourcehawk/pricedin/server/internal/store/pgtest"
)

func date(t *testing.T, s string) domain.Date {
	t.Helper()
	d, err := domain.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func datePtr(t *testing.T, s string) *domain.Date {
	t.Helper()
	d := date(t, s)
	return &d
}

// newStore returns a store and its pool over a fresh database.
func newStore(t *testing.T) (*store.Store, *pgxpool.Pool) {
	t.Helper()
	pool := pgtest.NewPool(t)
	return store.New(pool), pool
}

func countRows(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func mustUpsertCompanies(t *testing.T, s *store.Store, companies ...domain.Company) {
	t.Helper()
	if _, err := s.UpsertCompanies(context.Background(), companies); err != nil {
		t.Fatalf("UpsertCompanies() error = %v", err)
	}
}

func wantNotFound(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("%s error = %v, want ErrNotFound", what, err)
	}
}

var (
	acme = domain.Company{
		CIK: 1001, Name: "Acme Widgets Inc", SIC: "3420", SICDescription: "Cutlery, Handtools & General Hardware",
		FiscalYearEnd: "1231", StateOfIncorporation: "DE",
	}
	globex = domain.Company{CIK: 1002, Name: "Globex Corp", SIC: "7372", FiscalYearEnd: "0630"}
	initech = domain.Company{CIK: 1003, Name: "Initech Holdings", FiscalYearEnd: "0131"}
)
