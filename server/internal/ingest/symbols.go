package ingest

import (
	"slices"
	"strings"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// PricedIn stores every symbol in the SEC form, also the rows that come
// from the price provider, so that one symbol names one security in every
// table. The provider separates a share class with a dot (BRK.B), SEC with
// a dash (BRK-B).

// ProviderSymbol returns the provider's form of an SEC symbol.
func ProviderSymbol(secSymbol string) string {
	return strings.ReplaceAll(secSymbol, "-", ".")
}

// SECSymbol returns the SEC form of a provider symbol.
func SECSymbol(providerSymbol string) string {
	return strings.ReplaceAll(providerSymbol, ".", "-")
}

// isCoveredExchange reports whether the SEC exchange name is NYSE or
// Nasdaq.
func isCoveredExchange(exchange string) bool {
	return strings.EqualFold(exchange, "NYSE") || strings.EqualFold(exchange, "Nasdaq")
}

// SelectUniverse returns the tickers of current that trade on NYSE or
// Nasdaq, as the SEC ticker map names the exchange, sorted by symbol. When
// narrow is not empty, only its symbols stay. One company can appear once
// for each share class.
func SelectUniverse(current []domain.Ticker, narrow []string) []domain.Ticker {
	keep := map[string]bool{}
	for _, s := range narrow {
		keep[strings.ToUpper(s)] = true
	}
	out := []domain.Ticker{}
	for _, t := range current {
		if !isCoveredExchange(t.Exchange) {
			continue
		}
		if len(keep) > 0 && !keep[strings.ToUpper(t.Symbol)] {
			continue
		}
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b domain.Ticker) int { return strings.Compare(a.Symbol, b.Symbol) })
	return out
}

// companiesOf returns the distinct CIKs of tickers, in ascending order.
func companiesOf(tickers []domain.Ticker) []domain.CIK {
	ciks := make([]domain.CIK, 0, len(tickers))
	for _, t := range tickers {
		ciks = append(ciks, t.CIK)
	}
	slices.Sort(ciks)
	return slices.Compact(ciks)
}

// isPeriodicReport reports whether form is a filing that can change the
// derived quarters: a 10-Q, a 10-K, a transition 10-KT, or an amendment of
// one.
func isPeriodicReport(form string) bool {
	switch strings.TrimSuffix(form, "/A") {
	case "10-Q", "10-K", "10-KT":
		return true
	default:
		return false
	}
}
