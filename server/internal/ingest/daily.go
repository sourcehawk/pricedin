package ingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/store"
)

// DailyResult holds the results of the steps of the daily job.
type DailyResult struct {
	SECTickers     SECTickersResult
	SECDaily       SECDailyResult
	PriceReference PriceReferenceResult
	PricesDaily    PricesResult
	Fundamentals   FundamentalsResult
	// Skipped counts the steps that another runner was running.
	Skipped int64
}

// Stats implements Result.
func (d DailyResult) Stats() map[string]int64 {
	return map[string]int64{
		"tickers_opened": d.SECTickers.Opened, "tickers_closed": d.SECTickers.Closed,
		"filers": d.SECDaily.Filers, "facts": d.SECDaily.Facts,
		"splits": d.PriceReference.Splits, "dividends": d.PriceReference.Dividends,
		"price_days": d.PricesDaily.Loaded, "bars": d.PricesDaily.Bars,
		"rebuilt": d.Fundamentals.Companies, "skipped": d.Skipped,
	}
}

// Daily runs the jobs that keep the data current, in this order, each as
// its own job with its own lock and ingest_runs row: sec-tickers, sec-daily,
// price-reference, prices-daily, and fundamentals-rebuild of the companies
// whose facts or provider splits changed (fundamentals-rebuild pending). A step that fails does not stop
// the later ones; Daily then returns the joined errors. A step that another
// runner is running is skipped.
func (r *Runner) Daily(ctx context.Context) (DailyResult, error) {
	if err := r.needSEC(JobDaily); err != nil {
		return DailyResult{}, err
	}
	if err := r.needPrices(JobDaily); err != nil {
		return DailyResult{}, err
	}
	return runJob(ctx, r, JobDaily, JobDaily, false, r.daily)
}

func (r *Runner) daily(ctx context.Context) (DailyResult, error) {
	var res DailyResult
	var errs []error
	step := func(name string, err error) {
		switch {
		case errors.Is(err, ErrAlreadyRunning):
			res.Skipped++
		case err != nil:
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	var err error
	res.SECTickers, err = r.SECTickers(ctx)
	step(JobSECTickers, err)
	if ctx.Err() == nil {
		res.SECDaily, err = r.SECDaily(ctx)
		step(JobSECDaily, err)
	}
	if ctx.Err() == nil {
		res.PriceReference, err = r.PriceReference(ctx)
		step(JobPriceReference, err)
	}
	if ctx.Err() == nil {
		res.PricesDaily, err = r.PricesDaily(ctx)
		step(JobPricesDaily, err)
	}
	if ctx.Err() == nil {
		res.Fundamentals, err = r.rebuildPending(ctx)
		step(JobFundamentalsRebuild, err)
	}
	if ctx.Err() != nil {
		errs = append(errs, ctx.Err())
	}
	return res, errors.Join(errs...)
}

// LastDailySuccess returns the end of the last successful daily run, for
// Scheduler.LastSuccess.
func (r *Runner) LastDailySuccess(ctx context.Context) (time.Time, bool, error) {
	run, err := r.store.GetLastSucceededIngestRun(ctx, JobDaily)
	if errors.Is(err, store.ErrNotFound) {
		return time.Time{}, false, nil
	}
	if err != nil || run.FinishedAt == nil {
		return time.Time{}, false, err
	}
	return *run.FinishedAt, true, nil
}
