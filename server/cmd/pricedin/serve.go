package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"github.com/sourcehawk/pricedin/server/internal/auth"
	"github.com/sourcehawk/pricedin/server/internal/config"
	"github.com/sourcehawk/pricedin/server/internal/health"
	"github.com/sourcehawk/pricedin/server/internal/httpapi"
	"github.com/sourcehawk/pricedin/server/internal/httpserver"
	"github.com/sourcehawk/pricedin/server/internal/ingest"
	"github.com/sourcehawk/pricedin/server/internal/stocks"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

const readinessTimeout = 2 * time.Second

func newServeCmd(getenv func(string) string, stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Serve the HTTP API and the metrics endpoint until SIGINT or SIGTERM",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return serve(cmd.Context(), getenv, stdout)
		},
	}
}

func serve(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	if err := cfg.Database.Require(); err != nil {
		return err
	}
	logger := newLogger(cfg.Log, stdout)

	// The pool connects on first use, so the server starts while the
	// database is down and /readyz reports it.
	poolCfg, err := pgxpool.ParseConfig(cfg.Database.URL)
	if err != nil {
		// The parse error can repeat the URL, and with it the password.
		return errors.New("serve: the database URL does not parse")
	}
	// Room for the jobs of an on-demand load beside the requests.
	poolCfg.MaxConns = max(poolCfg.MaxConns, minPoolConns)
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("serve: database pool: %w", err)
	}
	defer pool.Close()

	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	st := store.New(pool)
	authSvc, err := auth.New(st, auth.Options{
		OwnerEmail: cfg.Auth.OwnerEmail, OwnerPasswordHash: cfg.Auth.OwnerPasswordHash, SessionTTL: cfg.Auth.SessionTTL,
	})
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	if !authSvc.LoginEnabled() {
		logger.WarnContext(ctx, "no owner configured: login is disabled and only API tokens work")
	}
	if len(cfg.Auth.PublicOrigins) == 0 {
		logger.WarnContext(ctx, "PRICEDIN_PUBLIC_ORIGIN is empty: no login from the web app is accepted")
	}

	loader, closeLoader := newCompanyLoader(ctx, cfg, pool, logger)
	// Deferred after the pool, so it runs first: the runner's lock pool
	// closes before the query pool.
	defer closeLoader()

	api := http.NewServeMux()
	api.Handle("GET /healthz", health.Liveness())
	api.Handle("GET /readyz", health.Readiness(pool, readinessTimeout, logger))
	api.Handle("/api/", httpapi.New(httpapi.Options{
		Logger:             logger,
		Auth:               authSvc,
		Stocks:             stocks.New(st, stocks.Options{Loader: loader, Logger: logger, SECRetryAttempts: ingest.SECRetryAttempts}),
		Origins:            auth.NewOrigins(cfg.Auth.PublicOrigins),
		CookieSecure:       cfg.Auth.CookieSecure,
		TrustedProxyHeader: cfg.Auth.TrustedProxyHeader,
		TrustedProxyHops:   cfg.Auth.TrustedProxyHops,
	}))

	metrics := http.NewServeMux()
	metrics.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))

	var lc net.ListenConfig
	apiListener, err := lc.Listen(ctx, "tcp", cfg.HTTP.Addr)
	if err != nil {
		return fmt.Errorf("serve: listen api: %w", err)
	}
	metricsListener, err := lc.Listen(ctx, "tcp", cfg.HTTP.MetricsAddr)
	if err != nil {
		_ = apiListener.Close()
		return fmt.Errorf("serve: listen metrics: %w", err)
	}

	return httpserver.Run(ctx, logger,
		httpserver.Unit{Name: "api", Server: httpserver.NewServer(httpserver.RequestID(httpserver.Observe(logger, reg)(api))), Listener: apiListener},
		httpserver.Unit{Name: "metrics", Server: httpserver.NewServer(metrics), Listener: metricsListener},
	)
}
