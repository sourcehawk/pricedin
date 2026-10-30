package engine

import (
	"errors"
	"fmt"
	"math"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// Quarter is one fiscal quarter of a stock's history. Per-share values and
// the close are split-adjusted. A nil value was not reported.
type Quarter struct {
	Label         string      `json:"label"` // "Q3 FY26"
	PeriodEnd     domain.Date `json:"periodEnd"`
	EPS           *float64    `json:"eps"`           // diluted EPS of the quarter
	Close         *float64    `json:"close"`         // close on or before PeriodEnd
	DilutedShares *float64    `json:"dilutedShares"` // weighted average, whole shares
	Revenue       *float64    `json:"revenue"`
	// OperatingIncome, OperatingCosts and FreeCashFlow are in US dollars.
	OperatingIncome *float64 `json:"operatingIncome"`
	OperatingCosts  *float64 `json:"operatingCosts"`
	FreeCashFlow    *float64 `json:"freeCashFlow"`
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

// Validate reports the first lever that Evaluate cannot use. EPS and the
// custom exit P/E may be zero or negative: those give values with reason
// needs_positive_eps instead of an error.
func (l Levers) Validate() error {
	switch {
	case !isFinite(l.Price) || l.Price <= 0:
		return invalid("price must be positive, got %v", l.Price)
	case !isFinite(l.TTMEPS):
		return invalid("ttmEps must be finite, got %v", l.TTMEPS)
	case !isFinite(l.Dividend) || l.Dividend < 0:
		return invalid("dividend must be zero or more, got %v", l.Dividend)
	case !isFinite(l.TargetReturn) || l.TargetReturn <= -1:
		return invalid("targetReturn must be above -1, got %v", l.TargetReturn)
	case l.HoldingYears < 1 || l.HoldingYears > 10:
		return invalid("holdingYears must be 1 to 10, got %d", l.HoldingYears)
	}
	switch l.ExitPE.Mode {
	case ExitPEEstimated, ExitPEToday, ExitPETrailing:
	case ExitPECustom:
		if !isFinite(l.ExitPE.Custom) {
			return invalid("exitPe.custom must be finite, got %v", l.ExitPE.Custom)
		}
	default:
		return invalid("unknown exitPe.mode %q", l.ExitPE.Mode)
	}
	if l.ExitPE.TrailingMonths < 0 || l.Baseline.TrailingMonths < 0 {
		return invalid("trailingMonths must not be negative")
	}
	switch l.Baseline.Mode {
	case BaselineWeighted, BaselineToday, BaselineTrailing, BaselineCustom:
	default:
		return invalid("unknown baseline.mode %q", l.Baseline.Mode)
	}
	if err := validGrowth("baseline.custom.rate", l.Baseline.Custom.Rate); err != nil {
		return err
	}
	for i, r := range l.Baseline.Custom.Rates {
		if err := validGrowth(fmt.Sprintf("baseline.custom.rates[%d]", i), r); err != nil {
			return err
		}
	}
	for i, c := range l.Scenarios {
		if err := validGrowth(fmt.Sprintf("scenarios[%d].growth", i), c.Growth); err != nil {
			return err
		}
		if !isFinite(c.ExitPE) {
			return invalid("scenarios[%d].exitPe must be finite, got %v", i, c.ExitPE)
		}
	}
	switch l.ChartRange {
	case "", Range6M, Range1Y, Range3Y:
	default:
		return invalid("unknown chartRange %q", l.ChartRange)
	}
	return nil
}

func validGrowth(name string, g float64) error {
	if !isFinite(g) || g <= -1 {
		return invalid("%s must be above -1, got %v", name, g)
	}
	return nil
}

// Validate reports a history that Evaluate cannot use: daily points out of
// order or with a non-positive close.
func (h History) Validate() error {
	for i, d := range h.Daily {
		if !isFinite(d.Close) || d.Close <= 0 {
			return invalid("daily[%d].close must be positive, got %v", i, d.Close)
		}
		if i > 0 && !h.Daily[i-1].Date.Before(d.Date) {
			return invalid("daily[%d].date %s is not after %s", i, d.Date, h.Daily[i-1].Date)
		}
	}
	return nil
}
