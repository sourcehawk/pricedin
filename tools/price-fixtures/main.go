// Command price-fixtures writes synthetic Massive API responses for tests.
// The prices are a seeded random walk, not real market data, because the
// provider's license forbids redistribution of its data.
//
// Usage, from this folder:
//
//	go run . [-config config.json] [-out ../../testdata/prices/massive] [-grouped-days 10]
//
// The anchors files under anchors/ come from the SEC fixtures: `make
// generate-price-fixtures` writes them with server/cmd/price-anchors first,
// so the prices agree with the companies' reported earnings.
//
// It replaces these folders of the output, one file per request:
//
//	grouped/<YYYY-MM-DD>.json  /v2/aggs/grouped/locale/us/market/stocks/<date>, only for trading days, the last -grouped-days of them when it is more than 0
//	aggs/<SYMBOL>.json         /v2/aggs/ticker/<SYMBOL>/range/1/day/<from>/<to> for the whole range
//	splits/<SYMBOL>.json       /stocks/v1/splits?ticker=<SYMBOL>
//	dividends/<SYMBOL>.json    /stocks/v1/dividends?ticker=<SYMBOL>
//	tickers/page-<N>.json      /v3/reference/tickers; page N+1 is next_url cursor=page-<N+1>
//	ticker/<SYMBOL>.json       /v3/reference/tickers/<SYMBOL>
//
// Every price is raw, as with adjusted=false. A grouped file holds, for each
// ticker, the same bar as its aggs file on that date. A server that serves
// these files must follow these rules:
//
//   - For a grouped request on a trading day of the range without a grouped
//     file, answer with the bar of that date from every aggs file, not with
//     an empty result.
//   - For a grouped request on a market holiday, answer with an empty result
//     and status OK.
//   - For an aggs request, cut the aggs file to the requested range.
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
	groupedDays := flag.Int("grouped-days", defaultGroupedDays, "trading days at the end of the range with a grouped file; 0 for all")
	flag.Parse()
	if err := run(*configPath, *out, *groupedDays); err != nil {
		fmt.Fprintln(os.Stderr, "price-fixtures:", err)
		os.Exit(1)
	}
}

func run(configPath, out string, groupedDays int) error {
	cfg, err := readConfig(configPath)
	if err != nil {
		return err
	}
	files, err := generate(cfg, filepath.Dir(configPath), groupedDays)
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
		// The files are public test data; a container user other than the
		// owner reads them.
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil { //nolint:gosec // see above
			return fmt.Errorf("write %s: %w", path, err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil { //nolint:gosec // see above
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	fmt.Printf("price-fixtures: wrote %d files to %s\n", len(files), out)
	return nil
}
