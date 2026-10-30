package prices

import (
	"fmt"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// AdjustedBar is a raw bar with its close in the share count of today.
type AdjustedBar struct {
	// Bar is the raw bar, unchanged.
	Bar domain.PriceBar
	// Factor is the cumulative ratio of the splits after the bar's date, or
	// 1 when there are none. Divide a raw per-share value of the bar by it.
	Factor float64
	// Close is the split-adjusted close: Bar.Close divided by Factor.
	Close float64
}

// AdjustForSplits returns one AdjustedBar for each bar, in the order of
// bars. A split applies to the bars of its own symbol that are dated before
// the split date. The bar on the split date already trades in the new
// shares. The splits can come in any order. AdjustForSplits returns an
// error for a split with a From or To that is not positive.
func AdjustForSplits(bars []domain.PriceBar, splits []domain.Split) ([]AdjustedBar, error) {
	for _, s := range splits {
		if s.From <= 0 || s.To <= 0 {
			return nil, fmt.Errorf("adjust for splits: %s split on %s: from %v and to %v must be positive", s.Symbol, s.Date, s.From, s.To)
		}
	}
	out := make([]AdjustedBar, len(bars))
	for i, b := range bars {
		factor := 1.0
		for _, s := range splits {
			if s.Symbol == b.Symbol && b.Date.Before(s.Date) {
				factor *= s.Ratio()
			}
		}
		out[i] = AdjustedBar{Bar: b, Factor: factor, Close: b.Close / factor}
	}
	return out, nil
}
