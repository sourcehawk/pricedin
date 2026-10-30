//go:build integration

package ingest_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

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

	// The older version is not written again before the newer one.
	before := tableRows(t, pool, map[string][]string{"dividends": {}, "price_splits": {}})
	res, err = r.Reprocess(context.Background(), ingest.ReprocessOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Dividends != 0 || res.Fundamentals.Companies != 0 {
		t.Errorf("second Reprocess() = %+v, want no dividend written and no rebuild", res)
	}
	if diff := cmp.Diff(before, tableRows(t, pool, map[string][]string{"dividends": {}, "price_splits": {}})); diff != "" {
		t.Errorf("second reprocess wrote the dividends again (-before +after):\n%s", diff)
	}
}

// A ticker map that repeats the one of the day before opens no row, but
// it moves the day of the last sync, as the job did.
func TestReprocessKeepsTheDaysOfARepeatedTickerMap(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	bs := newBlob(t)
	tickers := `{"fields":["cik","name","ticker","exchange"],"data":[[21344,"Coca-Cola Co","KO","NYSE"]]}`
	for _, at := range []string{"2026-10-27T12:00:00Z", "2026-10-28T12:00:00Z", "2026-10-29T12:00:00Z"} {
		record(t, bs, at, "sec/tickers/company_tickers_exchange.json", tickers)
	}
	r, pool := offlineRunner(t, e, bs)
	res, err := r.Reprocess(context.Background(), ingest.ReprocessOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 2 || res.Unchanged != 1 {
		t.Errorf("Reprocess() = %+v, want the first and the last map loaded, and the one between them unchanged", res)
	}
	ticker := must(store.New(pool).GetCurrentTicker(context.Background(), "KO"))(t)
	if ticker.ValidFrom != mustDate("2026-10-27") {
		t.Errorf("KO valid from %s, want 2026-10-27, the first map", ticker.ValidFrom)
	}
	synced := must(store.New(pool).GetTickerSyncDate(context.Background(), "sec"))(t)
	if synced != mustDate("2026-10-29") {
		t.Errorf("SEC tickers synced on %s, want 2026-10-29, the last map", synced)
	}
}

// The pages of two versions of a ticker list from one day cannot be
// paired, and a union of them would keep a symbol that the newer list
// dropped.
func TestReprocessSkipsATickerListWithTwoVersionsOfAPage(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	bs := newBlob(t)
	at := "2026-10-30T12:00:00Z"
	next := `,"next_url":"https://api.massive.com/v3/reference/tickers?cursor=p2"`
	record(t, bs, at, "prices/massive/tickers/stocks-active/page-1.json", `{"status":"OK","results":[{"ticker":"KO","cik":"0000021344"}]`+next+`}`)
	record(t, bs, at, "prices/massive/tickers/stocks-active/page-1.json", `{"status":"OK","results":[{"ticker":"KOF","cik":"0000021344"}]`+next+`}`)
	record(t, bs, at, "prices/massive/tickers/stocks-active/page-2.json", `{"status":"OK","results":[]}`)
	r, _ := offlineRunner(t, e, bs)
	res, err := r.Reprocess(context.Background(), ingest.ReprocessOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Incomplete != 3 || res.TickersOpened != 0 {
		t.Errorf("Reprocess() = %+v, want the 3 pages skipped as incomplete", res)
	}
}

// A submissions file that did not change still loads the newer version of
// an extra file that it lists.
func TestReprocessLoadsTheNewerVersionOfAnExtraSubmissionsFile(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	bs := newBlob(t)
	main := `{"cik":"21344","name":"KO","filings":{"recent":{},"files":[{"name":"CIK0000021344-submissions-001.json"}]}}`
	extra := func(accessions ...string) string {
		n := len(accessions)
		acc, form, filed, report, doc := make([]string, 0, n), make([]string, 0, n), make([]string, 0, n), make([]string, 0, n), make([]string, 0, n)
		for _, a := range accessions {
			acc, form, filed, report, doc = append(acc, `"`+a+`"`), append(form, `"10-K"`), append(filed, `"2020-02-20"`),
				append(report, `"2019-12-31"`), append(doc, `"a.htm"`)
		}
		join := func(v []string) string { return "[" + strings.Join(v, ",") + "]" }
		return `{"accessionNumber":` + join(acc) + `,"form":` + join(form) + `,"filingDate":` + join(filed) +
			`,"reportDate":` + join(report) + `,"primaryDocument":` + join(doc) + `}`
	}
	record(t, bs, "2026-10-28T12:00:00Z", "sec/submissions/CIK0000021344.json", main)
	record(t, bs, "2026-10-28T12:00:00Z", "sec/submissions/CIK0000021344-submissions-001.json", extra("0000021344-20-000001"))
	record(t, bs, "2026-10-29T12:00:00Z", "sec/submissions/CIK0000021344.json", main)
	record(t, bs, "2026-10-29T12:00:00Z", "sec/submissions/CIK0000021344-submissions-001.json",
		extra("0000021344-20-000001", "0000021344-20-000002"))
	r, pool := offlineRunner(t, e, bs)
	must(r.Reprocess(context.Background(), ingest.ReprocessOptions{}))(t)
	db := &env{pool: pool}
	if n := db.count(t, `SELECT count(*) FROM filings WHERE cik = 21344`); n != 2 {
		t.Errorf("filings of KO = %d, want 2, with the filing of the newer extra file", n)
	}
}

// hookBlob runs hook once, before the first Get.
type hookBlob struct {
	blob.Store
	once sync.Once
	hook func()
}

func (h *hookBlob) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	h.once.Do(h.hook)
	return h.Store.Get(ctx, key)
}

// A job that stores a newer response while a reprocess runs keeps its
// value: the reprocess loads that response again after the older ones.
func TestReprocessDoesNotLeaveAnOlderValueOverTheValueOfAJobThatRanMeanwhile(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	bs := newBlob(t)
	dividend := func(amount string) string {
		return `{"status":"OK","results":[{"id":"d1","ticker":"KO","ex_dividend_date":"2026-09-15","cash_amount":` + amount + `}]}`
	}
	recordReference := func(at, amount string) {
		record(t, bs, at, "prices/massive/splits/KO/page-1.json", `{"status":"OK","results":[]}`)
		record(t, bs, at, "prices/massive/dividends/KO/page-1.json", dividend(amount))
	}
	recordReference("2026-10-28T12:00:00Z", "0.5")
	recordReference("2026-10-29T12:00:00Z", "0.6")
	hb := &hookBlob{Store: bs}
	r, pool := offlineRunner(t, e, hb)
	// The job records the corrected response first, then stores its value.
	hb.hook = func() {
		recordReference("2026-10-30T12:00:00Z", "0.7")
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO dividends (provider_id, symbol, ex_dividend_date, cash_amount, source)
			VALUES ('d1', 'KO', '2026-09-15', 0.7, 'massive')
			ON CONFLICT (provider_id) DO UPDATE SET cash_amount = 0.7`); err != nil {
			t.Error(err)
		}
	}
	must(r.Reprocess(context.Background(), ingest.ReprocessOptions{}))(t)
	var amount float64
	if err := pool.QueryRow(context.Background(), `SELECT cash_amount FROM dividends WHERE provider_id = 'd1'`).Scan(&amount); err != nil {
		t.Fatal(err)
	}
	if amount != 0.7 {
		t.Errorf("cash amount = %v, want 0.7, the value of the job that ran meanwhile", amount)
	}
}

// sec-bulk stores the archive facts of a covered company before it asks
// for its submissions, so the company keeps its facts when that request
// fails.
func TestReprocessLoadsTheArchiveFactsOfACoveredCompanyWithoutSubmissions(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{wrap: func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/submissions/CIK0000021344.json") {
				http.NotFound(w, r)
				return
			}
			h.ServeHTTP(w, r)
		})
	}})
	ctx := context.Background()
	jobs := e.newRunner(t, e.pool, 30, reprocessUniverse())
	must(jobs.SECTickers(ctx))(t)
	must(jobs.SECBulk(ctx, ingest.BulkArchive))(t)
	const facts = `SELECT count(*) FROM facts WHERE cik = 21344`
	if e.count(t, facts) == 0 {
		t.Fatal("the jobs stored no archive facts of KO; the test needs them")
	}
	r, pool := offlineRunner(t, e, e.blob)
	must(r.Reprocess(ctx, ingest.ReprocessOptions{Prefix: ingest.ReprocessSEC}))(t)
	db := &env{pool: pool}
	if got, want := db.count(t, facts), e.count(t, facts); got != want {
		t.Errorf("facts of KO after reprocess = %d, want the %d of the jobs", got, want)
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

// barsBody is a Massive aggregates page with one bar of KO on 2025-10-29,
// at close.
func barsBody(grouped bool, closePrice string) string {
	ticker := `"ticker":"KO",`
	bar := `{"t":1761710400000,"c":` + closePrice + `}`
	if grouped {
		ticker, bar = "", `{"T":"KO","t":1761710400000,"c":`+closePrice+`}`
	}
	return `{"status":"OK",` + ticker + `"results":[` + bar + `]}`
}

func koClose(t *testing.T, pool *pgxpool.Pool) float64 {
	t.Helper()
	var c float64
	if err := pool.QueryRow(context.Background(), `SELECT close FROM prices_daily WHERE symbol = 'KO' AND date = '2025-10-29'`).Scan(&c); err != nil {
		t.Fatal(err)
	}
	return c
}

// A bar that the bars of one symbol and a grouped day both give keeps the
// value of the newest response, as it did for the jobs.
func TestReprocessKeepsTheBarOfTheNewestResponseAcrossSources(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	bs := newBlob(t)
	const aggs = "prices/massive/aggs/KO/2025-10-01_2025-10-29/page-1.json"
	record(t, bs, "2026-10-27T12:00:00Z", aggs, barsBody(false, "1"))
	record(t, bs, "2026-10-28T12:00:00Z", "prices/massive/grouped/2025-10-29/page-1.json", barsBody(true, "2"))
	record(t, bs, "2026-10-29T12:00:00Z", aggs, barsBody(false, "3"))
	r, pool := offlineRunner(t, e, bs)
	must(r.Reprocess(context.Background(), ingest.ReprocessOptions{}))(t)
	if c := koClose(t, pool); c != 3 {
		t.Errorf("close = %v, want 3 of the newest response", c)
	}
}

// The jobs mark a grouped day loaded only when the provider returned bars,
// so a retry after an empty answer stores the bars of the retry.
func TestReprocessStoresTheGroupedBarsOfARetryAfterAnEmptyAnswer(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	bs := newBlob(t)
	const grouped = "prices/massive/grouped/2025-10-29/page-1.json"
	record(t, bs, "2026-10-28T12:00:00Z", grouped, `{"status":"OK","results":[]}`)
	record(t, bs, "2026-10-29T12:00:00Z", grouped, barsBody(true, "2"))
	// The splits give KO a price file of its own, so its grouped bars stay.
	record(t, bs, "2026-10-29T12:00:00Z", "prices/massive/splits/KO/page-1.json", `{"status":"OK","results":[]}`)
	record(t, bs, "2026-10-29T12:00:00Z", "prices/massive/dividends/KO/page-1.json", `{"status":"OK","results":[]}`)
	r, pool := offlineRunner(t, e, bs)
	must(r.Reprocess(context.Background(), ingest.ReprocessOptions{}))(t)
	if c := koClose(t, pool); c != 2 {
		t.Errorf("close = %v, want 2 of the retry", c)
	}
	db := &env{pool: pool}
	if n := db.count(t, `SELECT count(*) FROM price_grouped_loads WHERE date = '2025-10-29' AND bars = 1`); n != 1 {
		t.Error("the grouped day is not marked loaded with the 1 bar of the retry")
	}
}

// An archive older than the first file that stores a company loads the
// other companies, and fails nothing.
func TestReprocessLoadsAnArchiveOlderThanACompanyOfIt(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	bs := newBlob(t)
	facts := `{"cik":21344,"facts":{"us-gaap":{"EarningsPerShareDiluted":{"units":{"USD/shares":[` +
		`{"start":"2025-01-01","end":"2025-03-31","val":0.5,"accn":"0000021344-25-000001","fy":2025,"fp":"Q1","form":"10-Q","filed":"2025-04-25"}]}}}}}`
	var zipped bytes.Buffer
	zw := zip.NewWriter(&zipped)
	w, err := zw.Create("CIK0000021344.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(facts)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	record(t, bs, "2026-10-28T12:00:00Z", "sec/archives/companyfacts.zip", zipped.String())
	record(t, bs, "2026-10-29T12:00:00Z", "sec/submissions/CIK0000021344.json", `{"cik":"21344","name":"KO","filings":{"recent":{}}}`)
	r, pool := offlineRunner(t, e, bs)
	res, err := r.Reprocess(context.Background(), ingest.ReprocessOptions{})
	if err != nil || res.Failed != 0 {
		t.Fatalf("Reprocess() = %+v, %v; want no failure", res, err)
	}
	db := &env{pool: pool}
	if n := db.count(t, `SELECT count(*) FROM companies WHERE cik = 21344`); n != 1 {
		t.Error("the submissions of the later day did not load")
	}

	// The company did not exist on the day of the archive, also on a
	// second run, after the first run stored it.
	before := tableRows(t, pool, map[string][]string{"facts": {"id"}, "quarterly_fundamentals": {"built_at"}})
	res, err = r.Reprocess(context.Background(), ingest.ReprocessOptions{})
	if err != nil || res.Facts != 0 || res.Fundamentals.Companies != 0 {
		t.Errorf("second Reprocess() = %+v, %v; want no fact and no rebuild", res, err)
	}
	if diff := cmp.Diff(before, tableRows(t, pool, map[string][]string{"facts": {"id"}, "quarterly_fundamentals": {"built_at"}})); diff != "" {
		t.Errorf("second reprocess changed the facts (-before +after):\n%s", diff)
	}
}

// koArchive returns a company facts archive with one fact of KO.
func koArchive(t *testing.T) string {
	t.Helper()
	facts := `{"cik":21344,"facts":{"us-gaap":{"EarningsPerShareDiluted":{"units":{"USD/shares":[` +
		`{"start":"2025-01-01","end":"2025-03-31","val":0.5,"accn":"0000021344-25-000001","fy":2025,"fp":"Q1","form":"10-Q","filed":"2025-04-25"}]}}}}}`
	var zipped bytes.Buffer
	zw := zip.NewWriter(&zipped)
	w, err := zw.Create("CIK0000021344.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(facts)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return zipped.String()
}

// An archive that repeats the one of the day before still loads the
// companies that exist only from its own day.
func TestReprocessLoadsARepeatedArchiveForACompanyOfItsDay(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	bs := newBlob(t)
	record(t, bs, "2026-10-28T12:00:00Z", "sec/archives/companyfacts.zip", koArchive(t))
	record(t, bs, "2026-10-29T12:00:00Z", "sec/archives/companyfacts.zip", koArchive(t))
	record(t, bs, "2026-10-29T12:00:00Z", "sec/submissions/CIK0000021344.json", `{"cik":"21344","name":"KO","filings":{"recent":{}}}`)
	r, pool := offlineRunner(t, e, bs)
	must(r.Reprocess(context.Background(), ingest.ReprocessOptions{}))(t)
	db := &env{pool: pool}
	if n := db.count(t, `SELECT count(*) FROM facts WHERE cik = 21344`); n != 1 {
		t.Errorf("facts of KO = %d, want the 1 fact of the archive of its day", n)
	}
	res := must(r.Reprocess(context.Background(), ingest.ReprocessOptions{}))(t)
	if res.Facts != 0 || res.Fundamentals.Companies != 0 {
		t.Errorf("second Reprocess() = %+v, want no fact and no rebuild", res)
	}
}

// A request whose pages, or whose splits and dividends, were recorded on
// both sides of midnight UTC is one response.
func TestReprocessJoinsAResponseRecordedAcrossMidnight(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	bs := newBlob(t)
	before, after := "2026-10-28T23:59:59Z", "2026-10-29T00:00:01Z"
	record(t, bs, before, "prices/massive/splits/KO/page-1.json", `{"status":"OK","results":[]}`)
	record(t, bs, after, "prices/massive/dividends/KO/page-1.json",
		`{"status":"OK","results":[{"id":"d1","ticker":"KO","ex_dividend_date":"2026-09-15","cash_amount":0.51}]}`)
	const grouped = "prices/massive/grouped/2025-10-29/"
	record(t, bs, before, grouped+"page-1.json",
		`{"status":"OK","results":[{"T":"KO","t":1761710400000,"c":70}],"next_url":"https://api.massive.com/v2/aggs?cursor=p2"}`)
	record(t, bs, after, grouped+"page-2.json", `{"status":"OK","results":[{"T":"KOF","t":1761710400000,"c":80}]}`)
	r, pool := offlineRunner(t, e, bs)
	res, err := r.Reprocess(context.Background(), ingest.ReprocessOptions{})
	if err != nil || res.Incomplete != 0 {
		t.Fatalf("Reprocess() = %+v, %v; want nothing incomplete", res, err)
	}
	db := &env{pool: pool}
	if n := db.count(t, `SELECT count(*) FROM dividends WHERE provider_id = 'd1'`); n != 1 {
		t.Error("the dividend recorded after midnight did not load with the splits")
	}
	if n := db.count(t, `SELECT count(*) FROM price_grouped_loads WHERE date = '2025-10-29' AND bars = 2`); n != 1 {
		t.Error("the grouped day is not marked loaded with the 2 bars of both pages")
	}
}

// A version that does not parse fails alone: the other versions of its
// source still load.
func TestReprocessLoadsTheOtherVersionsOfASourceWhenOneDoesNotParse(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	bs := newBlob(t)
	record(t, bs, "2026-10-28T12:00:00Z", "sec/submissions/CIK0000021344.json", `not json`)
	record(t, bs, "2026-10-29T12:00:00Z", "sec/submissions/CIK0000021344.json", `{"cik":"21344","name":"KO","filings":{"recent":{}}}`)
	const grouped = "prices/massive/grouped/2025-10-29/page-1.json"
	record(t, bs, "2026-10-28T12:00:00Z", grouped, `not json`)
	record(t, bs, "2026-10-29T12:00:00Z", grouped, barsBody(true, "2"))
	record(t, bs, "2026-10-29T12:00:00Z", "prices/massive/splits/KO/page-1.json", `{"status":"OK","results":[]}`)
	record(t, bs, "2026-10-29T12:00:00Z", "prices/massive/dividends/KO/page-1.json", `{"status":"OK","results":[]}`)
	r, pool := offlineRunner(t, e, bs)
	res, err := r.Reprocess(context.Background(), ingest.ReprocessOptions{})
	if err == nil || res.Failed != 2 {
		t.Errorf("Reprocess() = %+v, %v; want the 2 versions that do not parse as failed", res, err)
	}
	db := &env{pool: pool}
	if n := db.count(t, `SELECT count(*) FROM companies WHERE cik = 21344`); n != 1 {
		t.Error("the submissions version that parses did not load")
	}
	if c := koClose(t, pool); c != 2 {
		t.Errorf("close = %v, want 2 of the grouped version that parses", c)
	}
}

// A per-symbol response that repeats an older one is still the newest
// response, so its bar wins over a grouped day between them.
func TestReprocessKeepsTheBarOfARepeatedNewestResponse(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	bs := newBlob(t)
	const aggs = "prices/massive/aggs/KO/2025-10-01_2025-10-29/page-1.json"
	record(t, bs, "2026-10-27T12:00:00Z", aggs, barsBody(false, "1"))
	record(t, bs, "2026-10-28T12:00:00Z", "prices/massive/grouped/2025-10-29/page-1.json", barsBody(true, "2"))
	record(t, bs, "2026-10-29T12:00:00Z", aggs, barsBody(false, "1"))
	r, pool := offlineRunner(t, e, bs)
	must(r.Reprocess(context.Background(), ingest.ReprocessOptions{}))(t)
	if c := koClose(t, pool); c != 1 {
		t.Errorf("close = %v, want 1 of the newest response", c)
	}
}

// A newer per-symbol response that gives no bar does not make the older
// bars win over a grouped day between them.
func TestReprocessOrdersBarsByTheNewestResponseThatGivesBars(t *testing.T) {
	for name, newest := range map[string]string{
		"does not parse": `not json`,
		"is empty":       `{"status":"OK","ticker":"KO","results":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, envOptions{})
			bs := newBlob(t)
			const aggs = "prices/massive/aggs/KO/2025-10-01_2025-10-29/page-1.json"
			record(t, bs, "2026-10-27T12:00:00Z", aggs, barsBody(false, "1"))
			record(t, bs, "2026-10-28T12:00:00Z", "prices/massive/grouped/2025-10-29/page-1.json", barsBody(true, "2"))
			record(t, bs, "2026-10-29T12:00:00Z", aggs, newest)
			r, pool := offlineRunner(t, e, bs)
			_, _ = r.Reprocess(context.Background(), ingest.ReprocessOptions{})
			if c := koClose(t, pool); c != 2 {
				t.Errorf("close = %v, want 2 of the grouped day, the newest response with the bar", c)
			}
		})
	}
}

