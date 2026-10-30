package main

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	// The bar timestamps are midnight in New York.
	_ "time/tzdata"
)

// config is the generator input, read from config.json.
type config struct {
	// Seed makes the output repeatable. Each ticker draws from its own
	// stream, so adding a ticker does not change the others.
	Seed int64 `json:"seed"`
	// From and To are the first and last day with bars, as YYYY-MM-DD.
	From string `json:"from"`
	To   string `json:"to"`
	// TickersPageSize is the number of tickers on each page of the ticker
	// list. 0 puts all on one page.
	TickersPageSize int            `json:"tickers_page_size"`
	Tickers         []tickerConfig `json:"tickers"`
}

type tickerConfig struct {
	Symbol   string `json:"symbol"`
	CIK      int64  `json:"cik"`
	Name     string `json:"name"`
	Exchange string `json:"exchange"`
	Type     string `json:"type"`
	// StartPrice is the raw close on From. Anchors, when given, set the
	// level instead.
	StartPrice float64 `json:"start_price"`
	// Drift is the yearly trend of the log price.
	Drift float64 `json:"drift"`
	// Volatility is the yearly standard deviation of the log price.
	Volatility float64 `json:"volatility"`
	// Volume is the typical daily volume in today's shares.
	Volume    float64         `json:"volume"`
	Splits    []splitConfig   `json:"splits"`
	Dividends *dividendConfig `json:"dividends"`
	// Anchors is the path, relative to the config file, of a JSON list of
	// anchors. The split-adjusted close on each anchor date is exactly
	// ttm_eps × pe. An anchor date that is not a trading day moves to the
	// trading day before it.
	Anchors string `json:"anchors"`
}

type splitConfig struct {
	Date string  `json:"date"`
	From float64 `json:"from"`
	To   float64 `json:"to"`
}

// dividendConfig describes a regular dividend that is paid on the same
// days every year and grows once a year.
type dividendConfig struct {
	// FirstAmount is the cash per share, as paid, in the year of From.
	FirstAmount float64 `json:"first_amount"`
	AnnualRaise float64 `json:"annual_raise"`
	// ExDates are the ex-dividend days of each year, as MM-DD. A day that
	// is not a trading day moves to the next trading day.
	ExDates []string `json:"ex_dates"`
	// DeclaredYearlyOn, as MM-DD, declares all the dividends of a year on
	// one day. When it is empty, each dividend is declared
	// DeclaredDaysBefore its ex-date.
	DeclaredYearlyOn   string `json:"declared_yearly_on"`
	DeclaredDaysBefore int    `json:"declared_days_before"`
	PayDaysAfter       int    `json:"pay_days_after"`
}

type anchor struct {
	Date   string  `json:"date"`
	TTMEPS float64 `json:"ttm_eps"`
	PE     float64 `json:"pe"`
}

const (
	nextURLBase = "https://api.massive.com/v3/reference/tickers?cursor=page-"
	// meanReversion pulls the log price back toward its trend, so that a
	// five-year walk stays at a plausible level.
	meanReversion = 1.5
	tradingYear   = 252
)

