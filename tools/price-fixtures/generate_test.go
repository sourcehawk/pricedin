package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type testBar struct {
	T  string  `json:"T"`
	V  float64 `json:"v"`
	VW float64 `json:"vw"`
	O  float64 `json:"o"`
	C  float64 `json:"c"`
	H  float64 `json:"h"`
	L  float64 `json:"l"`
	Ts int64   `json:"t"`
	N  int64   `json:"n"`
}

type testAggs struct {
	Ticker       string    `json:"ticker"`
	ResultsCount int       `json:"resultsCount"`
	Adjusted     bool      `json:"adjusted"`
	Status       string    `json:"status"`
	Results      []testBar `json:"results"`
}

type testDividend struct {
	ID                 string   `json:"id"`
	Ticker             string   `json:"ticker"`
	ExDividendDate     string   `json:"ex_dividend_date"`
	DeclarationDate    string   `json:"declaration_date"`
	PayDate            string   `json:"pay_date"`
	CashAmount         float64  `json:"cash_amount"`
	Frequency          int      `json:"frequency"`
	DistributionType   string   `json:"distribution_type"`
	HistoricalFactor   *float64 `json:"historical_adjustment_factor"`
	SplitAdjustedValue *float64 `json:"split_adjusted_cash_amount"`
}

func baseConfig() config {
	return config{
		Seed: 7,
		From: "2024-01-02",
		To:   "2024-12-31",
		Tickers: []tickerConfig{
			{
				Symbol: "AAA", CIK: 1, Name: "AAA CORP", Exchange: "XNYS", Type: "CS",
				StartPrice: 1000, Drift: 0.1, Volatility: 0.3, Volume: 1e7,
				Splits: []splitConfig{{Date: "2024-06-10", From: 1, To: 10}},
			},
			{
				Symbol: "BBB", CIK: 22, Name: "BBB INC", Exchange: "XNAS", Type: "CS",
				StartPrice: 60, Drift: 0.05, Volatility: 0.15, Volume: 2e6,
				Dividends: &dividendConfig{
					FirstAmount: 0.5, AnnualRaise: 0.05, ExDates: []string{"03-14", "06-14", "09-13", "12-13"},
					DeclaredYearlyOn: "02-20", PayDaysAfter: 14,
				},
			},
		},
		TickersPageSize: 1,
	}
}

// mustGenerate writes grouped files for every trading day, unless a test
// asks for a window with mustGenerateWindow.
func mustGenerate(t *testing.T, cfg config, dir string) map[string][]byte {
	t.Helper()
	return mustGenerateWindow(t, cfg, dir, 0)
}

func mustGenerateWindow(t *testing.T, cfg config, dir string, groupedDays int) map[string][]byte {
	t.Helper()
	files, err := generate(cfg, dir, groupedDays)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	return files
}

func decodeFile[T any](t *testing.T, files map[string][]byte, path string) T {
	t.Helper()
	var v T
	body, ok := files[path]
	if !ok {
		t.Fatalf("no file %s", path)
	}
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return v
}

func closesByDate(t *testing.T, files map[string][]byte, symbol string) map[string]float64 {
	t.Helper()
	aggs := decodeFile[testAggs](t, files, "aggs/"+symbol+".json")
	out := map[string]float64{}
	for _, b := range aggs.Results {
		out[sessionDate(b.Ts)] = b.C
	}
	return out
}

func sessionDate(ms int64) string {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		panic(err)
	}
	return time.UnixMilli(ms).In(ny).Format(time.DateOnly)
}

func TestOutputIsDeterministic(t *testing.T) {
	a := mustGenerate(t, baseConfig(), t.TempDir())
	b := mustGenerate(t, baseConfig(), t.TempDir())
	if len(a) != len(b) {
		t.Fatalf("file counts differ: %d and %d", len(a), len(b))
	}
	for path, body := range a {
		if !bytes.Equal(body, b[path]) {
			t.Errorf("%s differs between two runs", path)
		}
	}
}

