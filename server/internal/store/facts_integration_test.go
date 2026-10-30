//go:build integration

package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

// epsFact returns a quarterly diluted EPS fact of acme.
func epsFact(t *testing.T, start, end string, value float64, filed, accession string) domain.Fact {
	t.Helper()
	return domain.Fact{
		CIK: acme.CIK, Taxonomy: "us-gaap", Concept: "EarningsPerShareDiluted", Unit: "USD/shares",
		PeriodStart: datePtr(t, start), PeriodEnd: date(t, end), Value: value,
		Form: "10-Q", FiscalYear: 2026, FiscalPeriod: "Q1", Filed: date(t, filed), Accession: accession,
	}
}

func sharesFact(t *testing.T, end string, value float64, filed, accession string) domain.Fact {
	t.Helper()
	return domain.Fact{
		CIK: acme.CIK, Taxonomy: "dei", Concept: "EntityCommonStockSharesOutstanding", Unit: "shares",
		PeriodEnd: date(t, end), Value: value,
		Form: "10-Q", FiscalYear: 2026, FiscalPeriod: "Q1", Filed: date(t, filed), Accession: accession,
	}
}

func TestInsertFactsTwiceLeavesTheRowCountUnchanged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, pool := newStore(t)
	mustUpsertCompanies(t, s, acme)
	batch := []domain.Fact{
		epsFact(t, "2026-01-01", "2026-03-31", 1.23, "2026-05-01", "0001001-26-000001"),
		epsFact(t, "2025-01-01", "2025-03-31", 1.01, "2026-05-01", "0001001-26-000001"),
		sharesFact(t, "2026-04-25", 1_234_567_890, "2026-05-01", "0001001-26-000001"),
	}

	n, err := s.InsertFacts(ctx, batch)
	if err != nil || n != 3 {
		t.Fatalf("first InsertFacts() = %d, %v; want 3, nil", n, err)
	}
	n, err = s.InsertFacts(ctx, batch)
	if err != nil || n != 0 {
		t.Fatalf("second InsertFacts() = %d, %v; want 0, nil", n, err)
	}
	if got := countRows(t, pool, "facts"); got != 3 {
		t.Errorf("facts rows = %d, want 3", got)
	}
}

func TestInsertFactsKeepsBothVersionsOfAnAmendedValue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme)
	original := epsFact(t, "2026-01-01", "2026-03-31", 1.23, "2026-05-01", "0001001-26-000001")
	amended := epsFact(t, "2026-01-01", "2026-03-31", 1.19, "2026-06-15", "0001001-26-000002")
	amended.Form = "10-Q/A"

	for _, f := range []domain.Fact{original, amended} {
		if n, err := s.InsertFacts(ctx, []domain.Fact{f}); err != nil || n != 1 {
			t.Fatalf("InsertFacts(%s) = %d, %v; want 1, nil", f.Accession, n, err)
		}
	}
	got, err := s.ListFacts(ctx, acme.CIK, store.FactQuery{})
	if err != nil {
		t.Fatalf("ListFacts() error = %v", err)
	}
	if diff := cmp.Diff([]domain.Fact{original, amended}, got); diff != "" {
		t.Errorf("ListFacts() (-want +got):\n%s", diff)
	}
}

func TestInsertFactsStoresAValueRepeatedInOneFilingOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme)
	plain := epsFact(t, "2026-01-01", "2026-03-31", 1.23, "2026-05-01", "0001001-26-000001")
	framed := plain
	framed.Frame = "CY2026Q1"

	n, err := s.InsertFacts(ctx, []domain.Fact{plain, framed})
	if err != nil || n != 1 {
		t.Fatalf("InsertFacts() = %d, %v; want 1, nil", n, err)
	}
	got, err := s.ListFacts(ctx, acme.CIK, store.FactQuery{})
	if err != nil {
		t.Fatalf("ListFacts() error = %v", err)
	}
	// The copy that carries SEC's calendar frame wins.
	if diff := cmp.Diff([]domain.Fact{framed}, got); diff != "" {
		t.Errorf("ListFacts() (-want +got):\n%s", diff)
	}
}

