//go:build integration

package ingest_test

import (
	"context"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/fixtureserver"
	"github.com/sourcehawk/pricedin/server/internal/ingest"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

// A company whose facts changed keeps its pending mark until a rebuild
// succeeds, also when the run that loaded the facts ended before it.
func TestAPendingRebuildSurvivesUntilARebuildRuns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, envOptions{now: "2026-08-27T12:00:00Z"})
	must(e.runner.SECTickers(ctx))(t)
	// A sec-daily run whose daily chain stopped before the rebuild.
	must(e.runner.SECDaily(ctx))(t)
	if n := e.count(t, `SELECT count(*) FROM quarterly_fundamentals WHERE cik = 1045810`); n != 0 {
		t.Fatalf("NVDA has %d derived values before any rebuild", n)
	}

	res := must(e.runner.Daily(ctx))(t)
	if res.Fundamentals.Companies == 0 {
		t.Errorf("Daily() rebuilt no company, want NVDA among them")
	}
	if n := e.count(t, `SELECT count(*) FROM quarterly_fundamentals WHERE cik = 1045810`); n == 0 {
		t.Error("NVDA has no derived values after the daily run")
	}
	if pending := must(e.store.ListPendingFundamentals(ctx))(t); len(pending) != 0 {
		t.Errorf("pending after the rebuild = %v, want none", pending)
	}
}

// An announced split counts for the per-share values only from the day on
// which the adjusted prices apply it too: when a bar on or after its date
// exists.
func TestAnAnnouncedSplitWaitsForItsFirstBar(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, envOptions{now: "2026-10-27T12:00:00Z"})
	must(e.runner.SECTickers(ctx))(t)
	must(e.runner.LoadCompanyResult(ctx, "NVDA"))(t)
	before := latestEPS(t, e)

	// A 2-for-1 split, announced for 2026-10-28.
	if _, err := e.store.UpsertPriceSplits(ctx, []domain.Split{{
		Symbol: "NVDA", Date: domain.NewDate(2026, 10, 28), From: 1, To: 2, Source: "massive", Evidence: "announced",
	}}); err != nil {
		t.Fatal(err)
	}
	must(e.runner.FundamentalsRebuild(ctx, "NVDA"))(t)
	if got := latestEPS(t, e); got != before {
		t.Errorf("EPS before the split's first bar = %v, want %v unchanged", got, before)
	}

	// The daily price load reaches the split date and marks NVDA pending.
	e.clock.Set("2026-10-30T12:00:00Z")
	must(e.runner.PricesDaily(ctx))(t)
	if diff := cmp.Diff([]domain.CIK{1045810}, must(e.store.ListPendingFundamentals(ctx))(t)); diff != "" {
		t.Errorf("pending after the split's first bar (-want +got):\n%s", diff)
	}
	must(e.runner.FundamentalsRebuild(ctx, "pending"))(t)
	if got := latestEPS(t, e); math.Abs(got-before/2) > 1e-6 {
		t.Errorf("EPS after the split's first bar = %v, want %v", got, before/2)
	}
}

func latestEPS(t *testing.T, e *env) float64 {
	t.Helper()
	var v float64
	if err := e.pool.QueryRow(context.Background(), `
		SELECT value FROM quarterly_fundamentals WHERE cik = 1045810 AND metric = 'eps_diluted'
		ORDER BY fiscal_year DESC, fiscal_quarter DESC LIMIT 1`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// A day that failed during a catch-up is tried again by the next daily
// run, also when it is older than the 30 days that a daily run checks.
func TestPricesDailyRetriesAnOldDayThatFailed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const failing = "2026-09-15"
	var fail atomic.Bool
	fail.Store(true)
	e := newEnv(t, envOptions{backfillDays: 40, wrap: func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if fail.Load() && strings.HasSuffix(r.URL.Path, "/grouped/locale/us/market/stocks/"+failing) {
				http.Error(w, `{"status":"ERROR"}`, http.StatusInternalServerError)
				return
			}
			h.ServeHTTP(w, r)
		})
	}})
	must(e.runner.SECTickers(ctx))(t)
	if _, err := e.runner.PricesBackfill(ctx); err == nil {
		t.Fatal("PricesBackfill() error = nil, want the failed day")
	}
	fail.Store(false)
	res := must(e.runner.PricesDaily(ctx))(t)
	if res.Loaded != 1 {
		t.Errorf("PricesDaily() = %+v, want the failed day loaded", res)
	}
	if n := e.count(t, `SELECT count(*) FROM prices_daily WHERE date = $1`, failing); n != 5 {
		t.Errorf("%s: %d bars, want 5", failing, n)
	}
}