func TestTheSeedChangesThePrices(t *testing.T) {
	cfg := baseConfig()
	a := mustGenerate(t, cfg, t.TempDir())
	cfg.Seed++
	b := mustGenerate(t, cfg, t.TempDir())
	if bytes.Equal(a["aggs/AAA.json"], b["aggs/AAA.json"]) {
		t.Error("aggs/AAA.json is the same for two seeds")
	}
}

func TestAnotherTickerDoesNotChangeTheOthers(t *testing.T) {
	cfg := baseConfig()
	both := mustGenerate(t, cfg, t.TempDir())
	cfg.Tickers = cfg.Tickers[:1]
	one := mustGenerate(t, cfg, t.TempDir())
	if !bytes.Equal(both["aggs/AAA.json"], one["aggs/AAA.json"]) {
		t.Error("aggs/AAA.json changed when BBB was added")
	}
}

func TestGroupedFilesCoverEveryTradingDayAndTicker(t *testing.T) {
	files := mustGenerate(t, baseConfig(), t.TempDir())
	var got []string
	for path := range files {
		if day, ok := strings.CutPrefix(path, "grouped/"); ok {
			got = append(got, strings.TrimSuffix(day, ".json"))
		}
	}
	slices.Sort(got)
	from, to := mustDate("2024-01-02"), mustDate("2024-12-31")
	days := tradingDays(from, to)
	want := make([]string, 0, len(days))
	for _, d := range days {
		want = append(want, d.Format(time.DateOnly))
	}
	if !slices.Equal(want, got) {
		t.Fatalf("grouped dates: got %d files, want %d trading days", len(got), len(want))
	}
	if len(want) != 252 {
		t.Errorf("2024 has %d trading days here, want 252", len(want))
	}
	g := decodeFile[testAggs](t, files, "grouped/2024-06-10.json")
	symbols := make([]string, 0, len(g.Results))
	for _, b := range g.Results {
		symbols = append(symbols, b.T)
		if sessionDate(b.Ts) != "2024-06-10" {
			t.Errorf("%s bar in grouped/2024-06-10.json is dated %s", b.T, sessionDate(b.Ts))
		}
	}
	if !slices.Equal([]string{"AAA", "BBB"}, symbols) {
		t.Errorf("grouped symbols = %v, want [AAA BBB]", symbols)
	}
	if g.ResultsCount != 2 || g.Adjusted || g.Status != "OK" {
		t.Errorf("grouped header = count %d, adjusted %t, status %q", g.ResultsCount, g.Adjusted, g.Status)
	}
}

func TestGroupedFilesCoverOnlyTheLastTradingDaysOfTheWindow(t *testing.T) {
	files := mustGenerateWindow(t, baseConfig(), t.TempDir(), 3)
	var got []string
	for path := range files {
		if day, ok := strings.CutPrefix(path, "grouped/"); ok {
			got = append(got, strings.TrimSuffix(day, ".json"))
		}
	}
	slices.Sort(got)
	// 2024-12-25 is Christmas Day.
	want := []string{"2024-12-27", "2024-12-30", "2024-12-31"}
	if !slices.Equal(want, got) {
		t.Errorf("grouped dates = %v, want %v", got, want)
	}
	if _, ok := files["aggs/AAA.json"]; !ok {
		t.Error("no aggs/AAA.json; the aggs files must still cover the whole range")
	}
}

func TestGroupedBarsEqualTheAggsBarsOfTheSameDay(t *testing.T) {
	files := mustGenerate(t, baseConfig(), t.TempDir())
	aggs := decodeFile[testAggs](t, files, "aggs/BBB.json")
	var fromAggs testBar
	for _, b := range aggs.Results {
		if sessionDate(b.Ts) == "2024-06-10" {
			fromAggs = b
		}
	}
	for _, b := range decodeFile[testAggs](t, files, "grouped/2024-06-10.json").Results {
		if b.T == "BBB" {
			b.T = ""
			if b != fromAggs {
				t.Errorf("grouped bar %+v, want the aggs bar %+v", b, fromAggs)
			}
			return
		}
	}
	t.Error("no BBB bar in grouped/2024-06-10.json")
}