func TestInsertFactsKeepsTwoDifferentValuesForOnePeriodInOneFiling(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme)
	a := epsFact(t, "2026-01-01", "2026-03-31", 1.23, "2026-05-01", "0001001-26-000001")
	b := a
	b.Value = 1.24
	n, err := s.InsertFacts(context.Background(), []domain.Fact{a, b})
	if err != nil || n != 2 {
		t.Errorf("InsertFacts() = %d, %v; want 2, nil", n, err)
	}
}

func TestFactsRejectUpdateDeleteAndTruncate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, pool := newStore(t)
	mustUpsertCompanies(t, s, acme)
	if _, err := s.InsertFacts(ctx, []domain.Fact{epsFact(t, "2026-01-01", "2026-03-31", 1.23, "2026-05-01", "0001001-26-000001")}); err != nil {
		t.Fatalf("InsertFacts() error = %v", err)
	}

	for _, stmt := range []string{
		"UPDATE facts SET value = 9.99",
		"DELETE FROM facts",
		"TRUNCATE facts",
	} {
		_, err := pool.Exec(ctx, stmt)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23001" {
			t.Errorf("%s: error = %v, want restrict_violation (23001)", stmt, err)
		}
	}
	if got := countRows(t, pool, "facts"); got != 1 {
		t.Errorf("facts rows = %d, want 1", got)
	}
}

func TestListFactsAsOfIgnoresLaterFilings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme)
	original := epsFact(t, "2026-01-01", "2026-03-31", 1.23, "2026-05-01", "0001001-26-000001")
	amended := epsFact(t, "2026-01-01", "2026-03-31", 1.19, "2026-06-15", "0001001-26-000002")
	if _, err := s.InsertFacts(ctx, []domain.Fact{original, amended}); err != nil {
		t.Fatalf("InsertFacts() error = %v", err)
	}

	tests := []struct {
		asOf string
		want []domain.Fact
	}{
		{"2026-04-30", []domain.Fact{}},
		{"2026-05-01", []domain.Fact{original}},
		{"2026-06-14", []domain.Fact{original}},
		{"2026-06-15", []domain.Fact{original, amended}},
	}
	for _, tt := range tests {
		got, err := s.ListFacts(ctx, acme.CIK, store.FactQuery{AsOf: date(t, tt.asOf)})
		if err != nil {
			t.Fatalf("ListFacts(as of %s) error = %v", tt.asOf, err)
		}
		if diff := cmp.Diff(tt.want, got); diff != "" {
			t.Errorf("ListFacts(as of %s) (-want +got):\n%s", tt.asOf, diff)
		}
	}
}

func TestListFactsFiltersByConceptAndCompany(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme, globex)
	eps := epsFact(t, "2026-01-01", "2026-03-31", 1.23, "2026-05-01", "0001001-26-000001")
	shares := sharesFact(t, "2026-04-25", 1_000_000, "2026-05-01", "0001001-26-000001")
	other := eps
	other.CIK = globex.CIK
	other.Accession = "0001002-26-000001"
	if _, err := s.InsertFacts(ctx, []domain.Fact{eps, shares, other}); err != nil {
		t.Fatalf("InsertFacts() error = %v", err)
	}

	got, err := s.ListFacts(ctx, acme.CIK, store.FactQuery{Concepts: []string{"EntityCommonStockSharesOutstanding"}})
	if err != nil {
		t.Fatalf("ListFacts() error = %v", err)
	}
	if diff := cmp.Diff([]domain.Fact{shares}, got); diff != "" {
		t.Errorf("ListFacts(shares concept) (-want +got):\n%s", diff)
	}
	got, err = s.ListFacts(ctx, initech.CIK, store.FactQuery{})
	if err != nil || len(got) != 0 {
		t.Errorf("ListFacts(company without facts) = %v, %v; want empty, nil", got, err)
	}
}

