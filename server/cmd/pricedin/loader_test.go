package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/config"
	"github.com/sourcehawk/pricedin/server/internal/ingest"
	"github.com/sourcehawk/pricedin/server/internal/stocks"
)

func TestLoadErrorsMapToTheService(t *testing.T) {
	unknown := loadError(fmt.Errorf("ingest: %q: %w", "ZZZZ", ingest.ErrUnknownSymbol))
	if !errors.Is(unknown, stocks.ErrUnknownTicker) || !errors.Is(unknown, ingest.ErrUnknownSymbol) {
		t.Errorf("loadError(unknown symbol) = %v, want ErrUnknownTicker and ErrUnknownSymbol", unknown)
	}
	other := errors.New("sec: 503")
	if got := loadError(other); errors.Is(got, stocks.ErrUnknownTicker) || !errors.Is(got, other) {
		t.Errorf("loadError(other) = %v, want the error as it is", got)
	}
	if loadError(nil) != nil {
		t.Error("loadError(nil) != nil")
	}
}

func TestOnDemandLoadingIsOffWithoutTheSourceSettings(t *testing.T) {
	cfg, err := config.Load(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	loader, closeLoader := newCompanyLoader(t.Context(), cfg, nil, slog.New(slog.DiscardHandler))
	defer closeLoader()
	if _, ok := loader.(stocks.UnavailableLoader); !ok {
		t.Errorf("loader = %T, want stocks.UnavailableLoader without the SEC user agent and the Massive key", loader)
	}
}

// A runner that cannot start in time must not hold up serve: the probe
// gives up, and the first load tries again.
func TestTheStartupProbeOfTheRunnerHasADeadline(t *testing.T) {
	calls := 0
	build := func(ctx context.Context) (*ingest.Runner, error) {
		calls++
		<-ctx.Done()
		return nil, ctx.Err()
	}
	start := time.Now()
	loader, closeLoader := startLoader(t.Context(), build, 50*time.Millisecond, slog.New(slog.DiscardHandler))
	defer closeLoader()
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("startLoader took %v, want it to give up at its deadline", elapsed)
	}
	if _, ok := loader.(*runnerLoader); !ok || calls != 1 {
		t.Errorf("loader = %T after %d builds, want a runnerLoader that tries again at the first load", loader, calls)
	}
}

func TestAPoolerInTransactionModeTurnsLoadingOff(t *testing.T) {
	build := func(context.Context) (*ingest.Runner, error) { return nil, ingest.ErrSessionRequired }
	loader, closeLoader := startLoader(t.Context(), build, time.Second, slog.New(slog.DiscardHandler))
	defer closeLoader()
	if _, ok := loader.(stocks.UnavailableLoader); !ok {
		t.Errorf("loader = %T, want stocks.UnavailableLoader", loader)
	}
}