func TestBarsAreConsistent(t *testing.T) {
	files := mustGenerate(t, baseConfig(), t.TempDir())
	for _, sym := range []string{"AAA", "BBB"} {
		aggs := decodeFile[testAggs](t, files, "aggs/"+sym+".json")
		if aggs.Ticker != sym || aggs.ResultsCount != len(aggs.Results) || aggs.Adjusted {
			t.Errorf("%s header: ticker %q, count %d of %d, adjusted %t", sym, aggs.Ticker, aggs.ResultsCount, len(aggs.Results), aggs.Adjusted)
		}
		for _, b := range aggs.Results {
			if b.L <= 0 || b.L > math.Min(b.O, b.C) || b.H < math.Max(b.O, b.C) || b.VW < b.L || b.VW > b.H || b.V <= 0 || b.N <= 0 {
				t.Errorf("%s %s: inconsistent bar %+v", sym, sessionDate(b.Ts), b)
			}
		}
	}
}

func TestASplitDividesTheRawPrices(t *testing.T) {
	files := mustGenerate(t, baseConfig(), t.TempDir())
	closes := closesByDate(t, files, "AAA")
	if ratio := closes["2024-06-07"] / closes["2024-06-10"]; ratio < 7 || ratio > 14 {
		t.Errorf("raw close before / after the 10-for-1 split = %v, want about 10", ratio)
	}

	type splitsPage struct {
		Results []struct {
			ExecutionDate string  `json:"execution_date"`
			SplitFrom     float64 `json:"split_from"`
			SplitTo       float64 `json:"split_to"`
			Ticker        string  `json:"ticker"`
			Factor        float64 `json:"historical_adjustment_factor"`
			Type          string  `json:"adjustment_type"`
		} `json:"results"`
	}
	splits := decodeFile[splitsPage](t, files, "splits/AAA.json")
	if len(splits.Results) != 1 {
		t.Fatalf("splits/AAA.json has %d splits, want 1", len(splits.Results))
	}
	s := splits.Results[0]
	if s.ExecutionDate != "2024-06-10" || s.SplitFrom != 1 || s.SplitTo != 10 || s.Ticker != "AAA" || s.Factor != 0.1 || s.Type != "forward_split" {
		t.Errorf("split = %+v", s)
	}
	if empty := decodeFile[struct{ Results []any }](t, files, "splits/BBB.json"); len(empty.Results) != 0 {
		t.Errorf("splits/BBB.json has %d splits, want 0", len(empty.Results))
	}
}