// A symbol added to the universe after its days were loaded gets them from
// the next backfill.
func TestPricesBackfillFillsASymbolAddedToTheUniverse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, envOptions{})
	must(e.runner.SECTickers(ctx))(t)
	narrow := e.newRunner(t, e.pool, 5, []string{"NVDA"})
	must(narrow.PricesBackfill(ctx))(t)
	if n := e.count(t, `SELECT count(*) FROM prices_daily WHERE symbol = 'KO'`); n != 0 {
		t.Fatalf("KO has %d bars before it joined the universe", n)
	}

	wider := e.newRunner(t, e.pool, 5, []string{"NVDA", "KO"})
	res := must(wider.PricesBackfill(ctx))(t)
	if res.SymbolsFilled != 1 || res.Days != 0 {
		t.Errorf("PricesBackfill() = %+v, want KO filled and no grouped day loaded again", res)
	}
	if n := e.count(t, `SELECT count(*) FROM prices_daily WHERE symbol = 'KO' AND date >= '2026-10-23'`); n != 5 {
		t.Errorf("KO has %d bars in the backfilled days, want 5", n)
	}
}

// Two on-demand loads on an empty database both wait for the one fresh
// copy of the SEC ticker map.
func TestConcurrentLoadsShareOneTickerRefresh(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, envOptions{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, symbol := range []string{"NVDA", "KO"} {
		wg.Go(func() { errs[i] = e.runner.LoadCompany(ctx, symbol) })
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("load %d: %v", i, err)
		}
	}
	if n := e.count(t, `SELECT count(*) FROM ingest_runs WHERE job = 'sec-tickers' AND stats->>'tickers' <> '0'`); n != 1 {
		t.Errorf("%d sec-tickers runs fetched the map, want 1", n)
	}
}

// When one process hands the SEC lock to another, the second one waits a
// second after the first one's last request, so the two rate limiters
// never add up to more than the SEC limit.
func TestTheSECLockHandoffKeepsASecondApart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var mu sync.Mutex
	var times []time.Time
	e := newEnv(t, envOptions{wrap: func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, fixtureserver.PrefixSECData) {
				mu.Lock()
				times = append(times, time.Now())
				mu.Unlock()
			}
			h.ServeHTTP(w, r)
		})
	}})
	must(e.runner.SECTickers(ctx))(t)
	must(e.runner.SECCompany(ctx, "KO"))(t)
	mu.Lock()
	first := len(times)
	mu.Unlock()

	other := e.newRunner(t, newPool(t, e.url), 0, nil)
	must(other.SECCompany(ctx, "GRMN"))(t)
	mu.Lock()
	defer mu.Unlock()
	if gap := times[first].Sub(times[first-1]); gap < 950*time.Millisecond {
		t.Errorf("the other process's first request came %v after the last one, want at least a second", gap)
	}
}

