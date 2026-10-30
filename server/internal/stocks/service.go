// Package stocks is the service layer of the HTTP API. It reads a stock's
// stored filings, facts, prices, splits and dividends, builds the engine
// History from them, and attaches the source and as-of date of every
// number it returns. Per-share values and closes are split-adjusted to
// today's share count.
package stocks

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // the New York zone must load on any host

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/fundamentals"
	"github.com/sourcehawk/pricedin/server/internal/prices"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

// Store is the part of *store.Store that the service reads.
type Store interface {
	GetCurrentTicker(ctx context.Context, symbol string) (domain.Ticker, error)
	ListSymbolTickers(ctx context.Context, symbol string) ([]store.TickerRecord, error)
	GetCompany(ctx context.Context, cik domain.CIK) (domain.Company, error)
	HasQuarterlyFundamentals(ctx context.Context, cik domain.CIK) (bool, error)
	ListFacts(ctx context.Context, cik domain.CIK, q store.FactQuery) ([]domain.Fact, error)
	GetLatestFiled(ctx context.Context, cik domain.CIK) (domain.Date, error)
	ListFilings(ctx context.Context, cik domain.CIK) ([]domain.Filing, error)
	ListPriceBars(ctx context.Context, symbol string, from, to domain.Date) ([]domain.PriceBar, error)
	ListPriceSplits(ctx context.Context, symbol string) ([]domain.Split, error)
	ListDividends(ctx context.Context, symbol string) ([]domain.Dividend, error)
	SearchTickers(ctx context.Context, query string, limit int) ([]store.TickerMatch, error)
	ListLatestIngestRuns(ctx context.Context) ([]store.IngestRun, error)
	GetLatestPriceDateOverall(ctx context.Context) (domain.Date, error)
	GetCoverage(ctx context.Context) (store.Coverage, error)
}

// CompanyLoader loads or refreshes one company's filings, facts, prices,
// splits and dividends into the store. The ingestion jobs implement it.
type CompanyLoader interface {
	LoadCompany(ctx context.Context, symbol string) error
}

// The errors of the service. Every other error is a failure of the store.
var (
	// ErrInvalidTicker means that a ticker is not 1 to 10 letters, digits,
	// dots or dashes, starting with a letter or digit.
	ErrInvalidTicker = errors.New("stocks: invalid ticker")
	// ErrUnknownTicker means that no company has the ticker now.
	ErrUnknownTicker = errors.New("stocks: unknown ticker")
	// ErrNotLoaded means that the company is known but its fundamentals
	// are not loaded yet; Load can load them.
	ErrNotLoaded = errors.New("stocks: company data not loaded")
	// ErrLoaderUnavailable means that this build has no CompanyLoader.
	ErrLoaderUnavailable = errors.New("stocks: loading a company is not available in this build")
)

// MissingInputError means that an evaluation input was not given and the
// stock has no stored value for it.
type MissingInputError struct {
	Input string // the lever name: "price" or "ttmEps"
}

func (e *MissingInputError) Error() string {
	return fmt.Sprintf("stocks: no stored %s for this stock; send one", e.Input)
}

// UnavailableLoader is the CompanyLoader of a build without ingestion. It
// always returns ErrLoaderUnavailable.
type UnavailableLoader struct{}

// LoadCompany returns ErrLoaderUnavailable.
func (UnavailableLoader) LoadCompany(context.Context, string) error {
	return ErrLoaderUnavailable
}

// Options configure a Service.
type Options struct {
	// Now returns the current time; nil means time.Now. Today is its date
	// in New York.
	Now func() time.Time
	// Loader loads a company on demand; nil means UnavailableLoader.
	Loader CompanyLoader
}

// Service is safe for concurrent use.
type Service struct {
	store   Store
	now     func() time.Time
	loader  CompanyLoader
	newYork *time.Location
}

// New returns a Service over st.
func New(st Store, opts Options) *Service {
	s := &Service{store: st, now: opts.Now, loader: opts.Loader}
	if s.now == nil {
		s.now = time.Now
	}
	if s.loader == nil {
		s.loader = UnavailableLoader{}
	}
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		loc = time.UTC // unreachable: time/tzdata is embedded
	}
	s.newYork = loc
	return s
}

var tickerPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.\-]{0,9}$`)

// earliest is the lower bound of a price query without a bound.
var earliest = domain.NewDate(1900, 1, 1)

// maxCloseLag is the most days a quarter-end close may lie before the end
// of the quarter, such as over a long weekend.
const maxCloseLag = 7

// stock is what one request knows about a stock.
type stock struct {
	ticker  domain.Ticker
	company domain.Company
	loaded  bool
	today   domain.Date
	// symbol is the price provider's symbol of the ticker.
	symbol string
	// bars, splits and dividends are the provider's, from the day the
	// ticker came to this company.
	bars      []prices.AdjustedBar
	splits    []domain.Split
	dividends []domain.Dividend
	// The fundamentals; set by withFundamentals.
	series      fundamentals.Series
	filings     map[string]domain.Filing
	latestFiled *domain.Date
}

// open reads the ticker, the company and the provider's data.
func (s *Service) open(ctx context.Context, ticker string) (*stock, error) {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	if !tickerPattern.MatchString(ticker) {
		return nil, ErrInvalidTicker
	}
	t, err := s.store.GetCurrentTicker(ctx, ticker)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrUnknownTicker
	}
	if err != nil {
		return nil, fmt.Errorf("stocks: %s: %w", ticker, err)
	}
	st := &stock{ticker: t, today: domain.DateOf(s.now().In(s.newYork)), symbol: priceSymbol(t.Symbol)}
	if st.company, err = s.store.GetCompany(ctx, t.CIK); err != nil {
		return nil, fmt.Errorf("stocks: %s: %w", ticker, err)
	}
	if st.loaded, err = s.store.HasQuarterlyFundamentals(ctx, t.CIK); err != nil {
		return nil, fmt.Errorf("stocks: %s: %w", ticker, err)
	}
	if err := s.readPrices(ctx, st); err != nil {
		return nil, fmt.Errorf("stocks: %s: %w", ticker, err)
	}
	if latest, err := s.store.GetLatestFiled(ctx, t.CIK); err == nil {
		st.latestFiled = &latest
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("stocks: %s: %w", ticker, err)
	}
	return st, nil
}

// readPrices reads the bars, splits and dividends of the stock's symbol
// from the day the symbol came to the company: a symbol that belonged to
// another company before carries that company's history too.
func (s *Service) readPrices(ctx context.Context, st *stock) error {
	rows, err := s.store.ListSymbolTickers(ctx, st.ticker.Symbol)
	if err != nil {
		return err
	}
	from := earliest
	for _, r := range rows {
		if r.CIK != st.ticker.CIK && r.ValidTo != nil && r.ValidTo.After(from) {
			from = *r.ValidTo
		}
	}
	since := func(d domain.Date) bool { return !d.Before(from) && !d.After(st.today) }

	splits, err := s.store.ListPriceSplits(ctx, st.symbol)
	if err != nil {
		return err
	}
	for _, sp := range splits {
		if since(sp.Date) && sp.From > 0 && sp.To > 0 {
			sp.CIK = st.ticker.CIK
			st.splits = append(st.splits, sp)
		}
	}
	bars, err := s.store.ListPriceBars(ctx, st.symbol, from, st.today)
	if err != nil {
		return err
	}
	bars = slices.DeleteFunc(bars, func(b domain.PriceBar) bool { return !(b.Close > 0) })
	if st.bars, err = prices.AdjustForSplits(bars, st.splits, st.today); err != nil {
		return err
	}
	dividends, err := s.store.ListDividends(ctx, st.symbol)
	if err != nil {
		return err
	}
	for _, d := range dividends {
		if since(d.ExDate) {
			st.dividends = append(st.dividends, d)
		}
	}
	return nil
}

// withFundamentals derives the stock's quarters from its stored facts,
// with the provider's splits.
func (s *Service) withFundamentals(ctx context.Context, st *stock) error {
	if !st.loaded {
		return ErrNotLoaded
	}
	facts, err := s.store.ListFacts(ctx, st.ticker.CIK, store.FactQuery{})
	if err != nil {
		return fmt.Errorf("stocks: %s: %w", st.ticker.Symbol, err)
	}
	filings, err := s.store.ListFilings(ctx, st.ticker.CIK)
	if err != nil {
		return fmt.Errorf("stocks: %s: %w", st.ticker.Symbol, err)
	}
	st.filings = make(map[string]domain.Filing, len(filings))
	for _, f := range filings {
		st.filings[f.Accession] = f
	}
	// Bars come from the provider, so its splits were fetched with them.
	opts := fundamentals.Options{Splits: st.splits, ProviderChecked: len(st.bars) > 0}
	if st.series, err = fundamentals.Derive(st.ticker.CIK, facts, opts); err != nil {
		return fmt.Errorf("stocks: %s: %w", st.ticker.Symbol, err)
	}
	return nil
}

// priceSymbol maps an SEC ticker to the provider's symbol: SEC writes a
// share class with a dash (BRK-B), the provider with a dot (BRK.B).
func priceSymbol(secSymbol string) string {
	return strings.ReplaceAll(secSymbol, "-", ".")
}

// lastBar returns the newest bar, or false without bars.
func (st *stock) lastBar() (prices.AdjustedBar, bool) {
	if len(st.bars) == 0 {
		return prices.AdjustedBar{}, false
	}
	return st.bars[len(st.bars)-1], true
}

// closeOn returns the last bar on or before day, if it is at most
// maxCloseLag days earlier.
func (st *stock) closeOn(day domain.Date) (prices.AdjustedBar, bool) {
	i, found := slices.BinarySearchFunc(st.bars, day, func(b prices.AdjustedBar, d domain.Date) int {
		return b.Bar.Date.Time().Compare(d.Time())
	})
	if !found {
		i--
	}
	if i < 0 || day.AddDays(-maxCloseLag).After(st.bars[i].Bar.Date) {
		return prices.AdjustedBar{}, false
	}
	return st.bars[i], true
}

func priceSource(b prices.AdjustedBar) PriceSource {
	return PriceSource{Provider: b.Bar.Source, Symbol: b.Bar.Symbol, CloseDate: b.Bar.Date, RawClose: b.Bar.Close, SplitFactor: b.Factor}
}

// latestClose is the newest split-adjusted close, or nil without bars.
func (st *stock) latestClose() *SourcedNumber {
	b, ok := st.lastBar()
	if !ok {
		return nil
	}
	return &SourcedNumber{Value: b.Close, Source: priceSource(b), AsOf: b.Bar.Date, AsOfLabel: "as of close"}
}

// ttmEPS is the newest TTM diluted EPS known today, or nil.
func (st *stock) ttmEPS() *SourcedNumber {
	at, v, ok := fundamentals.TTMKnownOn(st.series, fundamentals.EPSDiluted, st.today)
	if !ok {
		return nil
	}
	src := DerivedSource{Method: string(fundamentals.MethodTTM), Estimate: v.Estimate, Label: st.series.Quarters[at].Label}
	for _, q := range st.series.Quarters[at-3 : at+1] {
		src.Inputs = append(src.Inputs, valueSource(st.ticker.CIK, q.Values[fundamentals.EPSDiluted], st.filings))
	}
	return &SourcedNumber{Value: v.V, Source: src, AsOf: v.Filed}
}

// ttmDividend adds the recurring dividends in US dollars with an ex-date
// in the twelve months up to today, split-adjusted. Its as-of date is
// today.
func (st *stock) ttmDividend() SourcedNumber {
	start := domain.DateOf(st.today.Time().AddDate(-1, 0, 0))
	src := DerivedSource{Method: "recurring_dividends_12m"}
	total := 0.0
	for _, d := range st.dividends {
		if d.Type != "recurring" || !d.ExDate.After(start) || d.ExDate.After(st.today) || (d.Currency != "" && d.Currency != "USD") {
			continue
		}
		ratio := 1.0
		for _, sp := range st.splits {
			if d.ExDate.Before(sp.Date) {
				ratio *= sp.Ratio()
			}
		}
		total += d.CashAmount / ratio
		src.Inputs = append(src.Inputs, DividendSource{
			Provider: d.Source, Symbol: d.Symbol, ExDate: d.ExDate, PayDate: d.PayDate,
			Amount: d.CashAmount, SplitRatio: ratio, DividendType: d.Type,
		})
	}
	return SourcedNumber{Value: total, Source: src, AsOf: st.today}
}

func laterDate(a, b domain.Date) domain.Date {
	if b.After(a) {
		return b
	}
	return a
}
