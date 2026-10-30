// Package prices defines the price provider that the rest of PricedIn uses,
// and the helpers that do not depend on a provider: the US trading calendar
// and split adjustment. Code outside a provider's own package uses Provider
// and never imports the provider's package, so that a change of provider
// stays local.
package prices

import (
	"context"
	"errors"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// Provider gives daily prices, split events, cash dividends and the ticker
// list of US stocks. Every price is raw: not adjusted for splits or
// dividends. Use AdjustForSplits with the provider's splits to adjust.
//
// No method returns a bar dated after LastFinishedSession of the current
// time. GroupedDaily for a later date returns an empty slice, and DailyBars
// leaves out the part of its range after it.
//
// GroupedDaily for a day without trading, such as a market holiday or a
// weekend, returns an empty slice and no error. A call for data outside what
// the provider's plan covers, such as history that is too old, returns an
// error that wraps ErrNotAuthorized. DailyBars for a range that starts
// before the plan's history and ends inside it returns the bars that the
// plan covers. Each method returns its results sorted by date, or by symbol
// for GroupedDaily and Tickers.
type Provider interface {
	// Name identifies the provider. It is the Source of every value the
	// provider returns.
	Name() string
	// GroupedDaily returns one bar for each US stock that traded on date.
	GroupedDaily(ctx context.Context, date domain.Date) ([]domain.PriceBar, error)
	// DailyBars returns the bars of one symbol from from to to, both
	// included.
	DailyBars(ctx context.Context, symbol string, from, to domain.Date) ([]domain.PriceBar, error)
	// Splits returns every split event of symbol that the provider knows.
	Splits(ctx context.Context, symbol string) ([]domain.Split, error)
	// Dividends returns every cash dividend of symbol that the provider
	// knows, including those announced with an ex-dividend date in the
	// future.
	Dividends(ctx context.Context, symbol string) ([]domain.Dividend, error)
	// Tickers returns the active US stock tickers.
	Tickers(ctx context.Context) ([]ProviderTicker, error)
}

// ProviderTicker is a ticker as the provider lists it.
type ProviderTicker struct {
	Symbol string
	Name   string
	// CIK is 0 when the provider does not know the company's CIK, such as
	// for most funds.
	CIK domain.CIK
	// Exchange is the ISO 10383 market identifier code of the primary
	// exchange, such as "XNYS" or "XNAS". It can be empty.
	Exchange string
	// Type is the provider's security type, such as "CS" for common stock
	// or "ETF".
	Type string
	// CompositeFIGI identifies the security across symbol changes. It can be
	// empty.
	CompositeFIGI string
	Source        string
}

// SessionOverHour is the hour in New York from which a provider counts the
// session of that day as over, four hours after the 16:00 close.
const SessionOverHour = 20

// ErrNotAuthorized means that the provider's plan does not cover the
// requested data, such as history older than the plan allows.
var ErrNotAuthorized = errors.New("prices: not authorized for this data")
