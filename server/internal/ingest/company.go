package ingest

import (
	"context"
	"errors"
	"fmt"
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
// symbol, its daily bars for the provider's history window, and the
// derived quarters. It returns an error that wraps ErrUnknownSymbol when
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
// first.
func (r *Runner) lookupSymbol(ctx context.Context, symbol string) (domain.Ticker, error) {
	t, err := r.store.GetCurrentTicker(ctx, symbol)
	if !errors.Is(err, store.ErrNotFound) {
		return t, err
	}
	last, err := r.store.GetLastSucceededIngestRun(ctx, JobSECTickers)
	switch {
	case errors.Is(err, store.ErrNotFound):
	case err != nil:
		return domain.Ticker{}, err
	case !domain.DateOf(last.StartedAt.UTC()).Before(r.today()):
		return domain.Ticker{}, fmt.Errorf("%q: %w", symbol, ErrUnknownSymbol)
	}
	if _, err := r.SECTickers(ctx); err != nil && !errors.Is(err, ErrAlreadyRunning) {
		return domain.Ticker{}, err
	}
	t, err = r.store.GetCurrentTicker(ctx, symbol)
	if errors.Is(err, store.ErrNotFound) {
		return domain.Ticker{}, fmt.Errorf("%q: %w", symbol, ErrUnknownSymbol)
	}
	return t, err
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
	ref, err := r.symbolReference(ctx, t.Symbol)
	if err != nil {
		return res, fmt.Errorf("price reference of %s: %w", t.Symbol, err)
	}
	res.Splits, res.Dividends = ref.Splits, ref.Dividends
	if res.Bars, err = r.symbolBars(ctx, t.Symbol); err != nil {
		return res, err
	}
	res.Fundamentals, err = r.rebuild(ctx, t.CIK)
	return res, err
}
