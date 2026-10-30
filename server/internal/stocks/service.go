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
	"log/slog"
	"math"
	"regexp"
	"slices"
	"strings"
	"sync"
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
	GetDividendsLoadedAt(ctx context.Context, symbol string) (time.Time, error)
	GetLatestSucceededIngestRun(ctx context.Context, job string) (store.IngestRun, error)
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
	// Logger receives a warning for each stored value outside the engine's
	// bounds; nil discards them.
	Logger *slog.Logger
}

// Service is safe for concurrent use.
type Service struct {
	store   Store
	now     func() time.Time
	loader  CompanyLoader
	logger  *slog.Logger
	newYork *time.Location
	// warned holds "ticker|field" for each out-of-bounds warning given,
	// so that each is logged once per process.
	warned sync.Map
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
	s.logger = opts.Logger
	if s.logger == nil {
		s.logger = slog.New(slog.DiscardHandler)
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
	svc     *Service
	loaded  bool
	today   domain.Date
	// rawBars and dividends are the provider's, from the day the ticker
	// came to this company.
	rawBars   []domain.PriceBar
	dividends []domain.Dividend
	// dividendsFetched is the day the dividend data was last fetched, nil
	// when it never was.
	dividendsFetched *domain.Date
	// splits are the splits in effect: the provider's, and after
	// withFundamentals the confirmed ones, which add the splits that only
	// the filings give. bars are rawBars adjusted for them.
	splits []domain.Split
	bars   []prices.AdjustedBar
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
	st := &stock{
		ticker: t, today: domain.DateOf(s.now().In(s.newYork)),
		svc: s,
	}
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
	from := earliest
	rows, err := s.store.ListSymbolTickers(ctx, st.ticker.Symbol)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.CIK != st.ticker.CIK && r.ValidTo != nil && r.ValidTo.After(from) {
			from = *r.ValidTo
		}
	}
	since := func(d domain.Date) bool { return !d.Before(from) && !d.After(st.today) }

	splits, err := s.store.ListPriceSplits(ctx, st.ticker.Symbol)
	if err != nil {
		return err
	}
	var provider []domain.Split
	for _, sp := range splits {
		if since(sp.Date) {
			sp.CIK = st.ticker.CIK
			provider = append(provider, sp)
		}
	}
	bars, err := s.store.ListPriceBars(ctx, st.ticker.Symbol, from, st.today)
	if err != nil {
		return err
	}
	st.rawBars = slices.DeleteFunc(bars, func(b domain.PriceBar) bool { return !(b.Close > 0) })
	if err := st.applySplits(provider); err != nil {
		return err
	}
	dividends, err := s.store.ListDividends(ctx, st.ticker.Symbol)
	if err != nil {
		return err
	}
	for i := range dividends {
		if since(dividends[i].ExDate) {
			st.dividends = append(st.dividends, dividends[i])
		}
	}
	return s.readDividendsFetched(ctx, st)
}

// priceReferenceJob is the ingestion job that fetches the provider's
// splits and dividends of every symbol.
const priceReferenceJob = "price-reference"

// readDividendsFetched sets the day the dividends were last fetched, or
// leaves it nil when they never were.
func (s *Service) readDividendsFetched(ctx context.Context, st *stock) error {
	var at time.Time
	// A dividend's load time moves only when the dividend changes, and a
	// price-reference run fetches every symbol's dividends, also of a
	// symbol that pays none; the later of the two is the last fetch.
	changed, err := s.store.GetDividendsLoadedAt(ctx, st.ticker.Symbol)
	switch {
	case err == nil:
		at = changed
	case !errors.Is(err, store.ErrNotFound):
		return err
	}
	run, err := s.store.GetLatestSucceededIngestRun(ctx, priceReferenceJob)
	switch {
	case err == nil && run.FinishedAt != nil && run.FinishedAt.After(at):
		at = *run.FinishedAt
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return err
	}
	if at.IsZero() {
		return nil
	}
	day := domain.DateOf(at.In(s.newYork))
	st.dividendsFetched = &day
	return nil
}