func readConfig(path string) (config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the operator names the config file
	if err != nil {
		return config{}, fmt.Errorf("read config: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var cfg config
	if err := dec.Decode(&cfg); err != nil {
		return config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	return cfg, nil
}

// series is the simulated history of one ticker.
type series struct {
	cfg      tickerConfig
	bars     []bar
	adjClose map[time.Time]float64
	splits   []split
}

type split struct {
	date     time.Time
	from, to float64
}

// bar is one result of an aggregates response, in Massive's field order.
type bar struct {
	Ticker string  `json:"T,omitempty"`
	V      float64 `json:"v"`
	VW     float64 `json:"vw"`
	O      float64 `json:"o"`
	C      float64 `json:"c"`
	H      float64 `json:"h"`
	L      float64 `json:"l"`
	T      int64   `json:"t"`
	N      int64   `json:"n"`
}

// generate returns the content of every fixture file by its path relative
// to the output folder. dir is the folder of the config file.
func generate(cfg config, dir string) (map[string][]byte, error) {
	from, err := parseDay(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("from: %w", err)
	}
	to, err := parseDay(cfg.To)
	if err != nil {
		return nil, fmt.Errorf("to: %w", err)
	}
	days := tradingDays(from, to)
	if len(days) == 0 {
		return nil, fmt.Errorf("no trading days from %s to %s", cfg.From, cfg.To)
	}
	if len(cfg.Tickers) == 0 {
		return nil, errors.New("no tickers")
	}
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		return nil, fmt.Errorf("load time zone: %w", err)
	}

	tickers := slices.Clone(cfg.Tickers)
	slices.SortFunc(tickers, func(a, b tickerConfig) int { return cmp.Compare(a.Symbol, b.Symbol) })
	all := make([]series, 0, len(tickers))
	for i := range tickers {
		tc := &tickers[i]
		if tc.Symbol == "" || (i > 0 && tickers[i-1].Symbol == tc.Symbol) {
			return nil, fmt.Errorf("ticker %d: empty or duplicate symbol %q", i, tc.Symbol)
		}
		s, err := simulate(cfg.Seed, *tc, days, dir, newYork)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", tc.Symbol, err)
		}
		all = append(all, s)
	}

	g := generator{seed: cfg.Seed, files: map[string][]byte{}}
	for i := range all {
		s := &all[i]
		g.aggs(s)
		g.splits(s)
		if err := g.dividends(s, from, to); err != nil {
			return nil, fmt.Errorf("%s: dividends: %w", s.cfg.Symbol, err)
		}
		g.overview(&s.cfg, to)
	}
	g.grouped(all, days)
	g.tickerPages(tickers, cfg.TickersPageSize, to)
	return g.files, nil
}

func simulate(seed int64, tc tickerConfig, days []time.Time, dir string, newYork *time.Location) (series, error) {
	splits := make([]split, 0, len(tc.Splits))
	for _, sc := range tc.Splits {
		d, err := parseDay(sc.Date)
		if err != nil {
			return series{}, fmt.Errorf("split: %w", err)
		}
		if sc.From <= 0 || sc.To <= 0 {
			return series{}, fmt.Errorf("split on %s: from and to must be positive", sc.Date)
		}
		splits = append(splits, split{date: d, from: sc.From, to: sc.To})
	}
	slices.SortFunc(splits, func(a, b split) int { return a.date.Compare(b.date) })
	factor := func(d time.Time) float64 {
		f := 1.0
		for _, s := range splits {
			if d.Before(s.date) {
				f *= s.to / s.from
			}
		}
		return f
	}

	h := fnv.New64a()
	_, _ = h.Write([]byte(tc.Symbol))
	rng := rand.New(rand.NewPCG(uint64(seed), h.Sum64())) //nolint:gosec // test data needs no cryptographic randomness

	n := len(days)
	dt := 1.0 / tradingYear
	dev := make([]float64, n)
	for i := 1; i < n; i++ {
		dev[i] = dev[i-1] - meanReversion*dev[i-1]*dt + tc.Volatility*math.Sqrt(dt)*rng.NormFloat64()
	}

	logPrice, err := logPricePath(tc, days, dev, dir, factor(days[0]))
	if err != nil {
		return series{}, err
	}

	s := series{cfg: tc, bars: make([]bar, n), adjClose: make(map[time.Time]float64, n), splits: splits}
	for i, d := range days {
		c := math.Exp(logPrice[i])
		prev := c
		if i > 0 {
			prev = math.Exp(logPrice[i-1])
		}
		o := prev * math.Exp(0.006*rng.NormFloat64())
		hi := math.Max(o, c) * (1 + 0.012*math.Abs(rng.NormFloat64()))
		lo := math.Min(o, c) * (1 - math.Min(0.5, 0.012*math.Abs(rng.NormFloat64())))
		f := factor(d)
		vol := math.Round(tc.Volume * math.Exp(0.35*rng.NormFloat64()) / f)
		b := bar{
			V: math.Max(1, vol),
			O: round(o*f, 2), C: round(c*f, 2), H: round(hi*f, 2), L: round(lo*f, 2),
			T: time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, newYork).UnixMilli(),
		}
		b.VW = math.Min(b.H, math.Max(b.L, round((hi+lo+c)/3*f, 4)))
		b.N = max(1, int64(math.Round(b.V/120)))
		s.bars[i] = b
		s.adjClose[d] = c
	}
	return s, nil
}

