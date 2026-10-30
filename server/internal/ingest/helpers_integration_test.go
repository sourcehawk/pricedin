//go:build integration

package ingest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sourcehawk/pricedin/server/internal/blob"
	"github.com/sourcehawk/pricedin/server/internal/fixtureserver"
	"github.com/sourcehawk/pricedin/server/internal/ingest"
	"github.com/sourcehawk/pricedin/server/internal/prices/massive"
	"github.com/sourcehawk/pricedin/server/internal/sec"
	"github.com/sourcehawk/pricedin/server/internal/store"
	"github.com/sourcehawk/pricedin/server/internal/store/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.Main(m)
}

const (
	testdata   = "../../../testdata"
	massiveKey = "test-key"
)

// fixtureUniverse is every company with SEC and price fixtures.
func fixtureUniverse() []string { return []string{"NVDA", "AAPL", "KO", "GRMN", "ADM"} }

// clock is a settable time source that the runner and the price client
// share.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Set(s string) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}

type envOptions struct {
	now          string
	backfillDays int
	// wrap, when set, wraps the fixture server's handler.
	wrap func(http.Handler) http.Handler
}

// env is one test's database, blob store, fixture server and runner.
type env struct {
	url    string
	pool   *pgxpool.Pool
	store  *store.Store
	blob   blob.Store
	clock  *clock
	srv    *httptest.Server
	runner *ingest.Runner
}

func newEnv(t *testing.T, opts envOptions) *env {
	t.Helper()
	if opts.now == "" {
		// The 2026-10-29 session, the last of the fixtures, is over.
		opts.now = "2026-10-30T12:00:00Z"
	}
	h, err := fixtureserver.New(fixtureserver.Options{Root: testdata, MassiveAPIKey: massiveKey})
	if err != nil {
		t.Fatal(err)
	}
	if opts.wrap != nil {
		h = opts.wrap(h)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	url := pgtest.NewURL(t)
	pool := newPool(t, url)
	bs, err := blob.NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := &clock{}
	c.Set(opts.now)
	e := &env{url: url, pool: pool, store: store.New(pool), blob: bs, clock: c, srv: srv}
	e.runner = e.newRunner(t, pool, opts.backfillDays)
	return e
}

func newPool(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// newRunner returns a runner with its own clients over pool, as a second
// process would have.
func (e *env) newRunner(t *testing.T, pool *pgxpool.Pool, backfillDays int) *ingest.Runner {
	t.Helper()
	rec := ingest.NewRecorder(e.blob, e.clock.Now)
	sc, err := sec.New(sec.Options{
		UserAgent:         "PricedIn test@example.com",
		BaseURL:           e.srv.URL + fixtureserver.PrefixSECWWW,
		DataBaseURL:       e.srv.URL + fixtureserver.PrefixSECData,
		RequestsPerSecond: sec.MaxRequestsPerSecond,
		Recorder:          rec,
	})
	if err != nil {
		t.Fatal(err)
	}
	mc, err := massive.New(massive.Options{
		BaseURL: e.srv.URL + fixtureserver.PrefixMassive, APIKey: massiveKey,
		RateLimit: 1000, MaxRetries: -1, Recorder: rec, Now: e.clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err := ingest.New(ingest.Options{
		Pool: pool, Blob: e.blob, SEC: sc, Prices: mc,
		Universe: fixtureUniverse(), PricesBackfillDays: backfillDays, Now: e.clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// snapshotTables lists every table that the jobs write, with the columns
// that change on every write even when the content does not.
func snapshotTables() map[string][]string {
	return map[string][]string{
		"companies":              nil,
		"tickers":                nil,
		"ticker_syncs":           {"synced_at"},
		"filings":                nil,
		"facts":                  nil,
		"stock_splits":           nil,
		"quarterly_fundamentals": {"built_at"},
		"fundamental_conflicts":  nil,
		"prices_daily":           nil,
		"price_splits":           nil,
		"dividends":              nil,
		"price_grouped_loads":    nil,
		"price_reference_checks": nil,
		"ingest_cursors":         nil,
	}
}

// snapshot returns the content of the database tables and the keys of the
// blob store. Timestamps such as loaded_at stay in, so that a row that a
// second run rewrote with the same values shows up.
func (e *env) snapshot(t *testing.T) map[string]string {
	t.Helper()
	ctx := context.Background()
	out := map[string]string{}
	for table, volatile := range snapshotTables() {
		if volatile == nil {
			volatile = []string{}
		}
		var rows string
		err := e.pool.QueryRow(ctx, `
			SELECT coalesce(jsonb_agg(r ORDER BY r::text), '[]')::text
			FROM (SELECT to_jsonb(t) - $1::text[] AS r FROM `+table+` t) s`, volatile).Scan(&rows)
		if err != nil {
			t.Fatalf("snapshot %s: %v", table, err)
		}
		out[table] = rows
	}
	keys, err := e.blob.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	out["blob keys"] = strings.Join(keys, "\n")
	return out
}

func (e *env) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", sql, err)
	}
	return n
}

// must(f())(t) returns the value of f, and fails the test when f returns
// an error.
func must[T any](v T, err error) func(*testing.T) T {
	return func(t *testing.T) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}