// A copy of a response that does not parse fails alone: another copy of
// the same day still loads.
func TestReprocessLoadsTheValidCopyOfADayWhenAnotherDoesNotParse(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	bs := newBlob(t)
	at := "2026-10-29T12:00:00Z"
	record(t, bs, at, "prices/massive/grouped/2025-10-29/page-1.json", `not json`)
	record(t, bs, at, "prices/massive/grouped/2025-10-29/page-1.json", barsBody(true, "2"))
	record(t, bs, at, "prices/massive/splits/KO/page-1.json", `{"status":"OK","results":[]}`)
	record(t, bs, at, "prices/massive/dividends/KO/page-1.json", `{"status":"OK","results":[]}`)
	record(t, bs, at, "sec/submissions/CIK0000021344.json", `{"cik":"21344","name":"KO","filings":{"recent":{}}}`)
	record(t, bs, "2026-10-30T12:00:00Z", "sec/companyfacts/CIK0000021344.json", `not json`)
	record(t, bs, "2026-10-30T12:00:00Z", "sec/companyfacts/CIK0000021344.json", `{"cik":21344,"facts":{"us-gaap":{"EarningsPerShareDiluted":{"units":{"USD/shares":[`+
		`{"start":"2025-01-01","end":"2025-03-31","val":0.5,"accn":"0000021344-25-000001","fy":2025,"fp":"Q1","form":"10-Q","filed":"2025-04-25"}]}}}}}`)
	r, pool := offlineRunner(t, e, bs)
	res, err := r.Reprocess(context.Background(), ingest.ReprocessOptions{})
	if err == nil || res.Failed != 2 {
		t.Errorf("Reprocess() = %+v, %v; want the 2 copies that do not parse as failed", res, err)
	}
	if c := koClose(t, pool); c != 2 {
		t.Errorf("close = %v, want 2 of the valid grouped copy", c)
	}
	db := &env{pool: pool}
	if n := db.count(t, `SELECT count(*) FROM facts WHERE cik = 21344`); n != 1 {
		t.Errorf("facts of KO = %d, want the 1 fact of the valid copy", n)
	}
}