// logPricePath returns the split-adjusted log close of each day: a trend
// plus the deviation dev. Without anchors the trend starts at the start
// price and grows at the drift. With anchors it runs through the anchor
// prices, and dev is bent to zero on each anchor day.
func logPricePath(tc tickerConfig, days []time.Time, dev []float64, dir string, startFactor float64) ([]float64, error) {
	n := len(days)
	out := make([]float64, n)
	dt := 1.0 / tradingYear
	if tc.Anchors == "" {
		if tc.StartPrice <= 0 {
			return nil, errors.New("start_price must be positive without anchors")
		}
		base := math.Log(tc.StartPrice / startFactor)
		for i := range out {
			out[i] = base + tc.Drift*float64(i)*dt + dev[i]
		}
		return out, nil
	}

	idx, logA, err := readAnchors(filepath.Join(dir, tc.Anchors), days)
	if err != nil {
		return nil, err
	}
	first, last := idx[0], idx[len(idx)-1]
	for i := range out {
		switch {
		case i <= first:
			out[i] = logA[0] - tc.Drift*float64(first-i)*dt + dev[i] - dev[first]
		case i >= last:
			out[i] = logA[len(idx)-1] + tc.Drift*float64(i-last)*dt + dev[i] - dev[last]
		default:
			j := sortSearch(idx, i)
			a, b := idx[j], idx[j+1]
			w := float64(i-a) / float64(b-a)
			trend := logA[j] + w*(logA[j+1]-logA[j])
			out[i] = trend + dev[i] - (dev[a] + w*(dev[b]-dev[a]))
		}
	}
	return out, nil
}

// sortSearch returns the j with idx[j] < i < idx[j+1].
func sortSearch(idx []int, i int) int {
	j, _ := slices.BinarySearch(idx, i)
	return j - 1
}

// readAnchors returns the day index and the log price of each anchor, in
// date order.
func readAnchors(path string, days []time.Time) ([]int, []float64, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the config file names the anchors file
	if err != nil {
		return nil, nil, fmt.Errorf("anchors: %w", err)
	}
	var anchors []anchor
	if err := json.Unmarshal(data, &anchors); err != nil {
		return nil, nil, fmt.Errorf("anchors %s: %w", path, err)
	}
	if len(anchors) == 0 {
		return nil, nil, fmt.Errorf("anchors %s: empty", path)
	}
	idx := make([]int, 0, len(anchors))
	logA := make([]float64, 0, len(anchors))
	slices.SortFunc(anchors, func(a, b anchor) int { return cmp.Compare(a.Date, b.Date) })
	for _, a := range anchors {
		d, err := parseDay(a.Date)
		if err != nil {
			return nil, nil, fmt.Errorf("anchors %s: %w", path, err)
		}
		if a.TTMEPS*a.PE <= 0 {
			return nil, nil, fmt.Errorf("anchors %s: %s: ttm_eps × pe must be positive", path, a.Date)
		}
		// The last trading day on or before d.
		i, found := slices.BinarySearchFunc(days, d, func(x, t time.Time) int { return x.Compare(t) })
		if !found {
			i--
		}
		if i < 0 || d.After(days[len(days)-1]) {
			return nil, nil, fmt.Errorf("anchors %s: %s is outside the range", path, a.Date)
		}
		if len(idx) > 0 && idx[len(idx)-1] == i {
			return nil, nil, fmt.Errorf("anchors %s: two anchors on the trading day %s", path, days[i].Format(time.DateOnly))
		}
		idx = append(idx, i)
		logA = append(logA, math.Log(a.TTMEPS*a.PE))
	}
	return idx, logA, nil
}

