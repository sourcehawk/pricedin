// Command price-fixtures writes synthetic Massive API responses for tests.
// The prices are a seeded random walk, not real market data, because the
// provider's license forbids redistribution of its data.
//
// Usage, from this folder:
//
//	go run . [-config config.json] [-out ../../testdata/prices/massive]
//
// It replaces these folders of the output, one file per request:
//
//	grouped/<YYYY-MM-DD>.json  /v2/aggs/grouped/locale/us/market/stocks/<date>, only for trading days
//	aggs/<SYMBOL>.json         /v2/aggs/ticker/<SYMBOL>/range/1/day/<from>/<to> for the whole range
//	splits/<SYMBOL>.json       /stocks/v1/splits?ticker=<SYMBOL>
//	dividends/<SYMBOL>.json    /stocks/v1/dividends?ticker=<SYMBOL>
//	tickers/page-<N>.json      /v3/reference/tickers; page N+1 is next_url cursor=page-<N+1>
//	ticker/<SYMBOL>.json       /v3/reference/tickers/<SYMBOL>
//
// Every price is raw, as with adjusted=false. A server that serves these
// files answers a date without a grouped file with an empty result, and
// cuts the aggs file to the requested range.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	configPath := flag.String("config", "config.json", "generator input")
	out := flag.String("out", filepath.Join("..", "..", "testdata", "prices", "massive"), "output folder")
	flag.Parse()
	if err := run(*configPath, *out); err != nil {
		fmt.Fprintln(os.Stderr, "price-fixtures:", err)
		os.Exit(1)
	}
}

func run(configPath, out string) error {
	cfg, err := readConfig(configPath)
	if err != nil {
		return err
	}
	files, err := generate(cfg, filepath.Dir(configPath))
	if err != nil {
		return err
	}
	for _, dir := range []string{"grouped", "aggs", "splits", "dividends", "tickers", "ticker"} {
		if err := os.RemoveAll(filepath.Join(out, dir)); err != nil {
			return fmt.Errorf("clear %s: %w", dir, err)
		}
	}
	for path, data := range files {
		full := filepath.Join(out, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		if err := os.WriteFile(full, data, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	fmt.Printf("price-fixtures: wrote %d files to %s\n", len(files), out)
	return nil
}
