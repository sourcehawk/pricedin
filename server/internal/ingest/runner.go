package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sourcehawk/pricedin/server/internal/blob"
	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/prices"
	"github.com/sourcehawk/pricedin/server/internal/sec"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

// ErrAlreadyRunning means that another runner, in this process or another
// one, runs the same job now. The job did not run, and nothing changed.
var ErrAlreadyRunning = errors.New("ingest: the job is already running")

// ErrUnknownSymbol means that neither the stored ticker map nor a fresh
// copy of the SEC ticker map knows the symbol.
var ErrUnknownSymbol = errors.New("ingest: unknown symbol")

// ErrEmptyUniverse means that the covered universe has no company, because
// sec-tickers has not run yet or PRICEDIN_UNIVERSE names no covered
// symbol.
var ErrEmptyUniverse = errors.New("ingest: the covered universe is empty; run sec-tickers first, and check PRICEDIN_UNIVERSE")

// The time limits of one external call. The clients have no limit on a
// whole call of their own.
const (
	secCallTimeout      = 2 * time.Minute
	archiveTimeout      = 3 * time.Hour
	pricesCallTimeout   = 2 * time.Minute
	tickerListTimeout   = 10 * time.Minute
	defaultBackfillDays = 1260
)

// Options configure a Runner.
type Options struct {
	// Pool is the database. The Runner takes its advisory locks on it, so
	// it needs a few connections beyond the store's: about four for the
	// daily job.
	Pool *pgxpool.Pool
	// Blob keeps the bulk archives. The clients record the other responses
	// themselves, with a Recorder over the same store.
	Blob blob.Store
	// SEC and Prices are the clients. A job that needs a nil one fails.
	// The process must share one SEC client, because its rate limit holds
	// per client.
	SEC    *sec.Client
	Prices prices.Provider
	// Universe narrows the covered universe to these symbols when not
	// empty (PRICEDIN_UNIVERSE).
	Universe []string
	// PricesBackfillDays bounds prices-backfill. Default 1260.
	PricesBackfillDays int
	// WaitForLocks makes a job that another runner is running wait for it
	// and then run, instead of returning ErrAlreadyRunning.
	WaitForLocks bool
	// Now is the clock. It must agree with the clock of Prices, which
	// decides the last finished session. Default time.Now.
	Now    func() time.Time
	Logger *slog.Logger
}

// Runner runs the ingestion jobs. It is safe for concurrent use.
//
// Every job takes a Postgres advisory lock for its name and records one
// ingest_runs row. When another runner holds the lock, a job returns
// ErrAlreadyRunning at once and records nothing, unless
// Options.WaitForLocks is set; LoadCompany always waits for the other load
// of the same company. Every SEC request also
// takes the shared lock "sec-api", one company or one file at a time, so
// that all processes together stay within the SEC rate limit.
type Runner struct {
	store        *store.Store
	locks        locker
	blob         blob.Store
	sec          *sec.Client
	prices       prices.Provider
	narrow       []string
	backfillDays int
	waitForLocks bool
	now          func() time.Time
	logger       *slog.Logger
}

// New returns a Runner. Pool and Blob are required.
func New(opts Options) (*Runner, error) {
	if opts.Pool == nil || opts.Blob == nil {
		return nil, errors.New("ingest: a database pool and a blob store are required")
	}
	r := &Runner{
		store:        store.New(opts.Pool),
		locks:        locker{pool: opts.Pool},
		blob:         opts.Blob,
		sec:          opts.SEC,
		prices:       opts.Prices,
		narrow:       opts.Universe,
		backfillDays: opts.PricesBackfillDays,
		waitForLocks: opts.WaitForLocks,
		now:          opts.Now,
		logger:       opts.Logger,
	}
	if r.backfillDays <= 0 {
		r.backfillDays = defaultBackfillDays
	}
	if r.now == nil {
		r.now = time.Now
	}
	if r.logger == nil {
		r.logger = slog.New(slog.DiscardHandler)
	}
	return r, nil
}