func TestAnchorsFixTheAdjustedClose(t *testing.T) {
	dir := t.TempDir()
	// 2024-03-30 is a Saturday; the anchor moves to Thursday 2024-03-28,
	// because 2024-03-29 is Good Friday.
	anchors := `[{"date":"2024-03-30","ttm_eps":4,"pe":25},{"date":"2024-09-30","ttm_eps":5,"pe":30}]`
	if err := os.WriteFile(filepath.Join(dir, "aaa.json"), []byte(anchors), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := baseConfig()
	cfg.Tickers[0].Anchors = "aaa.json"
	closes := closesByDate(t, mustGenerate(t, cfg, dir), "AAA")

	// Before the 10-for-1 split, the raw close is 10 times the adjusted one.
	if got, want := closes["2024-03-28"], 4.0*25*10; math.Abs(got-want) > 0.01 {
		t.Errorf("raw close on 2024-03-28 = %v, want %v", got, want)
	}
	if got, want := closes["2024-09-30"], 5.0*30; math.Abs(got-want) > 0.01 {
		t.Errorf("raw close on 2024-09-30 = %v, want %v", got, want)
	}
}

func TestAnAnchorOnTheLastDayMovesToTheTradingDayBefore(t *testing.T) {
	dir := t.TempDir()
	// 2024-06-30 is a Sunday and the last day of the range.
	if err := os.WriteFile(filepath.Join(dir, "aaa.json"), []byte(`[{"date":"2024-06-30","ttm_eps":5,"pe":30}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := baseConfig()
	cfg.To = "2024-06-30"
	cfg.Tickers[0].Anchors = "aaa.json"
	closes := closesByDate(t, mustGenerate(t, cfg, dir), "AAA")
	if got, want := closes["2024-06-28"], 5.0*30; math.Abs(got-want) > 0.01 {
		t.Errorf("raw close on 2024-06-28 = %v, want %v", got, want)
	}
}

func TestTheOrderOfExDatesDoesNotChangeTheDividends(t *testing.T) {
	// AAA splits on 2024-06-10, so its dividends before and after the
	// split have different split-adjusted amounts.
	withDividends := func(exDates ...string) config {
		cfg := baseConfig()
		cfg.Tickers[0].Dividends = &dividendConfig{FirstAmount: 2, ExDates: exDates, PayDaysAfter: 7}
		return cfg
	}
	sorted := mustGenerate(t, withDividends("03-14", "09-13"), t.TempDir())
	unsorted := mustGenerate(t, withDividends("09-13", "03-14"), t.TempDir())
	if !bytes.Equal(sorted["dividends/AAA.json"], unsorted["dividends/AAA.json"]) {
		t.Errorf("dividends/AAA.json depends on the order of ex_dates:\nsorted:   %s\nunsorted: %s", sorted["dividends/AAA.json"], unsorted["dividends/AAA.json"])
	}
}

func TestDividendsFollowTheSchedule(t *testing.T) {
	files := mustGenerate(t, baseConfig(), t.TempDir())
	divs := decodeFile[struct{ Results []testDividend }](t, files, "dividends/BBB.json").Results

	exDates := make([]string, 0, len(divs))
	for _, d := range divs {
		exDates = append(exDates, d.ExDividendDate)
		if d.DeclarationDate != "2024-02-20" && d.DeclarationDate != "2025-02-20" {
			t.Errorf("dividend %s declared on %s, want the yearly date", d.ExDividendDate, d.DeclarationDate)
		}
		if d.Ticker != "BBB" || d.Frequency != 4 || d.DistributionType != "recurring" || !strings.HasPrefix(d.ID, "E") {
			t.Errorf("dividend %+v", d)
		}
		future := d.ExDividendDate > "2024-12-31"
		if future != (d.HistoricalFactor == nil) || future != (d.SplitAdjustedValue == nil) {
			t.Errorf("dividend %s: the adjustment fields must be present exactly when the ex-date is not after the range", d.ExDividendDate)
		}
	}
	// 2024-12-13 is a Friday and a trading day; the 2025-03-14 dividend is
	// declared on 2025-02-20, after the range, so it is not listed.
	want := []string{"2024-03-14", "2024-06-14", "2024-09-13", "2024-12-13"}
	if !slices.Equal(want, exDates) {
		t.Errorf("ex-dates = %v, want %v", exDates, want)
	}
	if divs[0].CashAmount != 0.5 || divs[3].CashAmount != 0.5 {
		t.Errorf("amounts = %v and %v, want 0.5 in the first year", divs[0].CashAmount, divs[3].CashAmount)
	}
	if empty := decodeFile[struct{ Results []any }](t, files, "dividends/AAA.json"); len(empty.Results) != 0 {
		t.Errorf("dividends/AAA.json has %d dividends, want 0", len(empty.Results))
	}
}

func TestAnnouncedDividendsAfterTheRangeAreListed(t *testing.T) {
	cfg := baseConfig()
	cfg.To = "2024-12-02"
	cfg.Tickers[1].Dividends.DeclaredYearlyOn = ""
	cfg.Tickers[1].Dividends.DeclaredDaysBefore = 30
	files := mustGenerate(t, cfg, t.TempDir())
	divs := decodeFile[struct{ Results []testDividend }](t, files, "dividends/BBB.json").Results
	last := divs[len(divs)-1]
	if last.ExDividendDate != "2024-12-13" || last.DeclarationDate != "2024-11-13" || last.HistoricalFactor != nil {
		t.Errorf("last dividend = %+v, want the announced 2024-12-13 one without adjustment fields", last)
	}
}

func TestTickerPagesLinkToEachOther(t *testing.T) {
	files := mustGenerate(t, baseConfig(), t.TempDir())
	type page struct {
		Results []struct {
			Ticker string `json:"ticker"`
			CIK    string `json:"cik"`
		} `json:"results"`
		NextURL string `json:"next_url"`
	}
	p1 := decodeFile[page](t, files, "tickers/page-1.json")
	p2 := decodeFile[page](t, files, "tickers/page-2.json")
	if len(p1.Results) != 1 || p1.Results[0].Ticker != "AAA" || p1.Results[0].CIK != "0000000001" {
		t.Errorf("page 1 = %+v", p1.Results)
	}
	if p1.NextURL != "https://api.massive.com/v3/reference/tickers?cursor=page-2" {
		t.Errorf("page 1 next_url = %q", p1.NextURL)
	}
	if len(p2.Results) != 1 || p2.Results[0].Ticker != "BBB" || p2.NextURL != "" {
		t.Errorf("page 2 = %+v, next_url %q", p2.Results, p2.NextURL)
	}
	overview := decodeFile[struct {
		Results struct {
			Ticker string `json:"ticker"`
			CIK    string `json:"cik"`
		} `json:"results"`
	}](t, files, "ticker/BBB.json")
	if overview.Results.Ticker != "BBB" || overview.Results.CIK != "0000000022" {
		t.Errorf("ticker/BBB.json = %+v", overview.Results)
	}
}

func TestInvalidConfigurationIsRejected(t *testing.T) {
	tests := map[string]func(*config){
		"no tickers":           func(c *config) { c.Tickers = nil },
		"bad from":             func(c *config) { c.From = "2024-13-01" },
		"from after to":        func(c *config) { c.From, c.To = c.To, c.From },
		"duplicate symbol":     func(c *config) { c.Tickers[1].Symbol = "AAA" },
		"split ratio of zero":  func(c *config) { c.Tickers[0].Splits[0].To = 0 },
		"no start price":       func(c *config) { c.Tickers[0].StartPrice = 0 },
		"bad ex-date":          func(c *config) { c.Tickers[1].Dividends.ExDates[0] = "13-01" },
		"declared after ex":    func(c *config) { c.Tickers[1].Dividends.DeclaredYearlyOn = "04-01" },
		"missing anchors file": func(c *config) { c.Tickers[0].Anchors = "nope.json" },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := baseConfig()
			change(&cfg)
			if _, err := generate(cfg, t.TempDir(), 0); err == nil {
				t.Error("generate() error = nil, want an error")
			}
		})
	}
}

func TestCommittedFixturesMatchTheGenerator(t *testing.T) {
	cfg, err := readConfig("config.json")
	if err != nil {
		t.Fatal(err)
	}
	want := mustGenerateWindow(t, cfg, ".", defaultGroupedDays)
	root := filepath.Join("..", "..", "testdata", "prices", "massive")
	got := map[string][]byte{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		got[filepath.ToSlash(rel)], err = os.ReadFile(path)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range want {
		if !bytes.Equal(body, got[path]) {
			t.Errorf("%s differs from the generator output; run make generate-price-fixtures", path)
		}
	}
	for path := range got {
		if _, ok := want[path]; !ok {
			t.Errorf("%s is not generator output; run make generate-price-fixtures", path)
		}
	}
}