func TestLatestFiledPerCompany(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme, globex)
	other := epsFact(t, "2026-01-01", "2026-03-31", 2, "2026-04-20", "0001002-26-000001")
	other.CIK = globex.CIK
	if _, err := s.InsertFacts(ctx, []domain.Fact{
		epsFact(t, "2026-01-01", "2026-03-31", 1.23, "2026-05-01", "0001001-26-000001"),
		epsFact(t, "2026-01-01", "2026-03-31", 1.19, "2026-06-15", "0001001-26-000002"),
		other,
	}); err != nil {
		t.Fatalf("InsertFacts() error = %v", err)
	}

	got, err := s.GetLatestFiled(ctx, acme.CIK)
	if err != nil || got != date(t, "2026-06-15") {
		t.Errorf("GetLatestFiled(acme) = %s, %v; want 2026-06-15, nil", got, err)
	}
	_, err = s.GetLatestFiled(ctx, initech.CIK)
	wantNotFound(t, "GetLatestFiled(company without facts)", err)

	all, err := s.ListLatestFiled(ctx)
	if err != nil {
		t.Fatalf("ListLatestFiled() error = %v", err)
	}
	want := map[domain.CIK]domain.Date{acme.CIK: date(t, "2026-06-15"), globex.CIK: date(t, "2026-04-20")}
	if diff := cmp.Diff(want, all); diff != "" {
		t.Errorf("ListLatestFiled() (-want +got):\n%s", diff)
	}
}

func TestInsertFactsOfAnUnknownCompanyFails(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	_, err := s.InsertFacts(context.Background(), []domain.Fact{epsFact(t, "2026-01-01", "2026-03-31", 1, "2026-05-01", "x")})
	if err == nil {
		t.Error("InsertFacts() for a CIK without a company error = nil, want an error")
	}
}

func TestFilings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme)
	q1 := domain.Filing{
		Accession: "0001001-26-000001", CIK: acme.CIK, Form: "10-Q", Filed: date(t, "2026-05-01"),
		ReportDate: datePtr(t, "2026-03-31"), PrimaryDocument: "acme-20260331.htm",
	}
	amendment := domain.Filing{Accession: "0001001-26-000002", CIK: acme.CIK, Form: "10-Q/A", Filed: date(t, "2026-06-15")}

	n, err := s.InsertFilings(ctx, []domain.Filing{amendment, q1})
	if err != nil || n != 2 {
		t.Fatalf("InsertFilings() = %d, %v; want 2, nil", n, err)
	}
	n, err = s.InsertFilings(ctx, []domain.Filing{q1})
	if err != nil || n != 0 {
		t.Fatalf("repeated InsertFilings() = %d, %v; want 0, nil", n, err)
	}

	got, err := s.GetFiling(ctx, q1.Accession)
	if err != nil {
		t.Fatalf("GetFiling() error = %v", err)
	}
	if diff := cmp.Diff(q1, got); diff != "" {
		t.Errorf("GetFiling() (-want +got):\n%s", diff)
	}
	_, err = s.GetFiling(ctx, "0000000-00-000000")
	wantNotFound(t, "GetFiling(unknown)", err)

	list, err := s.ListFilings(ctx, acme.CIK)
	if err != nil {
		t.Fatalf("ListFilings() error = %v", err)
	}
	if diff := cmp.Diff([]domain.Filing{q1, amendment}, list); diff != "" {
		t.Errorf("ListFilings() (-want +got):\n%s", diff)
	}
}

func TestStockSplits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme, globex)
	split := domain.Split{CIK: acme.CIK, Date: date(t, "2024-06-10"), From: 1, To: 10, Source: "sec", Evidence: "0001001-24-000124"}
	threeForTwo := domain.Split{CIK: acme.CIK, Date: date(t, "2020-01-15"), From: 2, To: 3, Source: "sec"}
	other := domain.Split{CIK: globex.CIK, Date: date(t, "2022-01-15"), From: 1, To: 4, Source: "sec"}

	n, err := s.InsertStockSplits(ctx, []domain.Split{split, threeForTwo, other})
	if err != nil || n != 3 {
		t.Fatalf("InsertStockSplits() = %d, %v; want 3, nil", n, err)
	}
	n, err = s.InsertStockSplits(ctx, []domain.Split{split})
	if err != nil || n != 0 {
		t.Fatalf("repeated InsertStockSplits() = %d, %v; want 0, nil", n, err)
	}

	got, err := s.ListStockSplits(ctx, acme.CIK)
	if err != nil {
		t.Fatalf("ListStockSplits() error = %v", err)
	}
	// The table keeps the ratio, so From comes back as 1.
	want := []domain.Split{
		{CIK: acme.CIK, Date: threeForTwo.Date, From: 1, To: 1.5, Source: "sec"},
		{CIK: acme.CIK, Date: split.Date, From: 1, To: 10, Source: "sec", Evidence: split.Evidence},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ListStockSplits() (-want +got):\n%s", diff)
	}
}
