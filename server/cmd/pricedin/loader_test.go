package main

import (
	"errors"
	"fmt"
	"log/slog"
	"testing"

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