type generator struct {
	seed  int64
	files map[string][]byte
}

func (g *generator) requestID(path string) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%d|%s", g.seed, path))
	return hex.EncodeToString(sum[:16])
}

// eventID imitates Massive's identifiers of splits and dividends.
func eventID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "E" + hex.EncodeToString(sum[:])
}

// barsDocument writes an aggregates response with one result on each line,
// which keeps the diffs of regenerated fixtures readable.
func barsDocument(head string, results []bar, requestID string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, `{%s"queryCount":%d,"resultsCount":%d,"adjusted":false,"results":[`, head, len(results), len(results))
	for i, r := range results {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
		line, _ := json.Marshal(r) //nolint:errchkjson // a bar always marshals
		b.Write(line)
	}
	fmt.Fprintf(&b, "\n],\"status\":\"OK\",\"request_id\":%q,\"count\":%d}\n", requestID, len(results))
	return b.Bytes()
}

func (g *generator) aggs(s *series) {
	path := "aggs/" + s.cfg.Symbol + ".json"
	head := fmt.Sprintf(`"ticker":%q,`, s.cfg.Symbol)
	g.files[path] = barsDocument(head, s.bars, g.requestID(path))
}

func (g *generator) grouped(all []series, days []time.Time) {
	for i, d := range days {
		path := "grouped/" + d.Format(time.DateOnly) + ".json"
		results := make([]bar, 0, len(all))
		for j := range all {
			s := &all[j]
			b := s.bars[i]
			b.Ticker = s.cfg.Symbol
			results = append(results, b)
		}
		g.files[path] = barsDocument("", results, g.requestID(path))
	}
}

func (g *generator) splits(s *series) {
	type result struct {
		ID         string  `json:"id"`
		Date       string  `json:"execution_date"`
		From       float64 `json:"split_from"`
		To         float64 `json:"split_to"`
		Ticker     string  `json:"ticker"`
		Type       string  `json:"adjustment_type"`
		Historical float64 `json:"historical_adjustment_factor"`
	}
	results := make([]result, 0, len(s.splits))
	for i, sp := range s.splits {
		kind := "forward_split"
		if sp.to < sp.from {
			kind = "reverse_split"
		}
		hist := 1.0
		for _, later := range s.splits[i:] {
			hist *= later.from / later.to
		}
		date := sp.date.Format(time.DateOnly)
		results = append(results, result{
			ID: eventID(s.cfg.Symbol, date, "split"), Date: date, From: sp.from, To: sp.to,
			Ticker: s.cfg.Symbol, Type: kind, Historical: round(hist, 6),
		})
	}
	path := "splits/" + s.cfg.Symbol + ".json"
	g.writeJSON(path, struct {
		Status    string   `json:"status"`
		RequestID string   `json:"request_id"`
		Results   []result `json:"results"`
	}{"OK", g.requestID(path), results})
}

type dividendResult struct {
	ID               string   `json:"id"`
	Ticker           string   `json:"ticker"`
	RecordDate       string   `json:"record_date"`
	PayDate          string   `json:"pay_date"`
	DeclarationDate  string   `json:"declaration_date"`
	ExDividendDate   string   `json:"ex_dividend_date"`
	Frequency        int      `json:"frequency"`
	CashAmount       float64  `json:"cash_amount"`
	Currency         string   `json:"currency"`
	DistributionType string   `json:"distribution_type"`
	Historical       *float64 `json:"historical_adjustment_factor,omitempty"`
	SplitAdjusted    *float64 `json:"split_adjusted_cash_amount,omitempty"`
}

