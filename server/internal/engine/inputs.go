package engine

import (
	"errors"
	"fmt"
	"math"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// Quarter is one fiscal quarter of a stock's history. Per-share values and
// the close are split-adjusted. Revenue, OperatingIncome, OperatingCosts and
// FreeCashFlow are in US dollars. A nil value was not reported.
type Quarter struct {
	Label           string      `json:"label"` // "Q3 FY26"
	PeriodEnd       domain.Date `json:"periodEnd"`
	EPS             *float64    `json:"eps"`           // diluted EPS of the quarter
	Close           *float64    `json:"close"`         // close on or before PeriodEnd
	DilutedShares   *float64    `json:"dilutedShares"` // weighted average, whole shares
	Revenue         *float64    `json:"revenue"`
	OperatingIncome *float64    `json:"operatingIncome"`
	OperatingCosts  *float64    `json:"operatingCosts"`
	FreeCashFlow    *float64    `json:"freeCashFlow"`
}

// PricePoint is one daily close with the TTM EPS that was known on that day.
type PricePoint struct {
	Date   domain.Date `json:"date"`
	Close  float64     `json:"close"`
	TTMEPS *float64    `json:"ttmEps"`
}

// History is everything the engine knows about a stock. Quarters are
// consecutive fiscal quarters and Daily holds trading days, both oldest
// first.
type History struct {
	Quarters []Quarter    `json:"quarters"`
	Daily    []PricePoint `json:"daily"`
}

// ExitPEMode selects how the exit P/E is set.
type ExitPEMode string

// The exit P/E modes.
const (
	ExitPEEstimated ExitPEMode = "estimated"
	ExitPEToday     ExitPEMode = "today"
	ExitPETrailing  ExitPEMode = "trailing"
	ExitPECustom    ExitPEMode = "custom"
)

// ExitPELever is the exit P/E lever. TrailingMonths is the look-back of the
// typical P/E in every mode; zero means 36.
type ExitPELever struct {
	Mode           ExitPEMode `json:"mode"`
	Custom         float64    `json:"custom,omitempty"`
	TrailingMonths int        `json:"trailingMonths,omitempty"`
}

// BaselineMode selects the growth baseline.
type BaselineMode string

// The growth baselines.
const (
	BaselineWeighted BaselineMode = "weighted"
	BaselineToday    BaselineMode = "today"
	BaselineTrailing BaselineMode = "trailing"
	BaselineCustom   BaselineMode = "custom"
)

// CustomGrowth is the user's own growth: one Rate for every year, or, when
// Yearly is set, one rate per year of the holding period.
type CustomGrowth struct {
	Yearly bool      `json:"yearly"`
	Rate   float64   `json:"rate"`
	Rates  []float64 `json:"rates"`
}

// BaselineLever is the growth-baseline lever. TrailingMonths is the
// look-back of trailing growth in every mode; zero means 12.
type BaselineLever struct {
	Mode           BaselineMode `json:"mode"`
	TrailingMonths int          `json:"trailingMonths,omitempty"`
	Custom         CustomGrowth `json:"custom"`
}

// ScenarioCase is one of the bear, base and bull cases.
type ScenarioCase struct {
	Name   string  `json:"name"`
	Growth float64 `json:"growth"`
	ExitPE float64 `json:"exitPe"`
}

// ChartRange is the visible period of the price chart.
type ChartRange string

// The chart ranges.
const (
	Range6M ChartRange = "6M"
	Range1Y ChartRange = "1Y"
	Range3Y ChartRange = "3Y"
)

// months returns the length of the range; an empty range is 3Y.
func (r ChartRange) months() int {
	switch r {
	case Range6M:
		return 6
	case Range1Y:
		return 12
	default:
		return 36
	}
}

// Levers are the user's inputs. Rates and returns are fractions (0.15).
type Levers struct {
	Price        float64       `json:"price"`
	TTMEPS       float64       `json:"ttmEps"`
	Dividend     float64       `json:"dividend"` // yearly, per share
	TargetReturn float64       `json:"targetReturn"`
	HoldingYears int           `json:"holdingYears"`
	ExitPE       ExitPELever   `json:"exitPe"`
	Baseline     BaselineLever `json:"baseline"`
	// Scenarios are the user's bear, base and bull cases; empty means the
	// defaults around the weighted trend.
	Scenarios  []ScenarioCase `json:"scenarios"`
	ChartRange ChartRange     `json:"chartRange"` // empty means 3Y
}

// ErrInvalidInput wraps every validation error of Evaluate.
var ErrInvalidInput = errors.New("invalid engine input")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, fmt.Sprintf(format, args...))
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// The bounds of the levers, from the prototype's own input limits, and of
// the history. Within them no computed value overflows.
const (
	maxTargetReturn = 0.5  // a 50% target return
	minCustomGrowth = -0.5 // a custom rate of -50%
	maxCustomGrowth = 2.0  // a custom rate of 200%
	maxCustomExitPE = 1000 // the highest custom exit P/E
	maxScenarioPE   = 1e4  // the highest exit P/E of a bear, base or bull case
	maxMonths       = 120  // the longest look-back that a lever can ask for
	maxPerShare     = 1e6  // the largest price, EPS or dividend per share
	minPerShare     = 1e-4 // the smallest EPS or price per share other than zero
	maxAmount       = 1e15 // the largest share count or dollar amount
	minAmount       = 1e-6 // the smallest share count or dollar amount other than zero
)

