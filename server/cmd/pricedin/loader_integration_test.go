//go:build integration

package main

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sourcehawk/pricedin/server/internal/config"
	"github.com/sourcehawk/pricedin/server/internal/stocks"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

func TestTheServedLoaderLoadsACompanyFromTheSources(t *testing.T) {
	env := fixtureEnv(t)
	cfg, err := config.Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	loader, closeLoader := newCompanyLoader(t.Context(), cfg, pool, slog.New(slog.DiscardHandler))
	defer closeLoader()
	if _, ok := loader.(*runnerLoader); !ok {
		t.Fatalf("loader = %T, want the ingestion runner", loader)
	}
	svc := stocks.New(store.New(pool), stocks.Options{Loader: loader})

	p, err := svc.Load(t.Context(), "KO")
	if err != nil {
		t.Fatalf("Load(KO) error = %v", err)
	}
	if !p.Loaded || p.LatestClose == nil || p.TTMEPS == nil {
		t.Errorf("profile after the load = %+v, want KO loaded with a close and a TTM EPS", p)
	}
	if _, err := svc.Load(t.Context(), "ZZZZ"); !errors.Is(err, stocks.ErrUnknownTicker) {
		t.Errorf("Load(ZZZZ) error = %v, want ErrUnknownTicker", err)
	}
}
