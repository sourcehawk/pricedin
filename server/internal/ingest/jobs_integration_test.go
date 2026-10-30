//go:build integration

package ingest_test

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/ingest"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

// A job is safe to run twice: the second run, on the same day and with the
// same upstream data, leaves the database and the blob store as the first
// run left them.
func TestEveryJobRunTwiceChangesNothingTheSecondTime(t *testing.T) {
	t.Parallel()
	secTickers := func(ctx context.Context, r *ingest.Runner) error { _, err := r.SECTickers(ctx); return err }
	tests := []struct {
		name  string
		now   string
		setup []func(context.Context, *ingest.Runner) error
		job   func(context.Context, *ingest.Runner) error
	}{
		{name: ingest.JobSECTickers, job: secTickers},
		{
			name: ingest.JobSECCompany, setup: []func(context.Context, *ingest.Runner) error{secTickers},
			job: func(ctx context.Context, r *ingest.Runner) error { _, err := r.SECCompany(ctx, "NVDA"); return err },
		},
		{
			name: ingest.JobSECBulk + " archive", setup: []func(context.Context, *ingest.Runner) error{secTickers},
			job: func(ctx context.Context, r *ingest.Runner) error {
				_, err := r.SECBulk(ctx, ingest.BulkArchive)
				return err
			},
		},
		{
			name: ingest.JobSECBulk + " api", setup: []func(context.Context, *ingest.Runner) error{secTickers},
			job: func(ctx context.Context, r *ingest.Runner) error {
				_, err := r.SECBulk(ctx, ingest.BulkAPI)
				return err
			},
		},
		{
			// The daily index fixture is of 2026-08-26.
			name: ingest.JobSECDaily, now: "2026-08-27T12:00:00Z", setup: []func(context.Context, *ingest.Runner) error{secTickers},
			job: func(ctx context.Context, r *ingest.Runner) error { _, err := r.SECDaily(ctx); return err },
		},
		{
			name: ingest.JobPriceReference, setup: []func(context.Context, *ingest.Runner) error{secTickers},
			job: func(ctx context.Context, r *ingest.Runner) error { _, err := r.PriceReference(ctx); return err },
		},
		{
			name: ingest.JobPricesBackfill, setup: []func(context.Context, *ingest.Runner) error{secTickers},
			job: func(ctx context.Context, r *ingest.Runner) error { _, err := r.PricesBackfill(ctx); return err },
		},
		{
			name: ingest.JobPricesDaily, setup: []func(context.Context, *ingest.Runner) error{secTickers},
			job: func(ctx context.Context, r *ingest.Runner) error { _, err := r.PricesDaily(ctx); return err },
		},
		{
			name: ingest.JobFundamentalsRebuild,
			setup: []func(context.Context, *ingest.Runner) error{
				secTickers,
				func(ctx context.Context, r *ingest.Runner) error {
					_, err := r.SECBulk(ctx, ingest.BulkAPI)
					return err
				},
				func(ctx context.Context, r *ingest.Runner) error { _, err := r.PriceReference(ctx); return err },
			},
			job: func(ctx context.Context, r *ingest.Runner) error {
				_, err := r.FundamentalsRebuild(ctx, "all")
				return err
			},
		},
		{
			name: ingest.JobCompany,
			job:  func(ctx context.Context, r *ingest.Runner) error { return r.LoadCompany(ctx, "NVDA") },
		},
		{
			name: ingest.JobDaily, setup: []func(context.Context, *ingest.Runner) error{secTickers},
			job: func(ctx context.Context, r *ingest.Runner) error { _, err := r.Daily(ctx); return err },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			e := newEnv(t, envOptions{now: tt.now, backfillDays: 15})
			for _, setup := range tt.setup {
				if err := setup(ctx, e.runner); err != nil {
					t.Fatalf("setup: %v", err)
				}
			}
			before := e.snapshot(t)
			if err := tt.job(ctx, e.runner); err != nil {
				t.Fatalf("first run: %v", err)
			}
			first := e.snapshot(t)
			if cmp.Equal(before, first) {
				t.Fatal("the first run changed nothing; the test proves nothing")
			}
			if err := tt.job(ctx, e.runner); err != nil {
				t.Fatalf("second run: %v", err)
			}
			if diff := cmp.Diff(first, e.snapshot(t)); diff != "" {
				t.Errorf("the second run changed the data (-first +second):\n%s", diff)
			}
			if n := e.count(t, `SELECT count(*) FROM ingest_runs WHERE status <> 'succeeded'`); n != 0 {
				t.Errorf("%d runs did not succeed", n)
			}
		})
	}
}

