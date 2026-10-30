//go:build integration

package store_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/sourcehawk/pricedin/server/internal/store"
	"github.com/sourcehawk/pricedin/server/internal/store/pgtest"
)

var wantTables = []string{
	"api_tokens",
	"companies",
	"dividends",
	"facts",
	"filings",
	"fundamental_conflicts",
	"fundamentals_pending",
	"goose_db_version",
	"ingest_cursors",
	"ingest_lock_releases",
	"ingest_runs",
	"price_grouped_loads",
	"price_reference_checks",
	"price_splits",
	"prices_daily",
	"quarterly_fundamentals",
	"sessions",
	"stock_splits",
	"ticker_syncs",
	"tickers",
}

func publicTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(),
		`SELECT table_name FROM information_schema.tables
		 WHERE table_schema = 'public' AND table_type = 'BASE TABLE' ORDER BY table_name`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	tables := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list tables: %v", err)
	}
	return tables
}

func TestMigrationsRoundTripOnAnEmptyDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := sql.Open("pgx", pgtest.NewEmptyURL(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	migrator, err := store.NewMigrator(db)
	if err != nil {
		t.Fatalf("NewMigrator() error = %v", err)
	}

	if _, err := migrator.Up(ctx); err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	if diff := cmp.Diff(wantTables, publicTables(t, db)); diff != "" {
		t.Errorf("tables after Up (-want +got):\n%s", diff)
	}

	if _, err := migrator.DownTo(ctx, 0); err != nil {
		t.Fatalf("DownTo(0) error = %v", err)
	}
	if diff := cmp.Diff([]string{"goose_db_version"}, publicTables(t, db)); diff != "" {
		t.Errorf("tables after DownTo(0) (-want +got):\n%s", diff)
	}
	var views int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.views WHERE table_schema = 'public'`).Scan(&views); err != nil {
		t.Fatalf("count views: %v", err)
	}
	if views != 0 {
		t.Errorf("views after DownTo(0) = %d, want 0", views)
	}

	if _, err := migrator.Up(ctx); err != nil {
		t.Fatalf("second Up() error = %v", err)
	}
	if diff := cmp.Diff(wantTables, publicTables(t, db)); diff != "" {
		t.Errorf("tables after the second Up (-want +got):\n%s", diff)
	}
}

func TestRollingBackTheFactsMigrationRefusesWhileFactsHasRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	url := pgtest.NewEmptyURL(t)
	open := func(params map[string]string) *sql.DB {
		t.Helper()
		cfg, err := pgx.ParseConfig(url)
		if err != nil {
			t.Fatalf("parse url: %v", err)
		}
		for k, v := range params {
			cfg.RuntimeParams[k] = v
		}
		db := stdlib.OpenDB(*cfg)
		t.Cleanup(func() { _ = db.Close() })
		return db
	}
	db := open(nil)
	migrator, err := store.NewMigrator(db)
	if err != nil {
		t.Fatalf("NewMigrator() error = %v", err)
	}
	if _, err := migrator.Up(ctx); err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO companies (cik, name) VALUES (1001, 'Acme')`); err != nil {
		t.Fatalf("insert company: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO facts (cik, taxonomy, concept, unit, period_end, value, form,
		fiscal_year, fiscal_period, filed, accession)
		VALUES (1001, 'us-gaap', 'EarningsPerShareDiluted', 'USD/shares', '2026-03-31', 1, '10-Q', 2026, 'Q1', '2026-05-01', 'a')`); err != nil {
		t.Fatalf("insert fact: %v", err)
	}

	if _, err := migrator.DownTo(ctx, 1); err == nil {
		t.Fatal("DownTo(1) with facts rows error = nil, want an error")
	}
	if n := countFacts(t, db); n != 1 {
		t.Errorf("facts rows after the refused rollback = %d, want 1", n)
	}

	forced, err := store.NewMigrator(open(map[string]string{store.AllowDropFactsSetting: "on"}))
	if err != nil {
		t.Fatalf("NewMigrator() error = %v", err)
	}
	if _, err := forced.DownTo(ctx, 1); err != nil {
		t.Errorf("DownTo(1) with %s on error = %v, want nil", store.AllowDropFactsSetting, err)
	}
}

func countFacts(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM facts`).Scan(&n); err != nil {
		t.Fatalf("count facts: %v", err)
	}
	return n
}

func TestTwoMigratorsAtOnceBothReachTheLatestVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	url := pgtest.NewEmptyURL(t)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			db, err := sql.Open("pgx", url)
			if err != nil {
				errs <- err
				return
			}
			defer func() { _ = db.Close() }()
			migrator, err := store.NewMigrator(db)
			if err != nil {
				errs <- err
				return
			}
			_, err = migrator.Up(ctx)
			errs <- err
		}()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Errorf("concurrent Up() error = %v", err)
		}
	}

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	migrator, err := store.NewMigrator(db)
	if err != nil {
		t.Fatalf("NewMigrator() error = %v", err)
	}
	pending, err := migrator.HasPending(ctx)
	if err != nil || pending {
		t.Errorf("HasPending() = %v, %v; want false, nil", pending, err)
	}
}
