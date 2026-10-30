package ingest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

// ErrEmptyUniverse means that the daily jobs have no company to keep
// current: the covered universe is empty, because sec-tickers has not run
// yet or PRICEDIN_UNIVERSE names no covered symbol, and no company loaded
// on demand has a current SEC ticker.
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
	// Pool is the database. The Runner opens a second, small pool for its
	// advisory locks, so that held locks never take the connections that
	// the jobs' queries need; Close closes it.
	Pool *pgxpool.Pool
	// LockURL is the connection of the lock pool
	// (PRICEDIN_DATABASE_LOCK_URL). Empty means the settings of Pool. It
	// must keep one session per connection: New fails with
	// ErrSessionRequired behind a pooler in transaction mode.
	LockURL string
	// Blob keeps the bulk archive. The clients record the other responses
	// themselves, with a Recorder over the same store.
	Blob blob.Store
	// SEC and Prices are the clients. A job that needs a nil one fails.
	// The process must share one SEC client, because its rate limit holds
	// per client.
	SEC    *sec.Client
	Prices prices.Provider
	// StoredPrices reads the price provider's responses in Blob for
	// reprocess. A reprocess of price files fails without it.
	StoredPrices prices.StoredReader
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
// ErrAlreadyRunning and records nothing, unless Options.WaitForLocks is
// set; LoadCompany always waits for the other load of the same company.
// Every SEC request also takes the shared lock "sec-api", one company or
// one file at a time, and a process that takes it over from another one
// starts no sooner than a second after that one released it, so that all
// processes together stay within the SEC rate limit. At most maxJobs
// top-level jobs run at once in one Runner; more wait for their turn.
type Runner struct {
	store        *store.Store
	locks        locker
	lockPool     *pgxpool.Pool
	slots        chan struct{}
	blob         blob.Store
	sec          *sec.Client
	prices       prices.Provider
	stored       prices.StoredReader
	narrow       []string
	backfillDays int
	waitForLocks bool
	now          func() time.Time
	logger       *slog.Logger
}

// New returns a Runner. Pool and Blob are required. It connects to the
// lock database, and returns an error that wraps ErrSessionRequired when
// its check finds that the connection does not keep its session. The
// caller closes the Runner with Close.
func New(ctx context.Context, opts Options) (*Runner, error) {
	if opts.Pool == nil || opts.Blob == nil {
		return nil, errors.New("ingest: a database pool and a blob store are required")
	}
	lockCfg := opts.Pool.Config().Copy()
	if opts.LockURL != "" {
		var err error
		if lockCfg, err = pgxpool.ParseConfig(opts.LockURL); err != nil {
			// The parse error can repeat the URL, and with it the password.
			return nil, errors.New("ingest: the lock database URL does not parse")
		}
	}
	lockCfg.MaxConns, lockCfg.MinConns = maxJobs*locksPerJob, 0
	lockPool, err := pgxpool.NewWithConfig(ctx, lockCfg)
	if err != nil {
		return nil, fmt.Errorf("ingest: lock pool: %w", err)
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	holder := make([]byte, 8)
	_, _ = rand.Read(holder)
	locks := locker{pool: lockPool, holder: hex.EncodeToString(holder), logger: logger}
	if err := locks.checkSession(ctx); err != nil {
		lockPool.Close()
		return nil, err
	}
	r := &Runner{
		store:        store.New(opts.Pool),
		locks:        locks,
		lockPool:     lockPool,
		slots:        make(chan struct{}, maxJobs),
		blob:         opts.Blob,
		sec:          opts.SEC,
		prices:       opts.Prices,
		stored:       opts.StoredPrices,
		narrow:       opts.Universe,
		backfillDays: opts.PricesBackfillDays,
		waitForLocks: opts.WaitForLocks,
		now:          opts.Now,
		logger:       logger,
	}
	if r.backfillDays <= 0 {
		r.backfillDays = defaultBackfillDays
	}
	if r.now == nil {
		r.now = time.Now
	}
	return r, nil
}

// Close closes the pool of the advisory locks. Call it after the last job
// ended.
func (r *Runner) Close() {
	r.lockPool.Close()
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

// maxJobs is the most jobs that one Runner runs at once, and locksPerJob
// the most advisory locks that one of them holds together: its own, a
// step's, and the SEC lock. Together they size the lock pool, so a holder
// always gets the connection for its next lock.
const (
	maxJobs     = 4
	locksPerJob = 3
)

// inJobKey marks the context of a running job, whose steps run as jobs of
// their own without a slot of their own.
type inJobKey struct{}

// runJob runs fn as job: it takes the advisory lock lockName (waiting for
// it when wait or Options.WaitForLocks is set, else returning
// ErrAlreadyRunning), records an
// ingest_runs row, and finishes the row with the result's counts and the
// error. A job that no other job started waits for one of the Runner's
// slots first.
func runJob[T Result](ctx context.Context, r *Runner, job, lockName string, wait bool, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	if ctx.Value(inJobKey{}) == nil {
		select {
		case r.slots <- struct{}{}:
		case <-ctx.Done():
			return zero, ctx.Err()
		}
		defer func() { <-r.slots }()
		ctx = context.WithValue(ctx, inJobKey{}, true)
	}
	var lk *lock
	var err error
	if wait || r.waitForLocks {
		lk, err = r.locks.wait(ctx, lockName)
	} else {
		lk, err = r.locks.try(ctx, lockName)
	}
	if err != nil {
		return zero, err
	}
	if lk == nil {
		r.logger.WarnContext(ctx, "ingest: job is already running elsewhere; skipping", "job", job, "lock", lockName)
		return zero, fmt.Errorf("%s: %w", job, ErrAlreadyRunning)
	}
	defer r.locks.release(ctx, lk)

	id, abandoned, err := r.store.StartIngestRunForLock(ctx, job, lockName)
	if err != nil {
		return zero, err
	}
	if abandoned > 0 {
		r.logger.WarnContext(ctx, "ingest: marked runs of a process that died as failed", "job", job, "lock", lockName, "runs", abandoned)
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