// An on-demand load brings every share class of the company up to date,
// because the quarters' split basis is the newest bar of any class.
func TestLoadCompanyBringsEveryShareClassUpToDate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, envOptions{})
	must(e.runner.SECTickers(ctx))(t)
	// The fixtures have no company with two priced classes, so GRMN plays
	// a second class of NVDA.
	if _, err := e.store.SyncTickers(ctx, "sec", domain.NewDate(2026, 10, 30), []domain.Ticker{
		{Symbol: "NVDA", CIK: 1045810, Exchange: "Nasdaq"}, {Symbol: "GRMN", CIK: 1045810, Exchange: "NYSE"},
	}); err != nil {
		t.Fatal(err)
	}
	res := must(e.runner.LoadCompanyResult(ctx, "NVDA"))(t)
	if res.Bars < 2*1200 {
		t.Errorf("LoadCompanyResult() bars = %d, want five years of both classes", res.Bars)
	}
	if n := e.count(t, `SELECT count(*) FROM prices_daily WHERE symbol = 'GRMN'`); n < 1200 {
		t.Errorf("GRMN has %d bars, want its five years", n)
	}
}

// An SEC split that the provider does not list, dated after the newest
// bar, waits for its first bar, and that bar marks the company pending.
func TestADeferredSECOnlySplitMarksTheCompanyWhenItsFirstBarArrives(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// The last finished session is 2024-05-28; NVDA's filings date its
	// 10-for-1 split on 2024-05-30.
	e := newEnv(t, envOptions{now: "2024-05-29T12:00:00Z"})
	must(e.runner.SECTickers(ctx))(t)
	must(e.runner.LoadCompanyResult(ctx, "NVDA"))(t)
	// The provider lists no split: an unchanged, empty response.
	if _, err := e.pool.Exec(ctx, `DELETE FROM price_splits WHERE symbol = 'NVDA'`); err != nil {
		t.Fatal(err)
	}
	must(e.runner.FundamentalsRebuild(ctx, "NVDA"))(t)
	if n := e.count(t, `SELECT count(*) FROM stock_splits WHERE cik = 1045810 AND source = 'sec' AND effective_date > '2024-05-28'`); n != 1 {
		t.Fatalf("%d deferred SEC splits stored, want the 2024 split", n)
	}

	e.clock.Set("2024-06-04T12:00:00Z")
	must(e.runner.PricesDaily(ctx))(t)
	if diff := cmp.Diff([]domain.CIK{1045810}, must(e.store.ListPendingFundamentals(ctx))(t)); diff != "" {
		t.Errorf("pending after the split's first bar (-want +got):\n%s", diff)
	}
}

// A run of a process that died stays "running" until the next run of its
// job takes the lock and marks it abandoned.
func TestAJobMarksTheRunOfADeadProcessAbandoned(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, envOptions{})
	must(e.runner.SECTickers(ctx))(t)
	dead, _, err := e.store.StartIngestRunForLock(ctx, ingest.JobPricesDaily, ingest.JobPricesDaily)
	if err != nil {
		t.Fatal(err)
	}
	must(e.runner.PricesDaily(ctx))(t)
	run := must(e.store.GetIngestRun(ctx, dead))(t)
	if run.Status != store.RunFailed || run.Error != "abandoned" {
		t.Errorf("the dead run = %s %q, want failed and abandoned", run.Status, run.Error)
	}
}

