//go:build integration

package ingest_test

import (
	"context"
	"net/http"
	"time"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sourcehawk/pricedin/server/internal/blob"
	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/ingest"
	"github.com/sourcehawk/pricedin/server/internal/prices/massive"
	"github.com/sourcehawk/pricedin/server/internal/sec"
	"github.com/sourcehawk/pricedin/server/internal/store"
	"github.com/sourcehawk/pricedin/server/internal/store/pgtest"
)

// reprocessUniverse leaves out ADM, which the tests load on demand, so
// that its bars come from per-symbol requests.
func reprocessUniverse() []string { return []string{"NVDA", "AAPL", "KO", "GRMN"} }

// loadWithJobs runs the jobs of a first import and an on-demand load
// against the fixture server, as an owner would.
func loadWithJobs(t *testing.T, e *env) {
	t.Helper()
	ctx := context.Background()
	r := e.newRunner(t, e.pool, 30, reprocessUniverse())
	must(r.SECTickers(ctx))(t)
	must(r.SECBulk(ctx, ingest.BulkArchive))(t)
	must(r.PriceReference(ctx))(t)
	must(r.PricesBackfill(ctx))(t)
	must(r.LoadCompanyResult(ctx, "ADM"))(t)
	must(r.FundamentalsRebuild(ctx, "pending"))(t)
}