func TestSECTickersLoadsTheMapAndTheUniverseFollowsIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, envOptions{})
	res := must(e.runner.SECTickers(ctx))(t)
	if res.Tickers != 13 || res.CompaniesAdded != 12 || res.Opened != 13 {
		t.Errorf("SECTickers() = %+v, want 13 tickers of 12 companies opened", res)
	}
	u := must(e.runner.Universe(ctx))(t)
	got := make([]string, 0, len(u.Members))
	for _, m := range u.Members {
		got = append(got, m.Symbol)
	}
	if diff := cmp.Diff([]string{"AAPL", "ADM", "GRMN", "KO", "NVDA"}, got); diff != "" {
		t.Errorf("Universe() (-want +got):\n%s", diff)
	}
}

func TestJobsNeedTheUniverse(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	if _, err := e.runner.PricesDaily(context.Background()); !errors.Is(err, ingest.ErrEmptyUniverse) {
		t.Errorf("PricesDaily() before sec-tickers: error = %v, want ErrEmptyUniverse", err)
	}
}

func TestSECBulkReadsTheArchiveFirst(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, envOptions{})
	must(e.runner.SECTickers(ctx))(t)
	res := must(e.runner.SECBulk(ctx, ingest.BulkArchive))(t)
	// The archive fixture holds KO and GRMN; the others come from the API.
	if res.ArchiveCompanies != 2 || res.Companies != 5 || res.Facts == 0 || res.ArchiveBytes == 0 {
		t.Errorf("SECBulk() = %+v, want 2 companies from the archive and 5 loaded", res)
	}
	keys := must(e.blob.List(ctx, "sec/archives/"))(t)
	if len(keys) != 1 || !strings.HasSuffix(keys[0], "-companyfacts.zip") {
		t.Errorf("archive keys = %v, want one companyfacts.zip", keys)
	}
	for _, cik := range []domain.CIK{21344, 1121788, 1045810} {
		if n := e.count(t, `SELECT count(*) FROM facts WHERE cik = $1`, int64(cik)); n == 0 {
			t.Errorf("no facts for %s", cik)
		}
	}
	c := must(e.store.GetCompany(ctx, 1045810))(t)
	if c.SIC == "" || c.FiscalYearEnd == "" {
		t.Errorf("NVDA = %+v, want the details of its submissions", c)
	}
}

func TestSECTickersKeepsTheDetailsFromSubmissions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, envOptions{})
	must(e.runner.SECTickers(ctx))(t)
	must(e.runner.SECCompany(ctx, "1045810"))(t)
	want := must(e.store.GetCompany(ctx, 1045810))(t)
	must(e.runner.SECTickers(ctx))(t)
	if got := must(e.store.GetCompany(ctx, 1045810))(t); got != want {
		t.Errorf("after sec-tickers: %+v, want %+v", got, want)
	}
}

func TestSECDailyLoadsTheUniverseCompaniesThatFiled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, envOptions{now: "2026-08-27T12:00:00Z"})
	must(e.runner.SECTickers(ctx))(t)
	res := must(e.runner.SECDaily(ctx))(t)
	// NVDA filed a 10-Q on 2026-08-26; the index has no other covered
	// company with a periodic report.
	if res.Indexes != 1 || res.Filers != 1 || res.Companies != 1 || res.Facts == 0 {
		t.Errorf("SECDaily() = %+v, want one index and NVDA loaded", res)
	}
	if diff := cmp.Diff([]domain.CIK{1045810}, must(e.store.ListPendingFundamentals(ctx))(t)); diff != "" {
		t.Errorf("pending rebuilds (-want +got):\n%s", diff)
	}
	cursor := must(e.store.GetIngestCursor(ctx, ingest.JobSECDaily))(t)
	if cursor != domain.NewDate(2026, 8, 26) {
		t.Errorf("cursor = %s, want the last index read, 2026-08-26", cursor)
	}
}

