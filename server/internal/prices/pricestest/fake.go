// Package pricestest provides an in-memory prices.Provider for the tests of
// other packages.
package pricestest

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/prices"
)

// Provider is a prices.Provider that answers from its fields. Set the fields
// before the first call and do not change them while calls run. Results are
// copies, sorted as prices.Provider promises.
type Provider struct {
	// ProviderName is what Name returns; "fake" when empty.
	ProviderName   string
	Bars           []domain.PriceBar
	SplitEvents    []domain.Split
	DividendEvents []domain.Dividend
	TickerList     []prices.ProviderTicker
	// AuthorizedFrom, when not zero, imitates a plan with limited history:
	// GroupedDaily for an earlier date and DailyBars for a range that ends
	// earlier return prices.ErrNotAuthorized, and DailyBars leaves out the
	// earlier part of a range that starts earlier.
	AuthorizedFrom domain.Date
	// Err, when not nil, is the error of every call.
	Err error
	// Now gives the current time for the rule that no bar of a session
	// that is not over is returned. Default time.Now.
	Now func() time.Time
}

// Name returns ProviderName, or "fake" when it is empty.
func (p *Provider) Name() string {
	if p.ProviderName == "" {
		return "fake"
	}
	return p.ProviderName
}

// GroupedDaily returns the bars dated date, sorted by symbol.
func (p *Provider) GroupedDaily(ctx context.Context, date domain.Date) ([]domain.PriceBar, error) {
	if err := p.check(ctx); err != nil {
		return nil, err
	}
	if p.unauthorized(date) {
		return nil, fmt.Errorf("pricestest: grouped daily %s: %w", date, prices.ErrNotAuthorized)
	}
	last := p.lastFinishedSession()
	bars := filter(p.Bars, func(b domain.PriceBar) bool { return b.Date == date && !b.Date.After(last) })
	slices.SortFunc(bars, func(a, b domain.PriceBar) int { return cmp.Compare(a.Symbol, b.Symbol) })
	return bars, nil
}

// DailyBars returns the bars of symbol from from to to, sorted by date.
func (p *Provider) DailyBars(ctx context.Context, symbol string, from, to domain.Date) ([]domain.PriceBar, error) {
	if err := p.check(ctx); err != nil {
		return nil, err
	}
	if p.unauthorized(to) {
		return nil, fmt.Errorf("pricestest: daily bars %s %s to %s: %w", symbol, from, to, prices.ErrNotAuthorized)
	}
	last := p.lastFinishedSession()
	bars := filter(p.Bars, func(b domain.PriceBar) bool {
		return b.Symbol == symbol && !b.Date.Before(from) && !b.Date.After(to) && !b.Date.After(last) && !p.unauthorized(b.Date)
	})
	slices.SortFunc(bars, func(a, b domain.PriceBar) int { return a.Date.Time().Compare(b.Date.Time()) })
	return bars, nil
}

// Splits returns the split events of symbol, sorted by date.
func (p *Provider) Splits(ctx context.Context, symbol string) ([]domain.Split, error) {
	if err := p.check(ctx); err != nil {
		return nil, err
	}
	splits := filter(p.SplitEvents, func(s domain.Split) bool { return s.Symbol == symbol })
	slices.SortFunc(splits, func(a, b domain.Split) int { return a.Date.Time().Compare(b.Date.Time()) })
	return splits, nil
}

// Dividends returns the dividends of symbol, sorted by ex-dividend date.
func (p *Provider) Dividends(ctx context.Context, symbol string) ([]domain.Dividend, error) {
	if err := p.check(ctx); err != nil {
		return nil, err
	}
	divs := filter(p.DividendEvents, func(d domain.Dividend) bool { return d.Symbol == symbol })
	for i := range divs {
		divs[i].PayDate = copyDate(divs[i].PayDate)
		divs[i].DeclarationDate = copyDate(divs[i].DeclarationDate)
	}
	slices.SortFunc(divs, func(a, b domain.Dividend) int { return a.ExDate.Time().Compare(b.ExDate.Time()) })
	return divs, nil
}

// Tickers returns every ticker in TickerList, sorted by symbol.
func (p *Provider) Tickers(ctx context.Context) ([]prices.ProviderTicker, error) {
	if err := p.check(ctx); err != nil {
		return nil, err
	}
	tickers := slices.Clone(p.TickerList)
	slices.SortFunc(tickers, func(a, b prices.ProviderTicker) int { return cmp.Compare(a.Symbol, b.Symbol) })
	return tickers, nil
}

func (p *Provider) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return p.Err
}

func (p *Provider) lastFinishedSession() domain.Date {
	if p.Now == nil {
		return prices.LastFinishedSession(time.Now())
	}
	return prices.LastFinishedSession(p.Now())
}

func (p *Provider) unauthorized(d domain.Date) bool {
	return !p.AuthorizedFrom.IsZero() && d.Before(p.AuthorizedFrom)
}

func copyDate(d *domain.Date) *domain.Date {
	if d == nil {
		return nil
	}
	c := *d
	return &c
}

func filter[T any](items []T, keep func(T) bool) []T {
	out := []T{}
	for _, item := range items {
		if keep(item) {
			out = append(out, item)
		}
	}
	return out
}
