package main

import (
	"context"
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

	"github.com/sourcehawk/pricedin/server/internal/config"
	"github.com/sourcehawk/pricedin/server/internal/health"
	"github.com/sourcehawk/pricedin/server/internal/httpserver"
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
	pool, err := pgxpool.New(ctx, cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("serve: database pool: %w", err)
	}
	defer pool.Close()

	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	api := http.NewServeMux()
	api.Handle("GET /healthz", health.Liveness())
	api.Handle("GET /readyz", health.Readiness(pool, readinessTimeout, logger))

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
		httpserver.Unit{Name: "api", Server: httpserver.NewServer(httpserver.Observe(logger, reg)(api)), Listener: apiListener},
		httpserver.Unit{Name: "metrics", Server: httpserver.NewServer(metrics), Listener: metricsListener},
	)
}
