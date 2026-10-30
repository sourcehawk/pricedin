package stocks

import (
	"math"
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/engine"
)

// The bounds of this package must be the engine's: a value they accept
// must pass engine validation, and a value just outside must fail it.
func TestTheBoundsAreTheEngines(t *testing.T) {
	day := domain.NewDate(2026, 1, 2)
	quarter := func(eps, shares float64) engine.History {
		return engine.History{Quarters: []engine.Quarter{{Label: "Q1 FY26", PeriodEnd: day, EPS: &eps, DilutedShares: &shares}}}
	}
	daily := func(c float64) engine.History {
		return engine.History{Daily: []engine.PricePoint{{Date: day, Close: c}}}
	}
	for _, tt := range []struct {
		name string
		h    engine.History
		ok   bool
	}{
		{"smallest EPS", quarter(minPerShare, 1), true},
		{"EPS below it", quarter(math.Nextafter(minPerShare, 0), 1), false},
		{"largest EPS", quarter(-maxPerShare, 1), true},
		{"EPS above it", quarter(math.Nextafter(maxPerShare, 2*maxPerShare), 1), false},
		{"smallest amount", quarter(1, minAmount), true},
		{"amount below it", quarter(1, math.Nextafter(minAmount, 0)), false},
		{"largest amount", quarter(1, maxAmount), true},
		{"amount above it", quarter(1, math.Nextafter(maxAmount, 2*maxAmount)), false},
		{"largest close", daily(maxPrice), true},
		{"close above it", daily(math.Nextafter(maxPrice, 2*maxPrice)), false},
		{"smallest close", daily(minPerShare), true},
		{"close below it", daily(math.Nextafter(minPerShare, 0)), false},
	} {
		if err := tt.h.Validate(); (err == nil) != tt.ok {
			t.Errorf("%s: engine Validate() = %v, want ok %v", tt.name, err, tt.ok)
		}
	}
	for v, ok := range map[float64]bool{0: true, minPerShare: true, -minPerShare: true, maxPerShare: true, math.Nextafter(minPerShare, 0): false, 2 * maxPerShare: false} {
		if perShareOK(v) != ok {
			t.Errorf("perShareOK(%v) = %v, want %v", v, !ok, ok)
		}
	}
	for v, ok := range map[float64]bool{0: false, minPerShare: true, 1.5e6: true, maxPrice: true, math.Nextafter(minPerShare, 0): false, math.Nextafter(maxPrice, 2*maxPrice): false} {
		if closeOK(v) != ok {
			t.Errorf("closeOK(%v) = %v, want %v", v, !ok, ok)
		}
	}
}

func TestBoundedEPSIsAlwaysAValidLever(t *testing.T) {
	for _, v := range []float64{0, 5e-5, -5e-5, 1, -3, 2e6, -2e6, math.Inf(1), math.Inf(-1), math.NaN()} {
		l := engine.Levers{Price: 1, TTMEPS: boundedEPS(v), HoldingYears: 3, ExitPE: engine.ExitPELever{Mode: engine.ExitPEEstimated}, Baseline: engine.BaselineLever{Mode: engine.BaselineWeighted}}
		if err := l.Validate(); err != nil {
			t.Errorf("boundedEPS(%v) = %v: %v", v, l.TTMEPS, err)
		}
	}
}
