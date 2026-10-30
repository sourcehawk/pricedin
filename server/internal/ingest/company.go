package ingest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/store"
)

// CompanyResult counts what loading one company wrote.
type CompanyResult struct {
	Symbol string
	CIK    domain.CIK
	SEC    SECCompanyResult
	// Splits and Dividends count the provider events added or changed, and
	// Bars the daily bars.
	Splits, Dividends, Bars int64
	Fundamentals            FundamentalsResult
}

// Stats implements Result.
func (c CompanyResult) Stats() map[string]int64 {
	return map[string]int64{
		"cik": int64(c.CIK), "filings": c.SEC.Filings, "facts": c.SEC.Facts,
		"splits": c.Splits, "dividends": c.Dividends, "bars": c.Bars,
		"quarters": c.Fundamentals.Quarters, "values": c.Fundamentals.Values,
		"conflicts": c.Fundamentals.Conflicts,
	}
}

// LoadCompany loads one company on demand, as the API does the first time
// its owner opens a company. It has the method shape of the API's
// CompanyLoader; LoadCompanyResult also returns the counts.
func (r *Runner) LoadCompany(ctx context.Context, symbol string) error {
	_, err := r.LoadCompanyResult(ctx, symbol)
	return err
}

// LoadCompanyResult loads, in this order, the SEC submissions and facts of
// the company that symbol maps to, the provider's splits and dividends of
// symbol and of the company's other current share classes, their daily
// bars for the provider's history window, and the derived quarters. It returns an error that wraps ErrUnknownSymbol when
// the symbol is not in the ticker map, after it fetched a fresh copy of the
// SEC map if today's was not loaded yet. A second load of the same company
// waits for the first and then changes nothing, so calling it again is
// safe; later loads fetch only the bars that are missing.
func (r *Runner) LoadCompanyResult(ctx context.Context, symbol string) (CompanyResult, error) {
	if err := r.needSEC(JobCompany); err != nil {
		return CompanyResult{}, err
	}
	if err := r.needPrices(JobCompany); err != nil {
		return CompanyResult{}, err
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	t, err := r.lookupSymbol(ctx, symbol)
	if err != nil {
		return CompanyResult{}, err
	}
	return runJob(ctx, r, JobCompany, JobCompany+":"+t.CIK.String(), true, func(ctx context.Context) (CompanyResult, error) {
		return r.loadCompany(ctx, t)
	})
}

// lookupSymbol finds the current mapping of symbol. When the stored ticker
// map lacks it and the SEC map was not loaded today, it loads the SEC map
// first, or waits for a load that another runner started.
func (r *Runner) lookupSymbol(ctx context.Context, symbol string) (domain.Ticker, error) {
	t, err := r.store.GetCurrentTicker(ctx, symbol)
	if !errors.Is(err, store.ErrNotFound) {
		return t, err
	}
	fresh, err := r.tickersLoadedToday(ctx)
	if err != nil {
		return domain.Ticker{}, err
	}
	if !fresh {
		_, err := runJob(ctx, r, JobSECTickers, JobSECTickers, true, func(ctx context.Context) (SECTickersResult, error) {
			// The load that this one waited for counts too.
			if fresh, err := r.tickersLoadedToday(ctx); err != nil || fresh {
				return SECTickersResult{}, err
			}
			return r.syncSECTickers(ctx)
		})
		if err != nil {
			return domain.Ticker{}, err
		}
	}
	t, err = r.store.GetCurrentTicker(ctx, symbol)
	if errors.Is(err, store.ErrNotFound) {
		return domain.Ticker{}, fmt.Errorf("%q: %w", symbol, ErrUnknownSymbol)
	}
	return t, err
}

// tickersLoadedToday reports whether a sec-tickers run that started today
// succeeded.
func (r *Runner) tickersLoadedToday(ctx context.Context) (bool, error) {
	last, err := r.store.GetLastSucceededIngestRun(ctx, JobSECTickers)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !domain.DateOf(last.StartedAt.UTC()).Before(r.today()), nil
}

func (r *Runner) loadCompany(ctx context.Context, t domain.Ticker) (CompanyResult, error) {
	res := CompanyResult{Symbol: t.Symbol, CIK: t.CIK}
	var err error
	if res.SEC, err = r.loadSECCompany(ctx, t.CIK, true); err != nil {
		return res, err
	}
	if res.SEC.NotFound > 0 {
		return res, fmt.Errorf("company %s: SEC has no submissions for it", t.CIK)
	}
	symbols, err := r.shareClasses(ctx, t)
	if err != nil {
		return res, err
	}
	// The quarters belong to the company, and their split basis is the
	// newest bar of any class, so every class gets its bars up to date.
	for _, symbol := range symbols {
		ref, err := r.symbolReference(ctx, symbol)
		if err != nil {
			return res, fmt.Errorf("price reference of %s: %w", symbol, err)
		}
		res.Splits += ref.Splits
		res.Dividends += ref.Dividends
		bars, err := r.symbolBars(ctx, symbol)
		if err != nil {
			return res, err
		}
		res.Bars += bars
	}
	res.Fundamentals, err = r.rebuild(ctx, t.CIK)
	return res, err
}

// shareClasses returns t's symbol first, then the other current SEC
// symbols of its company, sorted.
func (r *Runner) shareClasses(ctx context.Context, t domain.Ticker) ([]string, error) {
	records, err := r.store.ListTickers(ctx, t.CIK)
	if err != nil {
		return nil, err
	}
	symbols := []string{t.Symbol}
	for _, rec := range records {
		if rec.Source == store.TickerSourceSEC && rec.ValidTo == nil && !slices.Contains(symbols, rec.Symbol) {
			symbols = append(symbols, rec.Symbol)
		}
	}
	return symbols, nil
}