// Validate reports the first lever that Evaluate cannot use. The price is
// 0.0001 to 1,000,000; EPS is zero or 0.0001 to 1,000,000 in size; the
// dividend is 0 to 1,000,000. The target return is 0 to 50%, the holding
// period 1 to 10 years, a custom exit P/E above 0 and up to 1000, custom
// rates -50% to 200%, and look-backs 0 (the default) to 120 months. A case
// has growth above -100% and up to 1000%, and an exit P/E of at most 10,000
// in size. EPS and a case exit P/E may be zero or negative: those give
// values with reason needs_positive_eps instead of an error.
func (l Levers) Validate() error {
	if err := validPerShare("price", l.Price, false); err != nil {
		return err
	}
	if err := validPerShare("ttmEps", l.TTMEPS, true); err != nil {
		return err
	}
	switch {
	case !isFinite(l.Dividend) || l.Dividend < 0 || l.Dividend > maxPerShare:
		return invalid("dividend must be 0 to %g, got %v", maxPerShare, l.Dividend)
	case !isFinite(l.TargetReturn) || l.TargetReturn < 0 || l.TargetReturn > maxTargetReturn:
		return invalid("targetReturn must be 0 to %g, got %v", maxTargetReturn, l.TargetReturn)
	case l.HoldingYears < 1 || l.HoldingYears > 10:
		return invalid("holdingYears must be 1 to 10, got %d", l.HoldingYears)
	}
	switch l.ExitPE.Mode {
	case ExitPEEstimated, ExitPEToday, ExitPETrailing:
	case ExitPECustom:
		if c := l.ExitPE.Custom; !isFinite(c) || c <= 0 || c > maxCustomExitPE {
			return invalid("exitPe.custom must be above 0 and at most %d, got %v", maxCustomExitPE, c)
		}
	default:
		return invalid("unknown exitPe.mode %q", l.ExitPE.Mode)
	}
	for name, m := range map[string]int{"exitPe.trailingMonths": l.ExitPE.TrailingMonths, "baseline.trailingMonths": l.Baseline.TrailingMonths} {
		if m < 0 || m > maxMonths {
			return invalid("%s must be 0 to %d, got %d", name, maxMonths, m)
		}
	}
	switch l.Baseline.Mode {
	case BaselineWeighted, BaselineToday, BaselineTrailing, BaselineCustom:
	default:
		return invalid("unknown baseline.mode %q", l.Baseline.Mode)
	}
	if err := validCustomRate("baseline.custom.rate", l.Baseline.Custom.Rate); err != nil {
		return err
	}
	for i, r := range l.Baseline.Custom.Rates {
		if err := validCustomRate(fmt.Sprintf("baseline.custom.rates[%d]", i), r); err != nil {
			return err
		}
	}
	for i, c := range l.Scenarios {
		if !isFinite(c.Growth) || !growthInRange(c.Growth) {
			return invalid("scenarios[%d].growth must be above -1 and at most %g, got %v", i, float64(maxGrowth), c.Growth)
		}
		if !isFinite(c.ExitPE) || math.Abs(c.ExitPE) > maxScenarioPE {
			return invalid("scenarios[%d].exitPe must be at most %g in size, got %v", i, maxScenarioPE, c.ExitPE)
		}
	}
	switch l.ChartRange {
	case "", Range6M, Range1Y, Range3Y:
	default:
		return invalid("unknown chartRange %q", l.ChartRange)
	}
	return nil
}

