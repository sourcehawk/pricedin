package stocks

import "math"

// The bounds within which the engine accepts a stored value, from
// engine.Levers.Validate and engine.History.Validate. A value outside them
// is treated as missing, so that stored data never fails an evaluation.
const (
	minPerShare = 1e-4 // the smallest EPS, close or dividend other than zero
	maxPerShare = 1e6
	minAmount   = 1e-6 // the smallest share count or dollar amount other than zero
	maxAmount   = 1e15
)

// perShareOK reports whether the engine accepts v as an EPS.
func perShareOK(v float64) bool {
	a := math.Abs(v)
	return v == 0 || (a >= minPerShare && a <= maxPerShare)
}

// closeOK reports whether the engine accepts v as a close.
func closeOK(v float64) bool {
	return v >= minPerShare && v <= maxPerShare
}

// amountOK reports whether the engine accepts v as a share count or an
// amount in dollars.
func amountOK(v float64) bool {
	a := math.Abs(v)
	return v == 0 || (a >= minAmount && a <= maxAmount)
}
