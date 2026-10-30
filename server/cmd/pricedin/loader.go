package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sourcehawk/pricedin/server/internal/config"
	"github.com/sourcehawk/pricedin/server/internal/ingest"
	"github.com/sourcehawk/pricedin/server/internal/stocks"
)

// newCompanyLoader returns the loader of POST /api/v1/stocks/{ticker}/load
// and a function that closes it. Without the SEC, Massive and blob
// settings, or behind a database pooler in transaction mode, loading is
// off: it returns stocks.UnavailableLoader and logs why.
func newCompanyLoader(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, logger *slog.Logger) (stocks.CompanyLoader, func()) {
	if err := errors.Join(cfg.SEC.Require(), cfg.Massive.Require(), cfg.Blob.Require()); err != nil {
		logger.InfoContext(ctx, "loading a company on demand is off: the ingestion settings are missing", slog.Any("error", err))
		return stocks.UnavailableLoader{}, func() {}
	}
	return startLoader(ctx, func(ctx context.Context) (*ingest.Runner, error) {
		opts, err := ingestOptions(ctx, cfg, logger, pool, true, true)
		if err != nil {
			return nil, err
		}
		return ingest.New(ctx, opts)
	}, startupProbeTimeout, logger)
}

// startupProbeTimeout bounds the start of the runner before serve opens
// its listeners, so that a database that does not answer cannot hold up
// the health probes.
const startupProbeTimeout = 10 * time.Second

// startLoader tries to start the runner with build within timeout. When
// the database needs a session connection that it does not have, loading
// is off. On any other failure the first load tries again.
func startLoader(ctx context.Context, build func(context.Context) (*ingest.Runner, error), timeout time.Duration, logger *slog.Logger) (stocks.CompanyLoader, func()) {
	l := &runnerLoader{build: build}
	probe, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if _, err := l.runner(probe); errors.Is(err, ingest.ErrSessionRequired) {
		logger.ErrorContext(ctx, "loading a company on demand is off", slog.Any("error", err))
		return stocks.UnavailableLoader{}, func() {}
	} else if err != nil {
		// Such as a database that is not up yet: the first load tries again.
		logger.WarnContext(ctx, "the ingestion runner could not start; the first load tries again", slog.Any("error", err))
	}
	return l, l.close
}

// runnerLoader loads companies with an ingestion runner, which it starts
// on first use.
type runnerLoader struct {
	build func(context.Context) (*ingest.Runner, error)
	mu    sync.Mutex
	r     *ingest.Runner
}

func (l *runnerLoader) runner(ctx context.Context) (*ingest.Runner, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.r == nil {
		r, err := l.build(ctx)
		if err != nil {
			return nil, err
		}
		l.r = r
	}
	return l.r, nil
}

// LoadCompany loads the company of symbol with the runner.
func (l *runnerLoader) LoadCompany(ctx context.Context, symbol string) error {
	r, err := l.runner(ctx)
	if err != nil {
		return fmt.Errorf("start the ingestion runner: %w", err)
	}
	return loadError(r.LoadCompany(ctx, symbol))
}

func (l *runnerLoader) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.r != nil {
		l.r.Close()
	}
}

// loadError marks the runner's unknown symbol as the service's unknown
// ticker, which the API answers with 404.
func loadError(err error) error {
	if errors.Is(err, ingest.ErrUnknownSymbol) {
		return fmt.Errorf("%w: %w", stocks.ErrUnknownTicker, err)
	}
	return err
}