func TestPricesDailyFillsAGapOfThreeTradingDaysInOneRun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// The 2026-10-26 session is the last finished one.
	e := newEnv(t, envOptions{now: "2026-10-27T12:00:00Z"})
	must(e.runner.SECTickers(ctx))(t)
	must(e.runner.PricesDaily(ctx))(t)

	e.clock.Set("2026-10-30T12:00:00Z")
	res := must(e.runner.PricesDaily(ctx))(t)
	if res.Days != 3 || res.Loaded != 3 || res.Bars != 15 {
		t.Errorf("PricesDaily() = %+v, want 3 days with 5 bars each", res)
	}
	for _, d := range []string{"2026-10-27", "2026-10-28", "2026-10-29"} {
		if n := e.count(t, `SELECT count(*) FROM prices_daily WHERE date = $1`, d); n != 5 {
			t.Errorf("%s: %d bars, want one for each of the 5 universe symbols", d, n)
		}
	}
}

func TestPricesBackfillLoadsTheMissingDaysNewestFirstWithinTheBound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var mu sync.Mutex
	var requested []string
	e := newEnv(t, envOptions{backfillDays: 4, wrap: func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if day, ok := strings.CutPrefix(r.URL.Path, "/api.massive.com/v2/aggs/grouped/locale/us/market/stocks/"); ok {
				mu.Lock()
				requested = append(requested, day)
				mu.Unlock()
			}
			h.ServeHTTP(w, r)
		})
	}})
	must(e.runner.SECTickers(ctx))(t)
	res := must(e.runner.PricesBackfill(ctx))(t)
	if res.Loaded != 4 {
		t.Errorf("PricesBackfill() = %+v, want 4 days", res)
	}
	want := []string{"2026-10-29", "2026-10-28", "2026-10-27", "2026-10-26"}
	if diff := cmp.Diff(want, requested); diff != "" {
		t.Errorf("requested days (-want +got):\n%s", diff)
	}
}

func TestOnlyOneRunnerRunsAJobAtATime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	started := make(chan struct{})
	proceed := make(chan struct{})
	var once sync.Once
	e := newEnv(t, envOptions{wrap: func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/v2/aggs/grouped/") {
				once.Do(func() { close(started) })
				<-proceed
			}
			h.ServeHTTP(w, r)
		})
	}})
	must(e.runner.SECTickers(ctx))(t)
	// A second process: its own pool and its own clients.
	other := e.newRunner(t, newPool(t, e.url), 0, nil)

	done := make(chan error, 1)
	go func() {
		_, err := e.runner.PricesDaily(ctx)
		done <- err
	}()
	<-started
	_, err := other.PricesDaily(ctx)
	close(proceed)
	if !errors.Is(err, ingest.ErrAlreadyRunning) {
		t.Errorf("second runner: error = %v, want ErrAlreadyRunning", err)
	}
	if err := <-done; err != nil {
		t.Errorf("first runner: %v", err)
	}
	if n := e.count(t, `SELECT count(*) FROM ingest_runs WHERE job = $1`, ingest.JobPricesDaily); n != 1 {
		t.Errorf("%d prices-daily runs recorded, want 1", n)
	}
	// The lock is free again.
	if _, err := other.PricesDaily(ctx); err != nil {
		t.Errorf("a later run: %v", err)
	}
}

