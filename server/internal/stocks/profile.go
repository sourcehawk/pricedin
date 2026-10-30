package stocks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

// TickerInfo is a trading symbol and its exchange.
type TickerInfo struct {
	Symbol   string `json:"symbol"`
	Exchange string `json:"exchange"`
}

// Company is an SEC filer.
type Company struct {
	CIK            string `json:"cik"` // ten digits with leading zeros
	Name           string `json:"name"`
	SICCode        string `json:"sicCode"`
	SICDescription string `json:"sicDescription"`
	FiscalYearEnd  string `json:"fiscalYearEnd"` // "MMDD"
}

// Freshness gives the newest data stored for a stock.
type Freshness struct {
	LatestFiled *domain.Date `json:"latestFiled"` // newest filing date of a stored fact
	LatestClose *domain.Date `json:"latestClose"` // date of the newest close
}

// Profile is the summary of a stock. A profile of a stock that is not
// loaded has Loaded false and no TTM EPS or P/E; it still has the close and
// the dividend when the provider's data is stored.
type Profile struct {
	Ticker  TickerInfo `json:"ticker"`
	Company Company    `json:"company"`
	Loaded  bool       `json:"loaded"`
	// LatestClose is the newest split-adjusted close, labeled "as of
	// close"; nil without prices.
	LatestClose *SourcedNumber `json:"latestClose"`
	// TTMEPS is the TTM diluted EPS known today; nil when it cannot be
	// derived.
	TTMEPS *SourcedNumber `json:"ttmEps"`
	// TTMDividend adds the recurring dividends with an ex-date in the last
	// twelve months, split-adjusted; zero when there are none, and missing
	// when no dividend data was ever fetched.
	TTMDividend MaybeSourced `json:"ttmDividend"`
	// PE is LatestClose over TTMEPS; nil without both or when TTM EPS is
	// zero or negative.
	PE        *SourcedNumber `json:"pe"`
	Freshness Freshness      `json:"freshness"`
}

// Profile returns the profile of ticker. It gives ErrInvalidTicker or
// ErrUnknownTicker for a ticker it cannot use.
func (s *Service) Profile(ctx context.Context, ticker string) (Profile, error) {
	st, err := s.open(ctx, ticker)
	if err != nil {
		return Profile{}, err
	}
	if st.loaded {
		if err := s.withFundamentals(ctx, st); err != nil {
			return Profile{}, err
		}
	}
	return st.profile(), nil
}

func (st *stock) profile() Profile {
	c := st.company
	p := Profile{
		Ticker:      TickerInfo{Symbol: st.ticker.Symbol, Exchange: st.ticker.Exchange},
		Company:     Company{CIK: c.CIK.String(), Name: c.Name, SICCode: c.SIC, SICDescription: c.SICDescription, FiscalYearEnd: c.FiscalYearEnd},
		Loaded:      st.loaded,
		LatestClose: st.latestClose(),
		TTMDividend: st.ttmDividend(),
		Freshness:   st.freshness(),
	}
	if st.loaded {
		p.TTMEPS = st.ttmEPS()
	}
	if p.LatestClose != nil && p.TTMEPS != nil && p.TTMEPS.Value > 0 {
		p.PE = &SourcedNumber{
			Value:  p.LatestClose.Value / p.TTMEPS.Value,
			Source: DerivedSource{Method: "price_over_ttm_eps", Inputs: []Source{p.LatestClose.Source, p.TTMEPS.Source}},
			AsOf:   laterDate(p.LatestClose.AsOf, p.TTMEPS.AsOf),
		}
	}
	return p
}

func (st *stock) freshness() Freshness {
	f := Freshness{LatestFiled: st.latestFiled}
	if b, ok := st.lastBar(); ok {
		d := b.Bar.Date
		f.LatestClose = &d
	}
	return f
}

// SearchResult is one ticker that matches a search.
type SearchResult struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Exchange string `json:"exchange"`
	CIK      string `json:"cik"`
	Loaded   bool   `json:"loaded"`
}

// The bounds of a search's limit.
const (
	DefaultSearchLimit = 10
	MaxSearchLimit     = 50
)

