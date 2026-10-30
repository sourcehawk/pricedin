package ingest

import (
	"context"
	"fmt"
)

// Job describes a job that `pricedin jobs run` can start.
type Job struct {
	Name string
	// Args is the usage of the arguments, such as "<ticker|cik>"; empty
	// when the job takes none.
	Args    string
	MinArgs int
	MaxArgs int
	Summary string
	// SEC and Prices say which clients the job calls.
	SEC, Prices bool
	run         func(ctx context.Context, r *Runner, args []string) (Result, error)
}

// Jobs returns every job, in the order of a first full import.
func Jobs() []Job {
	return []Job{
		{
			Name: JobSECTickers, SEC: true,
			Summary: "Load the SEC ticker map: new companies and the dated symbol map",
			run: func(ctx context.Context, r *Runner, _ []string) (Result, error) {
				return r.SECTickers(ctx)
			},
		},
		{
			Name:    JobUniverse,
			Summary: "List the covered universe: current NYSE and Nasdaq tickers, narrowed by PRICEDIN_UNIVERSE (reads only)",
			run: func(ctx context.Context, r *Runner, _ []string) (Result, error) {
				return r.Universe(ctx)
			},
		},
		{
			Name: JobSECBulk, Args: "[auto|archive|api]", MaxArgs: 1, SEC: true,
			Summary: "Load the submissions and company facts of every covered company: the universe and the companies loaded on demand",
			run: func(ctx context.Context, r *Runner, args []string) (Result, error) {
				return r.SECBulk(ctx, argOr(args, BulkAuto))
			},
		},
		{
			Name: JobSECCompany, Args: "<ticker|cik>", MinArgs: 1, MaxArgs: 1, SEC: true,
			Summary: "Load the submissions and company facts of one company",
			run: func(ctx context.Context, r *Runner, args []string) (Result, error) {
				return r.SECCompany(ctx, args[0])
			},
		},
		{
			Name: JobSECDaily, SEC: true,
			Summary: "Read the daily filing indexes since the last run, load the covered companies that filed, and retry earlier failures",
			run: func(ctx context.Context, r *Runner, _ []string) (Result, error) {
				return r.SECDaily(ctx)
			},
		},
		{
			Name: JobPriceReference, Prices: true,
			Summary: "Load the provider's ticker map, and the splits and dividends of every covered symbol",
			run: func(ctx context.Context, r *Runner, _ []string) (Result, error) {
				return r.PriceReference(ctx)
			},
		},
		{
			Name: JobPricesBackfill, Prices: true,
			Summary: "Load the grouped daily bars of every missing trading day, up to PRICEDIN_PRICES_BACKFILL_DAYS back",
			run: func(ctx context.Context, r *Runner, _ []string) (Result, error) {
				return r.PricesBackfill(ctx)
			},
		},
		{
			Name: JobPricesDaily, Prices: true,
			Summary: "Load the grouped daily bars of the trading days since the newest loaded one",
			run: func(ctx context.Context, r *Runner, _ []string) (Result, error) {
				return r.PricesDaily(ctx)
			},
		},
		{
			Name: JobFundamentalsRebuild, Args: "<ticker|cik|all|pending>", MinArgs: 1, MaxArgs: 1,
			Summary: "Derive the quarters again from the stored facts and provider splits; pending: the companies whose inputs changed",
			run: func(ctx context.Context, r *Runner, args []string) (Result, error) {
				return r.FundamentalsRebuild(ctx, args[0])
			},
		},
		{
			Name: JobCompany, Args: "<ticker>", MinArgs: 1, MaxArgs: 1, SEC: true, Prices: true,
			Summary: "Load one company on demand: SEC data, splits and dividends, daily bars, quarters",
			run: func(ctx context.Context, r *Runner, args []string) (Result, error) {
				return r.LoadCompanyResult(ctx, args[0])
			},
		},
		{
			Name: JobDaily, SEC: true, Prices: true,
			Summary: "Run sec-tickers, sec-daily, price-reference, prices-daily and fundamentals-rebuild pending",
			run: func(ctx context.Context, r *Runner, _ []string) (Result, error) {
				return r.Daily(ctx)
			},
		},
	}
}

// LookupJob returns the job with the name.
func LookupJob(name string) (Job, bool) {
	for _, j := range Jobs() {
		if j.Name == name {
			return j, true
		}
	}
	return Job{}, false
}

// Run runs the job with args. It returns an error when the number of
// arguments does not fit the job.
func (r *Runner) Run(ctx context.Context, job Job, args []string) (Result, error) {
	if len(args) < job.MinArgs || len(args) > job.MaxArgs {
		usage := job.Name
		if job.Args != "" {
			usage += " " + job.Args
		}
		return nil, fmt.Errorf("ingest: usage: %s", usage)
	}
	return job.run(ctx, r, args)
}

func argOr(args []string, fallback string) string {
	if len(args) > 0 {
		return args[0]
	}
	return fallback
}