// dividends lists the dividends with an ex-date on or after from that are
// declared on or before to. A dividend with an ex-date after to is
// announced but not paid yet; like Massive, it has no adjustment fields.
func (g *generator) dividends(s *series, from, to time.Time) error {
	results := []dividendResult{}
	if dc := s.cfg.Dividends; dc != nil {
		var err error
		if results, err = schedule(s, dc, from, to); err != nil {
			return err
		}
	}
	path := "dividends/" + s.cfg.Symbol + ".json"
	g.writeJSON(path, struct {
		Status    string           `json:"status"`
		RequestID string           `json:"request_id"`
		Results   []dividendResult `json:"results"`
	}{"OK", g.requestID(path), results})
	return nil
}

func schedule(s *series, dc *dividendConfig, from, to time.Time) ([]dividendResult, error) {
	if len(dc.ExDates) == 0 || dc.FirstAmount <= 0 {
		return nil, errors.New("want ex_dates and a positive first_amount")
	}
	results := []dividendResult{}
	var adjAmounts []float64
	for year := from.Year(); year <= to.Year()+1; year++ {
		amount := round(dc.FirstAmount*math.Pow(1+dc.AnnualRaise, float64(year-from.Year())), 2)
		for _, md := range dc.ExDates {
			ex, err := parseDay(fmt.Sprintf("%d-%s", year, md))
			if err != nil {
				return nil, fmt.Errorf("ex-date %q: %w", md, err)
			}
			ex = nextTradingDay(ex)
			declared := ex.AddDate(0, 0, -dc.DeclaredDaysBefore)
			if dc.DeclaredYearlyOn != "" {
				if declared, err = parseDay(fmt.Sprintf("%d-%s", year, dc.DeclaredYearlyOn)); err != nil {
					return nil, fmt.Errorf("declared_yearly_on: %w", err)
				}
			}
			if declared.After(ex) {
				return nil, fmt.Errorf("the dividend of %s is declared after its ex-date", ex.Format(time.DateOnly))
			}
			if ex.Before(from) || declared.After(to) {
				continue
			}
			pay := nextTradingDay(ex.AddDate(0, 0, dc.PayDaysAfter))
			date := ex.Format(time.DateOnly)
			r := dividendResult{
				ID: eventID(s.cfg.Symbol, date, "dividend"), Ticker: s.cfg.Symbol,
				RecordDate: date, PayDate: pay.Format(time.DateOnly), DeclarationDate: declared.Format(time.DateOnly),
				ExDividendDate: date, Frequency: len(dc.ExDates), CashAmount: amount, Currency: "USD", DistributionType: "recurring",
			}
			adj := 0.0
			if !ex.After(to) {
				f := 1.0
				for _, sp := range s.splits {
					if ex.Before(sp.date) {
						f *= sp.to / sp.from
					}
				}
				adj = round(amount/f, 6)
				r.SplitAdjusted = &adj
			}
			results = append(results, r)
			adjAmounts = append(adjAmounts, adj)
		}
	}
	slices.SortStableFunc(results, func(a, b dividendResult) int { return cmp.Compare(a.ExDividendDate, b.ExDividendDate) })

	// The historical factor of a dividend multiplies the price drops of it
	// and every later paid dividend, each relative to the close the day
	// before its ex-date.
	hist := 1.0
	for i := len(results) - 1; i >= 0; i-- {
		r := &results[i]
		if r.SplitAdjusted == nil {
			continue
		}
		ex, _ := parseDay(r.ExDividendDate) //nolint:errcheck // the date was formatted above
		if prev, ok := s.closeBefore(ex); ok {
			hist *= 1 - adjAmounts[i]/prev
		}
		h := round(hist, 6)
		r.Historical = &h
	}
	return results, nil
}