// Search finds current tickers that start with query, or companies whose
// name contains it. A limit outside 1 to MaxSearchLimit becomes
// DefaultSearchLimit or MaxSearchLimit. A blank query finds nothing.
func (s *Service) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	switch {
	case limit <= 0:
		limit = DefaultSearchLimit
	case limit > MaxSearchLimit:
		limit = MaxSearchLimit
	}
	matches, err := s.store.SearchTickers(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("stocks: search: %w", err)
	}
	out := make([]SearchResult, len(matches))
	for i, m := range matches {
		out[i] = SearchResult{Symbol: m.Ticker.Symbol, Name: m.Name, Exchange: m.Ticker.Exchange, CIK: m.Ticker.CIK.String(), Loaded: m.Loaded}
	}
	return out, nil
}

// Load loads or refreshes the company of ticker with the CompanyLoader and
// returns its new profile. The ticker need not be stored yet. It gives
// ErrInvalidTicker before it calls the loader, ErrLoaderUnavailable without
// one, and the loader's error, which wraps ErrUnknownTicker when no company
// has the ticker.
func (s *Service) Load(ctx context.Context, ticker string) (Profile, error) {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	if !tickerPattern.MatchString(ticker) {
		return Profile{}, ErrInvalidTicker
	}
	if err := s.loader.LoadCompany(ctx, ticker); err != nil {
		return Profile{}, fmt.Errorf("stocks: load %s: %w", ticker, err)
	}
	return s.Profile(ctx, ticker)
}

// IngestRun is the newest run of one ingestion job.
type IngestRun struct {
	ID     int64  `json:"id"`
	Job    string `json:"job"`
	Status string `json:"status"` // running, succeeded or failed
	// Abandoned is set for a run that a process left running when it
	// died, which a later run marked as failed.
	Abandoned  bool             `json:"abandoned"`
	StartedAt  time.Time        `json:"startedAt"`
	FinishedAt *time.Time       `json:"finishedAt"`
	Stats      map[string]int64 `json:"stats"`
	Error      string           `json:"error"`
}

// Coverage counts the stored companies by what is loaded.
type Coverage struct {
	Total            int64 `json:"total"`
	WithFacts        int64 `json:"withFacts"`
	WithFundamentals int64 `json:"withFundamentals"`
}

// SECRetries counts the companies whose SEC load failed: Waiting will be
// tried again, Exhausted failed too often and will not.
type SECRetries struct {
	Waiting   int64 `json:"waiting"`
	Exhausted int64 `json:"exhausted"`
}

// Status is the freshness of the stored data, for the data page.
type Status struct {
	IngestRuns      []IngestRun  `json:"ingestRuns"`
	LatestPriceDate *domain.Date `json:"latestPriceDate"` // nil without prices
	Companies       Coverage     `json:"companies"`
	SECRetries      SECRetries   `json:"secRetries"`
	// TrackedCompanies is the number of companies loaded on demand, which
	// the daily jobs keep current.
	TrackedCompanies int64 `json:"trackedCompanies"`
}

// Status returns the newest run of each job, the newest price date and
// the coverage counts.
func (s *Service) Status(ctx context.Context) (Status, error) {
	runs, err := s.store.ListLatestIngestRuns(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("stocks: status: %w", err)
	}
	out := Status{IngestRuns: make([]IngestRun, len(runs))}
	for i, r := range runs {
		stats := r.Stats
		if stats == nil {
			stats = map[string]int64{}
		}
		out.IngestRuns[i] = IngestRun{
			ID: r.ID, Job: r.Job, Status: string(r.Status), StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, Stats: stats, Error: r.Error,
			// The store marks such a run failed with this error text.
			Abandoned: r.Status == store.RunFailed && r.Error == "abandoned",
		}
	}
	latest, err := s.store.GetLatestPriceDateOverall(ctx)
	switch {
	case err == nil:
		out.LatestPriceDate = &latest
	case !errors.Is(err, store.ErrNotFound):
		return Status{}, fmt.Errorf("stocks: status: %w", err)
	}
	c, err := s.store.GetCoverage(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("stocks: status: %w", err)
	}
	out.Companies = Coverage{Total: c.Companies, WithFacts: c.WithFacts, WithFundamentals: c.WithFundamentals}
	if out.SECRetries.Waiting, out.SECRetries.Exhausted, err = s.store.CountSECRetries(ctx, s.retries); err != nil {
		return Status{}, fmt.Errorf("stocks: status: %w", err)
	}
	tracked, err := s.store.ListTrackedTickers(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("stocks: status: %w", err)
	}
	// A company with several share classes has one ticker for each.
	companies := map[domain.CIK]bool{}
	for _, t := range tracked {
		companies[t.CIK] = true
	}
	out.TrackedCompanies = int64(len(companies))
	return out, nil
}