// withFundamentals derives the stock's quarters. After it, the closes and
// the TTM dividend follow the same confirmed splits as the EPS.
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
	opts := fundamentals.Options{AsOf: &st.today, Splits: st.splits, ProviderChecked: len(st.rawBars) > 0}
	if st.series, err = fundamentals.Derive(st.ticker.CIK, facts, opts); err != nil {
		return fmt.Errorf("stocks: %s: %w", st.ticker.Symbol, err)
	}
	if err := st.applySplits(st.series.Splits); err != nil {
		return fmt.Errorf("stocks: %s: %w", st.ticker.Symbol, err)
	}
	return nil
}

// applySplits makes splits the splits in effect and adjusts the bars for
// them. Splits from the filings carry no symbol; they get the ticker's.
// It leaves out a bar whose adjusted close is outside the engine's bounds.
func (st *stock) applySplits(splits []domain.Split) error {
	st.splits = make([]domain.Split, 0, len(splits))
	for _, sp := range splits {
		if sp.From > 0 && sp.To > 0 && !sp.Date.After(st.today) {
			sp.Symbol = st.ticker.Symbol
			st.splits = append(st.splits, sp)
		}
	}
	bars, err := prices.AdjustForSplits(st.rawBars, st.splits, st.today)
	if err != nil {
		return err
	}
	st.bars = slices.DeleteFunc(bars, func(b prices.AdjustedBar) bool {
		if closeOK(b.Close) {
			return false
		}
		st.warnOutOfBounds("close", slog.String("date", b.Bar.Date.String()), slog.Float64("value", b.Close))
		return true
	})
	return nil
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

// warnOutOfBounds logs that a stored value of field is outside the
// engine's bounds and left out, once per ticker and field per process.
func (st *stock) warnOutOfBounds(field string, attrs ...any) {
	if _, seen := st.svc.warned.LoadOrStore(st.ticker.Symbol+"|"+field, true); seen {
		return
	}
	st.svc.logger.Warn("stored value outside the engine's bounds left out",
		append([]any{slog.String("ticker", st.ticker.Symbol), slog.String("field", field)}, attrs...)...)
}

// ttmEPS is the newest TTM diluted EPS known today, or nil. The value is
// always a valid ttmEps lever.
func (st *stock) ttmEPS() *SourcedNumber {
	at, v, ok := fundamentals.TTMKnownOn(st.series, fundamentals.EPSDiluted, st.today)
	if !ok {
		return nil
	}
	v.V = boundedEPS(v.V)
	src := DerivedSource{Method: string(fundamentals.MethodTTM), Estimate: v.Estimate, Label: st.series.Quarters[at].Label}
	for _, q := range st.series.Quarters[at-3 : at+1] {
		src.Inputs = append(src.Inputs, valueSource(st.ticker.CIK, q.Values[fundamentals.EPSDiluted], st.filings))
	}
	return &SourcedNumber{Value: v.V, Source: src, AsOf: v.Filed}
}

// ttmDividend adds the recurring dividends in US dollars with an ex-date
// in the twelve months up to today, split-adjusted. Its as-of date can be
// earlier than today. It is missing with ReasonNoDividendData when no
// dividend of the symbol is stored and no price-reference run has
// succeeded.
func (st *stock) ttmDividend() MaybeSourced {
	if st.dividendsFetched == nil {
		return MaybeSourced{Reason: ReasonNoDividendData}
	}
	start := domain.DateOf(st.today.Time().AddDate(-1, 0, 0))
	src := DerivedSource{Method: "recurring_dividends_12m"}
	total := 0.0
	for i := range st.dividends {
		d := &st.dividends[i]
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
	// The lever's bound; no real dividend comes near it.
	return MaybeSourced{Number: &SourcedNumber{Value: math.Min(total, maxPerShare), Source: src, AsOf: *st.dividendsFetched}}
}

// boundedEPS returns a TTM EPS within the engine's bounds: 0 for a value
// smaller in size than the smallest EPS, or not a number, which the engine
// reports as needs_positive_eps, and the largest EPS of the same sign for a
// larger one.
func boundedEPS(v float64) float64 {
	switch a := math.Abs(v); {
	case !(a >= minPerShare):
		return 0
	case a > maxPerShare:
		return math.Copysign(maxPerShare, v)
	default:
		return v
	}
}

func laterDate(a, b domain.Date) domain.Date {
	if b.After(a) {
		return b
	}
	return a
}