// today is the current date in UTC.
func (r *Runner) today() domain.Date {
	return domain.DateOf(r.now().UTC())
}

func (r *Runner) needSEC(job string) error {
	if r.sec == nil {
		return fmt.Errorf("ingest: %s needs the SEC client", job)
	}
	return nil
}

func (r *Runner) needPrices(job string) error {
	if r.prices == nil {
		return fmt.Errorf("ingest: %s needs the price provider", job)
	}
	return nil
}

// Result is the outcome of a job: the counts that its ingest_runs row
// keeps.
type Result interface {
	Stats() map[string]int64
}

// runJob runs fn as job: it takes the advisory lock lockName (waiting for
// it when wait is true, else returning ErrAlreadyRunning), records an
// ingest_runs row, and finishes the row with the result's counts and the
// error.
func runJob[T Result](ctx context.Context, r *Runner, job, lockName string, wait bool, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	var release func()
	if wait || r.waitForLocks {
		var err error
		if release, err = r.locks.wait(ctx, lockName); err != nil {
			return zero, err
		}
	} else {
		var ok bool
		var err error
		release, ok, err = r.locks.try(ctx, lockName)
		if err != nil {
			return zero, err
		}
		if !ok {
			r.logger.InfoContext(ctx, "ingest: job is already running elsewhere; skipping", "job", job, "lock", lockName)
			return zero, fmt.Errorf("%s: %w", job, ErrAlreadyRunning)
		}
	}
	defer release()

	id, err := r.store.StartIngestRun(ctx, job)
	if err != nil {
		return zero, err
	}
	start := time.Now()
	r.logger.InfoContext(ctx, "ingest: job started", "job", job, "run", id)
	res, runErr := fn(ctx)

	status, errText := store.RunSucceeded, ""
	if runErr != nil {
		status, errText = store.RunFailed, runErr.Error()
	}
	stats := res.Stats()
	// The row is finished also when ctx ended, such as on a shutdown.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
	defer cancel()
	if err := r.store.FinishIngestRun(finishCtx, id, status, stats, errText); err != nil {
		return res, errors.Join(runErr, err)
	}
	attrs := []any{"job", job, "run", id, "status", status, "duration", time.Since(start).Round(time.Millisecond), "stats", stats}
	if runErr != nil {
		r.logger.ErrorContext(ctx, "ingest: job failed", append(attrs, "error", runErr.Error())...)
	} else {
		r.logger.InfoContext(ctx, "ingest: job finished", attrs...)
	}
	return res, runErr
}

// failures collects the errors of the companies, days or symbols that a
// job could not load, and keeps the first few for the error text.
type failures struct {
	n     int64
	first []error
}

const keptFailures = 5

func (f *failures) add(what string, err error) {
	f.n++
	if len(f.first) < keptFailures {
		f.first = append(f.first, fmt.Errorf("%s: %w", what, err))
	}
}

func (f *failures) err(of int) error {
	if f.n == 0 {
		return nil
	}
	return fmt.Errorf("%d of %d failed: %w", f.n, of, errors.Join(f.first...))
}

// fatal reports whether err must stop a job that works through many items:
// the job's context ended, or SEC refused the client, which every later
// request would repeat.
func fatal(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, sec.ErrForbidden)
}

// resolveCIK reads ref as a CIK when it is a number, and as a symbol
// otherwise.
func (r *Runner) resolveCIK(ctx context.Context, ref string) (domain.CIK, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return 0, errors.New("ingest: want a ticker or a CIK")
	}
	if strings.Trim(ref, "0123456789") == "" {
		return domain.ParseCIK(ref)
	}
	t, err := r.store.GetCurrentTicker(ctx, ref)
	if errors.Is(err, store.ErrNotFound) {
		return 0, fmt.Errorf("%q: %w", ref, ErrUnknownSymbol)
	}
	if err != nil {
		return 0, err
	}
	return t.CIK, nil
}