func validCustomRate(name string, g float64) error {
	if !isFinite(g) || g < minCustomGrowth || g > maxCustomGrowth {
		return invalid("%s must be %g to %g, got %v", name, minCustomGrowth, maxCustomGrowth, g)
	}
	return nil
}

// validPerShare checks a per-share value: positive (or, with signed, any
// sign or zero), and zero or 0.0001 to 1,000,000 in size.
func validPerShare(name string, v float64, signed bool) error {
	a := math.Abs(v)
	switch {
	case !isFinite(v):
		return invalid("%s must be finite, got %v", name, v)
	case !signed && v <= 0:
		return invalid("%s must be positive, got %v", name, v)
	case v != 0 && (a < minPerShare || a > maxPerShare):
		return invalid("%s must be zero or %g to %g in size, got %v", name, minPerShare, maxPerShare, v)
	}
	return nil
}

// validAmount checks a share count or dollar amount: zero or 0.000001 to
// 10^15 in size.
func validAmount(name string, v float64) error {
	a := math.Abs(v)
	if !isFinite(v) || (v != 0 && (a < minAmount || a > maxAmount)) {
		return invalid("%s must be zero or %g to %g in size, got %v", name, minAmount, maxAmount, v)
	}
	return nil
}

// Validate reports a history that Evaluate cannot use: an EPS, close or TTM
// EPS outside the per-share bounds of Levers.Validate, a close that is not
// positive, a share count or dollar amount that is not zero or 0.000001 to
// 10^15 in size, or a daily date that is not after the one before it.
func (h History) Validate() error {
	for i, q := range h.Quarters {
		at := fmt.Sprintf("quarters[%d]", i)
		if q.EPS != nil {
			if err := validPerShare(at+".eps", *q.EPS, true); err != nil {
				return err
			}
		}
		if q.Close != nil {
			if err := validPerShare(at+".close", *q.Close, false); err != nil {
				return err
			}
		}
		for name, v := range map[string]*float64{"dilutedShares": q.DilutedShares, "revenue": q.Revenue, "operatingIncome": q.OperatingIncome, "operatingCosts": q.OperatingCosts, "freeCashFlow": q.FreeCashFlow} {
			if v != nil {
				if err := validAmount(at+"."+name, *v); err != nil {
					return err
				}
			}
		}
	}
	for i, d := range h.Daily {
		at := fmt.Sprintf("daily[%d]", i)
		if err := validPerShare(at+".close", d.Close, false); err != nil {
			return err
		}
		if d.TTMEPS != nil {
			if err := validPerShare(at+".ttmEps", *d.TTMEPS, true); err != nil {
				return err
			}
		}
		if i > 0 && !h.Daily[i-1].Date.Before(d.Date) {
			return invalid("%s.date %s is not after %s", at, d.Date, h.Daily[i-1].Date)
		}
	}
	return nil
}
