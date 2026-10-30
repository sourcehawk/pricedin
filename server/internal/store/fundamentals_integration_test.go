//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/store"
)

func TestReplaceQuarterlyFundamentalsReplacesTheCompanysRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme, globex)
	q1 := store.QuarterlyValue{
		FiscalYear: 2026, FiscalQuarter: 1, Metric: "eps_diluted", Value: 1.23, Method: "reported",
		PeriodStart: date(t, "2026-01-01"), PeriodEnd: date(t, "2026-03-31"), Filed: date(t, "2026-05-01"),
		Inputs: []store.SourceRef{{Accession: "0001001-26-000001", Form: "10-Q", Filed: date(t, "2026-05-01"), Concept: "EarningsPerShareDiluted"}},
	}
	q4 := store.QuarterlyValue{
		FiscalYear: 2025, FiscalQuarter: 4, Metric: "revenue", Value: 5e9, Method: "fy_difference",
		PeriodStart: date(t, "2025-10-01"), PeriodEnd: date(t, "2025-12-31"), Filed: date(t, "2026-02-20"),
		Inputs: []store.SourceRef{
			{Accession: "0001001-26-000000", Form: "10-K", Filed: date(t, "2026-02-20"), Concept: "Revenues"},
			{Accession: "0001001-25-000009", Form: "10-Q", Filed: date(t, "2025-11-01"), Concept: "Revenues"},
		},
	}
	otherCompany := q1
	otherCompany.Value = 7

	if err := s.ReplaceQuarterlyFundamentals(ctx, globex.CIK, []store.QuarterlyValue{otherCompany}); err != nil {
		t.Fatalf("ReplaceQuarterlyFundamentals(globex) error = %v", err)
	}
	if err := s.ReplaceQuarterlyFundamentals(ctx, acme.CIK, []store.QuarterlyValue{q1, q4}); err != nil {
		t.Fatalf("ReplaceQuarterlyFundamentals(acme) error = %v", err)
	}
	got, err := s.ListQuarterlyFundamentals(ctx, acme.CIK)
	if err != nil {
		t.Fatalf("ListQuarterlyFundamentals() error = %v", err)
	}
	// Oldest quarter first.
	if diff := cmp.Diff([]store.QuarterlyValue{q4, q1}, got); diff != "" {
		t.Errorf("ListQuarterlyFundamentals() (-want +got):\n%s", diff)
	}

	if err := s.ReplaceQuarterlyFundamentals(ctx, acme.CIK, []store.QuarterlyValue{q1}); err != nil {
		t.Fatalf("second ReplaceQuarterlyFundamentals(acme) error = %v", err)
	}
	got, err = s.ListQuarterlyFundamentals(ctx, acme.CIK)
	if err != nil {
		t.Fatalf("ListQuarterlyFundamentals() error = %v", err)
	}
	if diff := cmp.Diff([]store.QuarterlyValue{q1}, got); diff != "" {
		t.Errorf("after the rebuild (-want +got):\n%s", diff)
	}
	others, err := s.ListQuarterlyFundamentals(ctx, globex.CIK)
	if err != nil || len(others) != 1 {
		t.Errorf("ListQuarterlyFundamentals(globex) = %v, %v; want its one row kept", others, err)
	}
}

func TestReplaceQuarterlyFundamentalsKeepsTheOldRowsWhenTheNewOnesFail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme)
	good := store.QuarterlyValue{
		FiscalYear: 2026, FiscalQuarter: 1, Metric: "eps_diluted", Value: 1.23, Method: "reported",
		PeriodStart: date(t, "2026-01-01"), PeriodEnd: date(t, "2026-03-31"), Filed: date(t, "2026-05-01"),
	}
	if err := s.ReplaceQuarterlyFundamentals(ctx, acme.CIK, []store.QuarterlyValue{good}); err != nil {
		t.Fatalf("ReplaceQuarterlyFundamentals() error = %v", err)
	}
	bad := good
	bad.Method = "guessed"

	if err := s.ReplaceQuarterlyFundamentals(ctx, acme.CIK, []store.QuarterlyValue{bad}); err == nil {
		t.Fatal("ReplaceQuarterlyFundamentals() with an unknown method error = nil, want an error")
	}
	got, err := s.ListQuarterlyFundamentals(ctx, acme.CIK)
	if err != nil {
		t.Fatalf("ListQuarterlyFundamentals() error = %v", err)
	}
	if diff := cmp.Diff([]store.QuarterlyValue{good}, got); diff != "" {
		t.Errorf("rows after the failed rebuild (-want +got):\n%s", diff)
	}
}

func TestReplaceQuarterlyFundamentalsWithNoRowsClearsTheCompany(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := newStore(t)
	mustUpsertCompanies(t, s, acme)
	if err := s.ReplaceQuarterlyFundamentals(ctx, acme.CIK, []store.QuarterlyValue{{
		FiscalYear: 2026, FiscalQuarter: 1, Metric: "eps_diluted", Value: 1, Method: "reported",
		PeriodStart: date(t, "2026-01-01"), PeriodEnd: date(t, "2026-03-31"), Filed: date(t, "2026-05-01"),
	}}); err != nil {
		t.Fatalf("ReplaceQuarterlyFundamentals() error = %v", err)
	}
	if err := s.ReplaceQuarterlyFundamentals(ctx, acme.CIK, nil); err != nil {
		t.Fatalf("ReplaceQuarterlyFundamentals(nil) error = %v", err)
	}
	got, err := s.ListQuarterlyFundamentals(ctx, acme.CIK)
	if err != nil || len(got) != 0 {
		t.Errorf("ListQuarterlyFundamentals() = %v, %v; want empty, nil", got, err)
	}
}

func TestReplaceQuarterlyFundamentalsOfOneCompanyRunOneAfterTheOther(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, pool := newStore(t)
	mustUpsertCompanies(t, s, acme)
	value := func(quarter int) store.QuarterlyValue {
		return store.QuarterlyValue{
			FiscalYear: 2026, FiscalQuarter: quarter, Metric: "eps_diluted", Value: 1, Method: "reported",
			PeriodStart: date(t, "2026-01-01"), PeriodEnd: date(t, "2026-03-31"), Filed: date(t, "2026-05-01"),
		}
	}

	second := make(chan error, 1)
	err := s.WithTx(ctx, func(tx *store.Store) error {
		if err := tx.ReplaceQuarterlyFundamentals(ctx, acme.CIK, []store.QuarterlyValue{value(1)}); err != nil {
			return err
		}
		go func() { second <- s.ReplaceQuarterlyFundamentals(ctx, acme.CIK, []store.QuarterlyValue{value(2)}) }()
		waitForBlockedQuery(t, pool)
		return nil
	})
	if err != nil {
		t.Fatalf("first ReplaceQuarterlyFundamentals() error = %v", err)
	}
	if err := <-second; err != nil {
		t.Fatalf("second ReplaceQuarterlyFundamentals() error = %v", err)
	}

	got, err := s.ListQuarterlyFundamentals(ctx, acme.CIK)
	if err != nil {
		t.Fatalf("ListQuarterlyFundamentals() error = %v", err)
	}
	// The replacement that committed last wins; the two never mix.
	if diff := cmp.Diff([]store.QuarterlyValue{value(2)}, got); diff != "" {
		t.Errorf("rows after two concurrent replacements (-want +got):\n%s", diff)
	}
}
