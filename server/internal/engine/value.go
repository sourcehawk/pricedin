package engine

import (
	"math"
	"slices"
	"strconv"
)

// Reason says why a value is missing. Every missing value in the engine's
// output carries one.
type Reason string

// The reasons a value can be missing.
const (
	// ReasonNeedsPositiveEPS: the value divides by EPS, or by a P/E, that is
	// zero or negative.
	ReasonNeedsPositiveEPS Reason = "needs_positive_eps"
	// ReasonDividendsReachTarget: the dividends alone reach the target return,
	// so no EPS growth is required.
	ReasonDividendsReachTarget Reason = "dividends_reach_target"
	// ReasonInsufficientHistory: the history has too few quarters or days.
	ReasonInsufficientHistory Reason = "insufficient_history"
	// ReasonMissingData: an input that the value needs is null.
	ReasonMissingData Reason = "missing_data"
	// ReasonNonPositiveBase: a change against a base that is zero or negative.
	ReasonNonPositiveBase Reason = "non_positive_base"
	// ReasonGrowthOutOfRange: a growth rate of -100% or lower, or above 1000%.
	ReasonGrowthOutOfRange Reason = "growth_out_of_range"
	// ReasonTooFewPoints: a correlation needs at least 20 points.
	ReasonTooFewPoints Reason = "too_few_points"
	// ReasonZeroVariance: a correlation of a series that does not vary.
	ReasonZeroVariance Reason = "zero_variance"
	// ReasonNotApplicable: the value does not exist for these inputs, such as
	// the custom growth rate when another baseline is active.
	ReasonNotApplicable Reason = "not_applicable"
)

// Maybe is a value that can be missing. It marshals to {"value": v} or to
// {"value": null, "reason": "..."}; a missing value always has a Reason.
type Maybe[T any] struct {
	Value  *T     `json:"value"`
	Reason Reason `json:"reason,omitempty"`
}

// Number is a number that can be missing.
type Number = Maybe[float64]

// Some returns a present value.
func Some[T any](v T) Maybe[T] {
	return Maybe[T]{Value: &v}
}

// None returns a missing value with its reason.
func None[T any](r Reason) Maybe[T] {
	return Maybe[T]{Reason: r}
}

// Ok reports whether the value is present.
func (m Maybe[T]) Ok() bool {
	return m.Value != nil
}

// Get returns the value, or the zero value of T when it is missing.
func (m Maybe[T]) Get() T {
	if m.Value == nil {
		var zero T
		return zero
	}
	return *m.Value
}

// finite returns v when it is a finite number, and None with r otherwise.
func finite(v float64, r Reason) Number {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return None[float64](r)
	}
	return Some(v)
}

// reasonOf returns the reason of the first missing value among ms.
func reasonOf[T any](ms ...Maybe[T]) Reason {
	for _, m := range ms {
		if !m.Ok() {
			return m.Reason
		}
	}
	return ""
}

// Tone compares a value with a target: good, warn (close to it) or bad.
// Financial changes that are neither good nor bad are neutral.
type Tone string

// The tones.
const (
	ToneGood    Tone = "good"
	ToneWarn    Tone = "warn"
	ToneBad     Tone = "bad"
	ToneNeutral Tone = "neutral"
)

// Axis is a chart scale with tick steps. Min and Max are tick positions.
type Axis struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Step float64 `json:"step"`
}

// Range is a chart scale without ticks.
type Range struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// widen gives a zero-width domain a visible width.
func widen(lo, hi float64) (float64, float64) {
	if hi > lo {
		return lo, hi
	}
	d := 1.0
	if math.Abs(hi) > 0 {
		d = math.Abs(hi) * 0.1
	}
	return lo - d, hi + d
}

func newAxis(lo, hi, step float64) Axis {
	lo, hi = widen(lo, hi)
	return Axis{Min: lo, Max: hi, Step: step}
}

func newRange(lo, hi float64) Range {
	lo, hi = widen(lo, hi)
	return Range{Min: lo, Max: hi}
}

// jsRound rounds like JavaScript's Math.round: halves go toward +Inf. The
// TypeScript engine uses Math.round, so both engines must round alike.
func jsRound(x float64) float64 {
	r := math.Floor(x)
	if x-r >= 0.5 {
		r++
	}
	return r
}

// roundHalf rounds to the nearest 0.5, the precision of every P/E lever.
func roundHalf(x float64) float64 {
	return jsRound(x*2) / 2
}

// toFixed rounds like JavaScript's Number.prototype.toFixed followed by a
// conversion back to a number. Both round the exact binary value; they
// differ only on exact decimal ties, which the engine's grids never produce.
func toFixed(x float64, digits int) float64 {
	v, err := strconv.ParseFloat(strconv.FormatFloat(x, 'f', digits, 64), 64)
	if err != nil {
		return x
	}
	return v
}

// median returns the conventional median of the finite values: the mean of
// the two middle values for an even count.
func median(values []float64) Number {
	v := make([]float64, 0, len(values))
	for _, x := range values {
		if !math.IsNaN(x) && !math.IsInf(x, 0) {
			v = append(v, x)
		}
	}
	if len(v) == 0 {
		return None[float64](ReasonInsufficientHistory)
	}
	slices.Sort(v)
	h := len(v) / 2
	if len(v)%2 == 1 {
		return Some(v[h])
	}
	return Some((v[h-1] + v[h]) / 2)
}
