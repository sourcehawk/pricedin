package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/sourcehawk/pricedin/server/internal/blob"
	"github.com/sourcehawk/pricedin/server/internal/config"
	"github.com/sourcehawk/pricedin/server/internal/ingest"
	"github.com/sourcehawk/pricedin/server/internal/prices/massive"
	"github.com/sourcehawk/pricedin/server/internal/sec"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

// minPoolConns leaves room for the advisory locks that the jobs hold on
// their own connections.
const minPoolConns = 8

func newJobsCmd(getenv func(string) string, stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "List or run the ingestion jobs",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return errors.New("jobs: want a subcommand: list or run")
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the jobs, with the latest run of each when the database is configured",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listJobs(cmd.Context(), getenv, cmd.OutOrStdout())
		},
	})
	var wait bool
	run := &cobra.Command{
		Use:   "run <job> [args]",
		Short: "Run one job and exit; see `pricedin jobs list`",
		Long: "run runs one ingestion job and prints its counts as JSON. When another process\n" +
			"runs the same job, it logs that and exits with success, or with --wait it waits\n" +
			"for that run to end and then runs the job.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			job, ok := ingest.LookupJob(args[0])
			if !ok {
				return fmt.Errorf("jobs run: unknown job %q; see pricedin jobs list", args[0])
			}
			return runJob(cmd.Context(), getenv, stdout, cmd.OutOrStdout(), job, args[1:], wait)
		},
	}
	run.Flags().BoolVar(&wait, "wait", false, "wait for a run of the same job in another process, instead of skipping")
	cmd.AddCommand(run)
	return cmd
}

func newIngestCmd(getenv func(string) string, stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Load data on demand",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return errors.New("ingest: want a subcommand: company")
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "company <ticker>",
		Short: "Load one company: SEC data, splits and dividends, daily bars and quarters",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			job, _ := ingest.LookupJob(ingest.JobCompany)
			return runJob(cmd.Context(), getenv, stdout, cmd.OutOrStdout(), job, args, true)
		},
	})
	return cmd
}

func newSchedulerCmd(getenv func(string) string, stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "scheduler",
		Short: "Run the daily job at PRICEDIN_SCHEDULE_DAILY (UTC) every day until SIGINT or SIGTERM",
		Long: "scheduler runs the daily job once a day. When it starts and the last successful\n" +
			"daily run is more than a day old, it runs the job at once. A job that another\n" +
			"process runs is skipped. On SIGINT or SIGTERM, the running job stops at its next\n" +
			"step and its run is recorded as failed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, cfg, logger, cleanup, err := buildRunner(cmd.Context(), getenv, stdout, true, true, false)
			if err != nil {
				return err
			}
			defer cleanup()
			s := &ingest.Scheduler{
				At: cfg.Ingest.DailyAt,
				Run: func(ctx context.Context) error {
					_, err := r.Daily(ctx)
					if errors.Is(err, ingest.ErrAlreadyRunning) {
						return nil
					}
					return err
				},
				LastSuccess: r.LastDailySuccess,
				Logger:      logger,
			}
			return s.Loop(cmd.Context())
		},
	}
}

// runJob builds a runner with the clients that job needs, runs it, and
// prints its result as JSON to out.
func runJob(ctx context.Context, getenv func(string) string, logOut, out io.Writer, job ingest.Job, args []string, wait bool) error {
	r, _, logger, cleanup, err := buildRunner(ctx, getenv, logOut, job.SEC, job.Prices, wait)
	if err != nil {
		return err
	}
	defer cleanup()
	res, err := r.Run(ctx, job, args)
	if errors.Is(err, ingest.ErrAlreadyRunning) {
		logger.InfoContext(ctx, "jobs run: another process runs the job; nothing to do", "job", job.Name)
		return nil
	}
	if res != nil {
		body, jerr := json.Marshal(res)
		if jerr == nil {
			_, _ = fmt.Fprintln(out, string(body))
		}
	}
	if err != nil {
		return fmt.Errorf("%s: %w", job.Name, err)
	}
	return nil
}

// buildRunner checks the configuration that the clients need before it
// connects to anything, and returns the runner and a cleanup that closes
// the database pool. With wait, the runner's jobs wait for a run of the
// same job elsewhere.
func buildRunner(ctx context.Context, getenv func(string) string, logOut io.Writer, withSEC, withPrices, wait bool) (*ingest.Runner, config.Config, *slog.Logger, func(), error) {
	cfg, err := config.Load(getenv)
	if err != nil {
		return nil, cfg, nil, nil, err
	}
	required := []error{cfg.Database.Require()}
	if withSEC {
		required = append(required, cfg.SEC.Require())
	}
	if withPrices {
		required = append(required, cfg.Massive.Require())
	}
	if err := errors.Join(required...); err != nil {
		return nil, cfg, nil, nil, err
	}
	logger := newLogger(cfg.Log, logOut)

	poolCfg, err := pgxpool.ParseConfig(cfg.Database.URL)
	if err != nil {
		// The parse error can repeat the URL, and with it the password.
		return nil, cfg, nil, nil, errors.New("the database URL does not parse")
	}
	poolCfg.MaxConns = max(poolCfg.MaxConns, minPoolConns)
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, cfg, nil, nil, fmt.Errorf("database pool: %w", err)
	}
	bs, err := blob.New(ctx, cfg.Blob)
	if err != nil {
		pool.Close()
		return nil, cfg, nil, nil, err
	}
	rec := ingest.NewRecorder(bs, time.Now)
	opts := ingest.Options{
		Pool: pool, Blob: bs, Universe: cfg.Ingest.Universe,
		PricesBackfillDays: cfg.Ingest.PricesBackfillDays, WaitForLocks: wait, Logger: logger,
	}
	if withSEC {
		// One client for the process: its rate limit holds per client.
		if opts.SEC, err = sec.New(sec.Options{
			UserAgent: cfg.SEC.UserAgent, BaseURL: cfg.SEC.BaseURL, DataBaseURL: cfg.SEC.DataBaseURL,
			Recorder: rec, Logger: logger,
		}); err != nil {
			pool.Close()
			return nil, cfg, nil, nil, err
		}
	}
	if withPrices {
		mc, err := massive.New(massive.Options{
			BaseURL: cfg.Massive.BaseURL, APIKey: cfg.Massive.APIKey, RateLimit: cfg.Massive.RateLimit,
			Recorder: rec, Logger: logger,
		})
		if err != nil {
			pool.Close()
			return nil, cfg, nil, nil, err
		}
		opts.Prices = mc
	}
	r, err := ingest.New(opts)
	if err != nil {
		pool.Close()
		return nil, cfg, nil, nil, err
	}
	return r, cfg, logger, pool.Close, nil
}

func listJobs(ctx context.Context, getenv func(string) string, out io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	latest := map[string]store.IngestRun{}
	if cfg.Database.URL != "" {
		pool, err := pgxpool.New(ctx, cfg.Database.URL)
		if err != nil {
			return errors.New("jobs list: the database URL does not parse")
		}
		defer pool.Close()
		runs, err := store.New(pool).ListLatestIngestRuns(ctx)
		if err != nil {
			return err
		}
		for _, run := range runs {
			latest[run.Job] = run
		}
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "JOB\tARGS\tLAST RUN\tSTATUS\tSUMMARY")
	for _, j := range ingest.Jobs() {
		last, status := "-", "-"
		if run, ok := latest[j.Name]; ok {
			last, status = run.StartedAt.UTC().Format(time.RFC3339), string(run.Status)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", j.Name, j.Args, last, status, j.Summary)
	}
	return tw.Flush()
}