func TestLoadCompanyLoadsNVDAWithItsSplitApplied(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, envOptions{})
	res := must(e.runner.LoadCompanyResult(ctx, "nvda"))(t)
	if res.CIK != 1045810 || res.SEC.Facts == 0 || res.Bars < 1200 || res.Splits != 2 || res.Fundamentals.Quarters < 12 {
		t.Errorf("LoadCompanyResult() = %+v, want NVDA's facts, five years of bars, both splits and its quarters", res)
	}

	values := must(e.store.ListQuarterlyFundamentals(ctx, 1045810))(t)
	var eps []store.QuarterlyValue
	for _, v := range values {
		if v.Metric == "eps_diluted" {
			eps = append(eps, v)
		}
	}
	if len(eps) < 12 {
		t.Fatalf("%d quarters of diluted EPS, want at least 12", len(eps))
	}
	last := eps[len(eps)-1]
	if last.Label == "" || last.FirstFiled.IsZero() || len(last.Inputs) == 0 || last.Inputs[0].PeriodEnd.IsZero() {
		t.Errorf("latest EPS %+v, want a label, a first filing date and inputs with periods", last)
	}
	// The conflicts that the provider's splits explain are gone.
	for _, c := range must(e.store.ListFundamentalConflicts(ctx, 1045810))(t) {
		if c.Kind == "split_evidence" {
			t.Errorf("conflict %+v: the provider's splits match the SEC splits", c)
		}
	}

	bars := must(e.store.ListAdjustedPriceBars(ctx, "NVDA", domain.NewDate(2024, 6, 6), domain.NewDate(2024, 6, 11)))(t)
	if len(bars) != 4 {
		t.Fatalf("%d bars around the split, want 4", len(bars))
	}
	for _, b := range bars {
		wantFactor := 1.0
		if b.Date.Before(domain.NewDate(2024, 6, 10)) {
			wantFactor = 10
		}
		if b.SplitFactor != wantFactor || math.Abs(b.Close*wantFactor-b.RawClose) > 1e-6 {
			t.Errorf("%s: factor %v, close %v, raw %v; want factor %v", b.Date, b.SplitFactor, b.Close, b.RawClose, wantFactor)
		}
	}
	if n := e.count(t, `SELECT count(*) FROM ingest_runs WHERE job = 'company' AND status = 'succeeded'`); n != 1 {
		t.Errorf("%d succeeded company runs, want 1", n)
	}
}

func TestLoadCompanyOfAnUnknownSymbol(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{})
	err := e.runner.LoadCompany(context.Background(), "ZZZZ")
	if !errors.Is(err, ingest.ErrUnknownSymbol) {
		t.Errorf("LoadCompany(ZZZZ) error = %v, want ErrUnknownSymbol", err)
	}
	// It loaded the SEC map once to look for the symbol.
	if n := e.count(t, `SELECT count(*) FROM ingest_runs WHERE job = 'sec-tickers'`); n != 1 {
		t.Errorf("%d sec-tickers runs, want 1", n)
	}
	if err := e.runner.LoadCompany(context.Background(), "ZZZZ"); !errors.Is(err, ingest.ErrUnknownSymbol) {
		t.Errorf("second LoadCompany(ZZZZ) error = %v, want ErrUnknownSymbol", err)
	}
	if n := e.count(t, `SELECT count(*) FROM ingest_runs WHERE job = 'sec-tickers'`); n != 1 {
		t.Errorf("%d sec-tickers runs after a second unknown symbol, want still 1 on the same day", n)
	}
}

func TestPriceReferenceMapsProviderTickersToStoredCompanies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, envOptions{})
	must(e.runner.SECTickers(ctx))(t)
	res := must(e.runner.PriceReference(ctx))(t)
	if res.ProviderTickers != 5 || res.Matched != 5 || res.Symbols != 5 || res.Dividends == 0 {
		t.Errorf("PriceReference() = %+v, want 5 provider tickers matched and 5 symbols loaded", res)
	}
	records := must(e.store.ListTickers(ctx, 7084))(t)
	sources := map[string]string{}
	for _, r := range records {
		sources[r.Source] = r.Exchange
	}
	if diff := cmp.Diff(map[string]string{"sec": "NYSE", "massive": "XNYS"}, sources); diff != "" {
		t.Errorf("ADM ticker rows (-want +got):\n%s", diff)
	}
}

func TestDailyRunsEveryStep(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, envOptions{now: "2026-08-27T12:00:00Z"})
	res := must(e.runner.Daily(ctx))(t)
	if res.SECTickers.Tickers == 0 || res.SECDaily.Companies != 1 || res.PriceReference.Symbols != 5 ||
		res.PricesDaily.Loaded == 0 || res.Fundamentals.Companies == 0 {
		t.Errorf("Daily() = %+v, want every step to do work", res)
	}
	last, ok, err := e.runner.LastDailySuccess(ctx)
	if err != nil || !ok || last.IsZero() {
		t.Errorf("LastDailySuccess() = %v, %v, %v; want the run just finished", last, ok, err)
	}
	for _, job := range []string{"daily", "sec-tickers", "sec-daily", "price-reference", "prices-daily", "fundamentals-rebuild"} {
		if n := e.count(t, `SELECT count(*) FROM ingest_runs WHERE job = $1 AND status = 'succeeded'`, job); n != 1 {
			t.Errorf("%s: %d succeeded runs, want 1", job, n)
		}
	}
}
