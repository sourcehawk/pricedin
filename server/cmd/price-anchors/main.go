// Command price-anchors writes the anchor files of tools/price-fixtures from
// the SEC fixtures, so that the synthetic prices agree with the companies'
// reported earnings. It is a development tool; `make generate-price-fixtures`
// runs it before the price generator.
//
// For each ticker of the generator's config that has a cik, an anchor_pe
// and an anchors path, it derives the fiscal quarters from the company
// facts fixture, with the config's splits as provider splits. Each quarter
// with a positive TTM diluted EPS gives one anchor: on the day that TTM
// became known (the latest first filing of its four quarters, moved back to
// a trading day), the split-adjusted close is TTM EPS × anchor_pe. So the
// P/E that PricedIn computes on each filing day is anchor_pe.
//
// Usage, from the server folder:
//
//	go run ./cmd/price-anchors [-sec ../testdata/sec] [-config ../tools/price-fixtures/config.json]
package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/fundamentals"
	"github.com/sourcehawk/pricedin/server/internal/prices"
	"github.com/sourcehawk/pricedin/server/internal/sec"
)

// config is the part of the price generator's config that this tool reads.
type config struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Tickers []struct {
		Symbol   string  `json:"symbol"`
		CIK      int64   `json:"cik"`
		AnchorPE float64 `json:"anchor_pe"`
		Anchors  string  `json:"anchors"`
		Splits   []struct {
			Date string  `json:"date"`
			From float64 `json:"from"`
			To   float64 `json:"to"`
		} `json:"splits"`
	} `json:"tickers"`
}

type anchor struct {
	Date   string  `json:"date"`
	TTMEPS float64 `json:"ttm_eps"`
	PE     float64 `json:"pe"`
}

func main() {
	secDir := flag.String("sec", filepath.Join("..", "testdata", "sec"), "the SEC fixture folder")
	configPath := flag.String("config", filepath.Join("..", "tools", "price-fixtures", "config.json"), "the price generator's config")
	flag.Parse()
	if err := run(*secDir, *configPath); err != nil {
		fmt.Fprintln(os.Stderr, "price-anchors:", err)
		os.Exit(1)
	}
}

func run(secDir, configPath string) error {
	files, err := build(secDir, configPath)
	if err != nil {
		return err
	}
	for _, path := range slices.Sorted(maps.Keys(files)) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // fixtures are public
			return err
		}
		if err := os.WriteFile(path, files[path], 0o644); err != nil { //nolint:gosec // fixtures are public
			return err
		}
		fmt.Printf("price-anchors: wrote %s\n", path)
	}
	return nil
}

// build returns the content of each anchors file by its path.
func build(secDir, configPath string) (map[string][]byte, error) {
	data, err := os.ReadFile(configPath) //nolint:gosec // the operator names the config file
	if err != nil {
		return nil, err
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", configPath, err)
	}
	from, err := domain.ParseDate(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("from: %w", err)
	}
	to, err := domain.ParseDate(cfg.To)
	if err != nil {
		return nil, fmt.Errorf("to: %w", err)
	}
	files := map[string][]byte{}
	for _, tc := range cfg.Tickers {
		if tc.CIK <= 0 || tc.AnchorPE <= 0 || tc.Anchors == "" {
			continue
		}
		var splits []domain.Split
		for _, s := range tc.Splits {
			d, err := domain.ParseDate(s.Date)
			if err != nil {
				return nil, fmt.Errorf("%s: split: %w", tc.Symbol, err)
			}
			splits = append(splits, domain.Split{Symbol: tc.Symbol, Date: d, From: s.From, To: s.To, Source: "massive"})
		}
		anchors, err := anchorsOf(filepath.Join(secDir, "data.sec.gov", "api", "xbrl", "companyfacts",
			"CIK"+domain.CIK(tc.CIK).String()+".json"), domain.CIK(tc.CIK), splits, tc.AnchorPE, from, to)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", tc.Symbol, err)
		}
		out, err := json.MarshalIndent(anchors, "", "  ")
		if err != nil {
			return nil, err
		}
		files[filepath.Join(filepath.Dir(configPath), tc.Anchors)] = append(out, '\n')
	}
	return files, nil
}

func anchorsOf(factsPath string, cik domain.CIK, splits []domain.Split, pe float64, from, to domain.Date) ([]anchor, error) {
	body, err := os.ReadFile(factsPath) //nolint:gosec // the path comes from the config's CIK
	if err != nil {
		return nil, err
	}
	facts, err := sec.ParseCompanyFacts(body)
	if err != nil {
		return nil, err
	}
	series, err := fundamentals.Derive(cik, facts, fundamentals.Options{Splits: splits, ProviderChecked: true})
	if err != nil {
		return nil, err
	}
	byDay := map[domain.Date]anchor{}
	for i := range series.Quarters {
		v, ok := fundamentals.TTM(series, fundamentals.EPSDiluted, i)
		if !ok || v.V <= 0 {
			continue
		}
		d := v.FirstFiled
		for !prices.IsTradingDay(d) {
			d = d.AddDays(-1)
		}
		if d.Before(from) || d.After(to) {
			continue
		}
		// A later quarter on the same trading day wins.
		byDay[d] = anchor{Date: d.String(), TTMEPS: math.Round(v.V*1e4) / 1e4, PE: pe}
	}
	if len(byDay) == 0 {
		return nil, errors.New("no quarter with a positive TTM EPS in the range")
	}
	out := make([]anchor, 0, len(byDay))
	for _, a := range byDay {
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b anchor) int { return cmp.Compare(a.Date, b.Date) })
	return out, nil
}