// A company that fails does not hold the cursor back: sec-daily moves on,
// and a later run tries the company again once its wait is over.
func TestSECDailyMovesOnAndRetriesAFailedCompany(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var fail atomic.Bool
	fail.Store(true)
	e := newEnv(t, envOptions{now: "2026-08-27T12:00:00Z", wrap: func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if fail.Load() && strings.HasSuffix(r.URL.Path, "/companyfacts/CIK0001045810.json") {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			h.ServeHTTP(w, r)
		})
	}})
	must(e.runner.SECTickers(ctx))(t)
	res := must(e.runner.SECDaily(ctx))(t)
	if res.Failed != 1 || res.RetriesWaiting != 1 {
		t.Errorf("SECDaily() = %+v, want NVDA failed and waiting", res)
	}
	if cursor := must(e.store.GetIngestCursor(ctx, ingest.JobSECDaily))(t); cursor != domain.NewDate(2026, 8, 26) {
		t.Errorf("cursor = %s, want 2026-08-26 although NVDA failed", cursor)
	}

	fail.Store(false)
	// Later indexes without a filing of NVDA: only the retry loads it.
	if err := e.store.SetIngestCursor(ctx, ingest.JobSECDaily, domain.NewDate(2026, 8, 28)); err != nil {
		t.Fatal(err)
	}
	// Before the wait of 6 hours is over, NVDA is not tried again.
	e.clock.Set("2026-08-27T13:00:00Z")
	if res := must(e.runner.SECDaily(ctx))(t); res.RetriesDue != 0 || res.Companies != 0 {
		t.Errorf("SECDaily() an hour later = %+v, want no retry yet", res)
	}
	e.clock.Set("2026-08-31T12:00:00Z")
	res = must(e.runner.SECDaily(ctx))(t)
	if res.RetriesDue != 1 || res.Failed != 0 || res.RetriesWaiting != 0 {
		t.Errorf("SECDaily() after the wait = %+v, want NVDA tried again and loaded", res)
	}
	if n := e.count(t, `SELECT count(*) FROM facts WHERE cik = 1045810`); n == 0 {
		t.Error("NVDA has no facts after its retry")
	}
}

// A company loaded on demand outside the universe stays current: the daily
// price job keeps its bars.
func TestDailyJobsKeepACompanyLoadedOnDemandCurrent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, envOptions{now: "2026-10-27T12:00:00Z"})
	narrow := e.newRunner(t, e.pool, 0, []string{"NVDA"})
	must(narrow.SECTickers(ctx))(t)
	must(narrow.LoadCompanyResult(ctx, "KO"))(t)

	e.clock.Set("2026-10-30T12:00:00Z")
	must(narrow.PricesDaily(ctx))(t)
	if n := e.count(t, `SELECT count(*) FROM prices_daily WHERE symbol = 'KO' AND date > '2026-10-26'`); n != 3 {
		t.Errorf("KO has %d bars after its load, want the 3 new days", n)
	}
}

// sec-bulk leaves sec-daily a cursor at its start, so the filings between
// the bulk load and the first daily run are read.
func TestSECBulkSetsTheSECDailyCursor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, envOptions{})
	must(e.runner.SECTickers(ctx))(t)
	must(e.runner.SECBulk(ctx, ingest.BulkAPI))(t)
	if cursor := must(e.store.GetIngestCursor(ctx, ingest.JobSECDaily))(t); cursor != domain.NewDate(2026, 10, 30) {
		t.Errorf("cursor = %s, want the bulk load's day, 2026-10-30", cursor)
	}
}

// A second on-demand load asks only for the bars after the newest one.
func TestLoadCompanyFetchesOnlyTheBarsAfterTheNewestOne(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var mu sync.Mutex
	var ranges []string
	e := newEnv(t, envOptions{now: "2026-10-27T12:00:00Z", wrap: func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rest, ok := strings.CutPrefix(r.URL.Path, fixtureserver.PrefixMassive+"/v2/aggs/ticker/NVDA/range/1/day/"); ok {
				mu.Lock()
				ranges = append(ranges, rest)
				mu.Unlock()
			}
			h.ServeHTTP(w, r)
		})
	}})
	must(e.runner.LoadCompanyResult(ctx, "NVDA"))(t)
	e.clock.Set("2026-10-30T12:00:00Z")
	res := must(e.runner.LoadCompanyResult(ctx, "NVDA"))(t)
	if res.Bars != 3 {
		t.Errorf("second load stored %d bars, want 3", res.Bars)
	}
	mu.Lock()
	defer mu.Unlock()
	if diff := cmp.Diff([]string{"2021-10-28/2026-10-26", "2026-10-27/2026-10-29"}, ranges); diff != "" {
		t.Errorf("requested ranges (-want +got):\n%s", diff)
	}
}
