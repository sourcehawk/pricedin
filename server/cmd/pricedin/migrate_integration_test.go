//go:build integration

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/store"
	"github.com/sourcehawk/pricedin/server/internal/store/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.Main(m)
}

// run executes the command and returns what it printed.
func run(t *testing.T, env map[string]string, args ...string) string {
	t.Helper()
	out, err := runErr(t, env, args...)
	if err != nil {
		t.Fatalf("pricedin %s error = %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func runErr(t *testing.T, env map[string]string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := newRootCmd(func(k string) string { return env[k] }, &out)
	cmd.SetArgs(args)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.ExecuteContext(t.Context())
	return out.String(), err
}

func TestMigrateUpDownAndUpAgain(t *testing.T) {
	env := map[string]string{"PRICEDIN_DATABASE_URL": pgtest.NewEmptyURL(t)}

	out := run(t, env, "migrate", "status")
	if strings.Contains(out, "applied") || !strings.Contains(out, "pending  00001_sec_reference.sql") {
		t.Errorf("status before up:\n%s\nwant every migration pending", out)
	}

	out = run(t, env, "migrate", "up")
	if !strings.Contains(out, "up  00007_session_owner.sql") {
		t.Errorf("up:\n%s\nwant the last migration applied", out)
	}
	out = run(t, env, "migrate", "status")
	if strings.Contains(out, "pending") {
		t.Errorf("status after up:\n%s\nwant no pending migration", out)
	}

	out = run(t, env, "migrate", "down")
	if !strings.Contains(out, "down  00007_session_owner.sql") || strings.Contains(out, "00006") {
		t.Errorf("down:\n%s\nwant only the last migration rolled back", out)
	}
	out = run(t, env, "migrate", "down", "--to", "0")
	if !strings.Contains(out, "down  00001_sec_reference.sql") {
		t.Errorf("down --to 0:\n%s\nwant every migration rolled back", out)
	}
	out = run(t, env, "migrate", "status")
	if strings.Contains(out, "applied") {
		t.Errorf("status after down --to 0:\n%s\nwant every migration pending", out)
	}

	run(t, env, "migrate", "up")
	out = run(t, env, "migrate", "up")
	if !strings.Contains(out, "no pending migrations") {
		t.Errorf("second up:\n%s\nwant no pending migrations", out)
	}
}

func TestMigrateDownRefusesWhileFactsHasRows(t *testing.T) {
	url := pgtest.NewEmptyURL(t)
	env := map[string]string{"PRICEDIN_DATABASE_URL": url}
	run(t, env, "migrate", "up")
	pool, err := pgxpool.New(t.Context(), url)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()
	s := store.New(pool)
	if _, err := s.UpsertCompanies(t.Context(), []domain.Company{{CIK: 1001, Name: "Acme"}}); err != nil {
		t.Fatalf("UpsertCompanies() error = %v", err)
	}
	if _, err := s.InsertFacts(t.Context(), []domain.Fact{{
		CIK: 1001, Taxonomy: "us-gaap", Concept: "EarningsPerShareDiluted", Unit: "USD/shares",
		PeriodEnd: domain.NewDate(2026, 3, 31), Value: 1, Form: "10-Q", FiscalYear: 2026, FiscalPeriod: "Q1",
		Filed: domain.NewDate(2026, 5, 1), Accession: "0001001-26-000001",
	}}); err != nil {
		t.Fatalf("InsertFacts() error = %v", err)
	}
	pool.Close()

	for _, args := range [][]string{{"migrate", "down"}, {"migrate", "down", "--to", "0"}} {
		out, err := runErr(t, env, args...)
		if err == nil || !strings.Contains(err.Error(), "--force") {
			t.Errorf("pricedin %s error = %v, want a refusal that names --force\n%s", strings.Join(args, " "), err, out)
		}
	}
	if out := run(t, env, "migrate", "status"); strings.Contains(out, "pending") {
		t.Errorf("status after the refusals:\n%s\nwant every migration still applied", out)
	}

	if out := run(t, env, "migrate", "down", "--to", "0", "--force"); !strings.Contains(out, "down  00002_facts.sql") {
		t.Errorf("down --to 0 --force:\n%s\nwant the facts migration rolled back", out)
	}
}