// offlineRunner returns a runner over a new database that reads bs, and
// whose SEC and price clients fail the test on any request.
func offlineRunner(t *testing.T, e *env, bs blob.Store) (*ingest.Runner, *pgxpool.Pool) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("reprocess sent a request: %s %s", r.Method, r.URL)
		http.Error(w, "no requests", http.StatusTeapot)
	}))
	t.Cleanup(srv.Close)
	sc, err := sec.New(sec.Options{UserAgent: "PricedIn test@example.com", BaseURL: srv.URL, DataBaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	mc, err := massive.New(massive.Options{BaseURL: srv.URL, APIKey: massiveKey, MaxRetries: -1, Now: e.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	pool := newPool(t, pgtest.NewURL(t))
	r, err := ingest.New(context.Background(), ingest.Options{
		Pool: pool, Blob: bs, SEC: sc, Prices: mc, StoredPrices: massive.Stored{},
		Universe: reprocessUniverse(), Now: e.clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r, pool
}

// loadedTables lists the tables that hold loaded data, with the columns
// that hold when or in which order a row was written.
func loadedTables() map[string][]string {
	return map[string][]string{
		"companies":              {"updated_at"},
		"tickers":                {},
		"ticker_syncs":           {"synced_at"},
		"filings":                {"loaded_at"},
		"facts":                  {"id", "loaded_at"},
		"stock_splits":           {},
		"quarterly_fundamentals": {"built_at"},
		"fundamental_conflicts":  {},
		"fundamentals_pending":   {"marked_at"},
		"prices_daily":           {"loaded_at"},
		"price_splits":           {"loaded_at"},
		"dividends":              {"loaded_at"},
		"price_grouped_loads":    {"loaded_at"},
		"price_reference_checks": {"first_checked_at"},
	}
}

// tableRows returns the rows of each table, without the given columns.
func tableRows(t *testing.T, pool *pgxpool.Pool, tables map[string][]string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for table, drop := range tables {
		var rows string
		err := pool.QueryRow(context.Background(), `
			SELECT coalesce(jsonb_agg(r ORDER BY r::text), '[]')::text
			FROM (SELECT to_jsonb(t) - $1::text[] AS r FROM `+table+` t) s`, drop).Scan(&rows)
		if err != nil {
			t.Fatalf("rows of %s: %v", table, err)
		}
		out[table] = rows
	}
	return out
}

func TestReprocessIntoAnEmptyDatabaseGivesTheDataOfTheJobs(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	loadWithJobs(t, e)
	r, pool := offlineRunner(t, e, e.blob)
	blobBefore := must(e.blob.List(context.Background(), ""))(t)

	res, err := r.Reprocess(context.Background(), ingest.ReprocessOptions{})
	if err != nil {
		t.Fatalf("Reprocess() error = %v", err)
	}
	if res.Failed != 0 || res.Incomplete != 0 || res.Files == 0 || res.Fundamentals.Companies != 5 {
		t.Errorf("Reprocess() = %+v, want files, no failures, and the quarters of 5 companies", res)
	}
	want := tableRows(t, e.pool, loadedTables())
	got := tableRows(t, pool, loadedTables())
	for table := range loadedTables() {
		if diff := cmp.Diff(want[table], got[table]); diff != "" {
			t.Errorf("%s after reprocess differs from the jobs (-jobs +reprocess):\n%s", table, diff)
		}
	}
	if diff := cmp.Diff(blobBefore, must(e.blob.List(context.Background(), ""))(t)); diff != "" {
		t.Errorf("reprocess changed the blob store (-before +after):\n%s", diff)
	}
	run := must(store.New(pool).GetLastSucceededIngestRun(context.Background(), ingest.JobReprocess))(t)
	if run.Stats["files"] != res.Files || run.Stats["facts"] != res.Facts {
		t.Errorf("ingest run stats = %v, want the counts of %+v", run.Stats, res)
	}
}

func TestReprocessTwiceChangesNothingTheSecondTime(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	loadWithJobs(t, e)
	r, pool := offlineRunner(t, e, e.blob)
	ctx := context.Background()
	must(r.Reprocess(ctx, ingest.ReprocessOptions{}))(t)
	// The write times stay in, so that a row written again shows up.
	tables := map[string][]string{}
	for table := range loadedTables() {
		tables[table] = []string{}
	}
	tables["ticker_syncs"] = []string{"synced_at"}
	tables["quarterly_fundamentals"] = []string{"built_at"}
	before := tableRows(t, pool, tables)

	res, err := r.Reprocess(ctx, ingest.ReprocessOptions{})
	if err != nil {
		t.Fatalf("second Reprocess() error = %v", err)
	}
	if diff := cmp.Diff(before, tableRows(t, pool, tables)); diff != "" {
		t.Errorf("second reprocess changed the database (-before +after):\n%s", diff)
	}
	changed := res.CompaniesChanged + res.Filings + res.Facts + res.TickersOpened + res.TickersClosed +
		res.Splits + res.Dividends + res.Bars + res.Fundamentals.Companies
	if changed != 0 || res.Failed != 0 {
		t.Errorf("second Reprocess() = %+v, want no row added or changed and no rebuild", res)
	}
}

// A reprocess on the database that the jobs loaded finds nothing new.
func TestReprocessOverTheDatabaseOfTheJobsChangesNothing(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	loadWithJobs(t, e)
	// A ticker map of the day before the jobs ran, which lists no symbol.
	record(t, e.blob, "2026-10-29T12:00:00Z", "sec/tickers/company_tickers_exchange.json",
		`{"fields":["cik","name","ticker","exchange"],"data":[]}`)
	before := e.snapshot(t)
	r := e.newRunner(t, e.pool, 30, reprocessUniverse())
	res, err := r.Reprocess(context.Background(), ingest.ReprocessOptions{})
	if err == nil {
		t.Fatal("Reprocess() without StoredPrices error = nil, want an error")
	}
	if res.Files != 0 {
		t.Errorf("Reprocess() without StoredPrices read %d files, want none", res.Files)
	}
	res, err = r.Reprocess(context.Background(), ingest.ReprocessOptions{Prefix: ingest.ReprocessSEC})
	if err != nil {
		t.Fatalf("Reprocess(sec) error = %v", err)
	}
	if res.Facts+res.Filings+res.CompaniesChanged+res.TickersOpened+res.TickersClosed != 0 || res.Old != 1 {
		t.Errorf("Reprocess(sec) = %+v, want nothing new, and the ticker map of the day before the sync as old", res)
	}
	if diff := cmp.Diff(before, e.snapshot(t)); diff != "" {
		t.Errorf("reprocess changed the data of the jobs (-before +after):\n%s", diff)
	}
}

// record stores body under the versioned key of base, retrieved at the
// RFC 3339 time at.
func record(t *testing.T, bs blob.Store, at, base, body string) {
	t.Helper()
	when, err := time.Parse(time.RFC3339, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := ingest.NewRecorder(bs, func() time.Time { return when }).Record(context.Background(), base, []byte(body)); err != nil {
		t.Fatal(err)
	}
}

func mustDate(s string) domain.Date {
	d, err := domain.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

func newBlob(t *testing.T) blob.Store {
	t.Helper()
	bs, err := blob.NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return bs
}

func TestReprocessWithAPrefixReadsOnlyTheFilesOfThatSource(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	loadWithJobs(t, e)
	r, pool := offlineRunner(t, e, e.blob)
	must(r.Reprocess(context.Background(), ingest.ReprocessOptions{Prefix: ingest.ReprocessSEC}))(t)
	db := &env{pool: pool}
	if n := db.count(t, `SELECT count(*) FROM facts`); n == 0 {
		t.Error("reprocess of the SEC files loaded no facts")
	}
	if n := db.count(t, `SELECT count(*) FROM prices_daily`) + db.count(t, `SELECT count(*) FROM dividends`); n != 0 {
		t.Errorf("reprocess of the SEC files loaded %d price rows, want none", n)
	}
}

func TestReprocessOfOneCompanyLoadsOnlyItsFiles(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	loadWithJobs(t, e)
	r, pool := offlineRunner(t, e, e.blob)
	ctx := context.Background()
	const ko = 21344
	must(r.Reprocess(ctx, ingest.ReprocessOptions{Prefix: ingest.ReprocessSEC}))(t)
	db := &env{pool: pool}
	res, err := r.Reprocess(ctx, ingest.ReprocessOptions{CIK: ko})
	if err != nil {
		t.Fatalf("Reprocess(--cik %d) error = %v", ko, err)
	}
	if res.TickersOpened+res.TickersClosed != 0 {
		t.Errorf("Reprocess(--cik) = %+v, want the ticker maps left out", res)
	}
	if n := db.count(t, `SELECT count(DISTINCT symbol) FROM prices_daily WHERE symbol <> 'KO'`); n != 0 {
		t.Errorf("Reprocess(--cik) loaded the bars of %d other symbols, want KO only", n)
	}
	if n := db.count(t, `SELECT count(*) FROM prices_daily WHERE symbol = 'KO'`); n == 0 {
		t.Error("Reprocess(--cik) loaded no bars of KO")
	}
	if n := db.count(t, `SELECT count(*) FROM price_grouped_loads`); n != 0 {
		t.Errorf("Reprocess(--cik) marked %d grouped days loaded, want none", n)
	}
}

func TestReprocessSinceADayLeavesOutTheFilesRetrievedBefore(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	loadWithJobs(t, e)
	r, _ := offlineRunner(t, e, e.blob)
	res, err := r.Reprocess(context.Background(), ingest.ReprocessOptions{Since: mustDate("2026-10-31")})
	if err != nil || res.Files != 0 {
		t.Errorf("Reprocess(--since the day after the jobs) = %+v, %v; want no file read", res, err)
	}
}

// A provider's splits and dividends load in date order, so the newest
// version wins, and a version equal to the one of the day before is
// skipped.
func TestReprocessLoadsTheVersionsOfASourceOldestFirst(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	bs := newBlob(t)
	splits := `{"status":"OK","results":[]}`
	dividend := func(amount string) string {
		return `{"status":"OK","results":[{"id":"d1","ticker":"KO","ex_dividend_date":"2026-09-15","cash_amount":` + amount + `}]}`
	}
	for _, v := range []struct{ at, amount string }{
		{"2026-10-28T12:00:00Z", "0.6"},
		{"2026-10-27T12:00:00Z", "0.5"},
		{"2026-10-29T12:00:00Z", "0.6"},
	} {
		record(t, bs, v.at, "prices/massive/splits/KO/page-1.json", splits)
		record(t, bs, v.at, "prices/massive/dividends/KO/page-1.json", dividend(v.amount))
	}
	r, pool := offlineRunner(t, e, bs)
	res, err := r.Reprocess(context.Background(), ingest.ReprocessOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 4 || res.Unchanged != 2 {
		t.Errorf("Reprocess() = %+v, want 4 files loaded and the 2 of the last day unchanged", res)
	}
	var amount float64
	if err := pool.QueryRow(context.Background(), `SELECT cash_amount FROM dividends WHERE provider_id = 'd1'`).Scan(&amount); err != nil {
		t.Fatal(err)
	}
	if amount != 0.6 {
		t.Errorf("cash amount = %v, want 0.6 of the newest version", amount)
	}
}

func TestReprocessSkipsAResponseThatTheJobCouldNotHaveStored(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	bs := newBlob(t)
	at := "2026-10-30T12:00:00Z"
	// Page 2 of the grouped day is missing.
	record(t, bs, at, "prices/massive/grouped/2026-10-29/page-1.json",
		`{"status":"OK","results":[{"T":"KO","t":1761710400000,"c":70}],"next_url":"https://api.massive.com/v2/aggs?cursor=p2"}`)
	// The dividends that the job stores with these splits are missing.
	record(t, bs, at, "prices/massive/splits/KO/page-1.json", `{"status":"OK","results":[]}`)
	// The extra submissions file that this one lists is missing.
	record(t, bs, at, "sec/submissions/CIK0000021344.json",
		`{"cik":"21344","name":"KO","filings":{"recent":{},"files":[{"name":"CIK0000021344-submissions-001.json"}]}}`)
	r, pool := offlineRunner(t, e, bs)
	res, err := r.Reprocess(context.Background(), ingest.ReprocessOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Incomplete != 3 || res.Files != 0 {
		t.Errorf("Reprocess() = %+v, want 3 incomplete files and none loaded", res)
	}
	db := &env{pool: pool}
	if n := db.count(t, `SELECT (SELECT count(*) FROM price_grouped_loads) + (SELECT count(*) FROM companies)`); n != 0 {
		t.Errorf("Reprocess() stored %d rows of incomplete responses, want none", n)
	}
}

func TestReprocessLoadsTheOtherFilesWhenOneFails(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	bs := newBlob(t)
	record(t, bs, "2026-10-30T12:00:00Z", "prices/massive/splits/KO/page-1.json", `{"status":"OK","results":[]}`)
	record(t, bs, "2026-10-30T12:00:00Z", "prices/massive/dividends/KO/page-1.json", `{"status":"OK","results":[]}`)
	record(t, bs, "2026-10-30T12:00:00Z", "sec/companyfacts/CIK0000021344.json", `not json`)
	record(t, bs, "2026-10-30T12:00:00Z", "sec/unknown/file.json", `{}`)
	r, pool := offlineRunner(t, e, bs)
	res, err := r.Reprocess(context.Background(), ingest.ReprocessOptions{})
	if err == nil {
		t.Fatal("Reprocess() error = nil, want the failures")
	}
	if res.Failed != 2 || res.Files != 2 {
		t.Errorf("Reprocess() = %+v, want 2 failed files and 2 loaded", res)
	}
	db := &env{pool: pool}
	if n := db.count(t, `SELECT count(*) FROM price_reference_checks WHERE symbol = 'KO'`); n != 1 {
		t.Error("Reprocess() did not load the splits and dividends of KO")
	}
	if n := db.count(t, `SELECT count(*) FROM ingest_runs WHERE job = 'reprocess' AND status = 'failed'`); n != 1 {
		t.Errorf("failed reprocess runs = %d, want 1", n)
	}
}