// closeBefore returns the split-adjusted close of the last trading day
// before d.
func (s *series) closeBefore(d time.Time) (float64, bool) {
	for p := d.AddDate(0, 0, -1); !p.Before(d.AddDate(0, 0, -10)); p = p.AddDate(0, 0, -1) {
		if c, ok := s.adjClose[p]; ok {
			return c, true
		}
	}
	return 0, false
}

type tickerResult struct {
	Ticker          string `json:"ticker"`
	Name            string `json:"name"`
	Market          string `json:"market"`
	Locale          string `json:"locale"`
	PrimaryExchange string `json:"primary_exchange"`
	Type            string `json:"type"`
	Active          bool   `json:"active"`
	CurrencyName    string `json:"currency_name"`
	CIK             string `json:"cik,omitempty"`
	CompositeFIGI   string `json:"composite_figi"`
	ShareClassFIGI  string `json:"share_class_figi"`
	LastUpdatedUTC  string `json:"last_updated_utc"`
}

func newTickerResult(tc *tickerConfig, to time.Time) tickerResult {
	r := tickerResult{
		Ticker: tc.Symbol, Name: tc.Name, Market: "stocks", Locale: "us",
		PrimaryExchange: tc.Exchange, Type: tc.Type, Active: true, CurrencyName: "usd",
		CompositeFIGI:  syntheticFIGI(tc.Symbol, "composite"),
		ShareClassFIGI: syntheticFIGI(tc.Symbol, "share class"),
		LastUpdatedUTC: to.Format(time.DateOnly) + "T06:00:00Z",
	}
	if tc.CIK > 0 {
		r.CIK = fmt.Sprintf("%010d", tc.CIK)
	}
	return r
}

// syntheticFIGI returns a 12-character identifier in the form of a FIGI.
// Its "FX" marks it as fixture data.
func syntheticFIGI(symbol, kind string) string {
	sum := sha256.Sum256([]byte(symbol + "|" + kind))
	return "BBGFX" + strings.ToUpper(hex.EncodeToString(sum[:4]))[:7]
}

func (g *generator) overview(tc *tickerConfig, to time.Time) {
	path := "ticker/" + tc.Symbol + ".json"
	g.writeJSON(path, struct {
		RequestID string       `json:"request_id"`
		Results   tickerResult `json:"results"`
		Status    string       `json:"status"`
	}{g.requestID(path), newTickerResult(tc, to), "OK"})
}

func (g *generator) tickerPages(tickers []tickerConfig, pageSize int, to time.Time) {
	if pageSize <= 0 {
		pageSize = len(tickers)
	}
	for page, chunk := 1, tickers; len(chunk) > 0; page++ {
		size := min(pageSize, len(chunk))
		results := make([]tickerResult, 0, size)
		for i := range chunk[:size] {
			results = append(results, newTickerResult(&chunk[i], to))
		}
		chunk = chunk[size:]
		next := ""
		if len(chunk) > 0 {
			next = fmt.Sprintf("%s%d", nextURLBase, page+1)
		}
		path := fmt.Sprintf("tickers/page-%d.json", page)
		g.writeJSON(path, struct {
			Results   []tickerResult `json:"results"`
			Status    string         `json:"status"`
			RequestID string         `json:"request_id"`
			Count     int            `json:"count"`
			NextURL   string         `json:"next_url,omitempty"`
		}{results, "OK", g.requestID(path), len(results), next})
	}
}

func (g *generator) writeJSON(path string, v any) {
	data, _ := json.MarshalIndent(v, "", "  ") //nolint:errchkjson // the documents hold only strings and finite numbers
	g.files[path] = append(data, '\n')
}

func parseDay(s string) (time.Time, error) {
	return time.Parse(time.DateOnly, s)
}

func nextTradingDay(d time.Time) time.Time {
	for !isTradingDay(d) {
		d = d.AddDate(0, 0, 1)
	}
	return d
}

func round(x float64, decimals int) float64 {
	p := math.Pow10(decimals)
	return math.Round(x*p) / p
}
