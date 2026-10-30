//go:build integration

package store_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/go-cmp/cmp"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/sourcehawk/pricedin/server/internal/store"
	"github.com/sourcehawk/pricedin/server/internal/store/pgtest"
)

var wantTables = []string{
	"api_tokens",
	"companies",
	"dividends",
	"facts",
	"filings",
	"goose_db_version",
	"ingest_runs",
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
