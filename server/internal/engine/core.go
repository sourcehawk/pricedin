package engine

import "math"

// Investment is a purchase at Price, held for Years, that pays Dividend per
// share each year in cash (not reinvested). EPS is the TTM EPS at purchase.
type Investment struct {
	Price        float64 `json:"price"`
	EPS          float64 `json:"eps"`
	TargetReturn float64 `json:"targetReturn"`
	Years        int     `json:"years"`
	Dividend     float64 `json:"dividend"`
}

// grown returns the factor (1+target)^years: what each dollar must become.
func (inv Investment) grown() float64 {
	return math.Pow(1+inv.TargetReturn, float64(inv.Years))
}

// neededFromExit is the part of the target value that the exit price must
// deliver: the target value minus the dividends received.
func (inv Investment) neededFromExit() float64 {
	return inv.Price*inv.grown() - inv.Dividend*float64(inv.Years)
}

// RequiredGrowth returns the average yearly EPS growth (compound) that
// turns the price into the target return when the stock sells at exitPE.
// It is missing with needs_positive_eps when EPS or exitPE is not positive,
// and with dividends_reach_target when the dividends alone reach the target.
// The result can be negative.
func RequiredGrowth(inv Investment, exitPE float64) Number {
	if !(inv.EPS > 0) || !(exitPE > 0) {
		return None[float64](ReasonNeedsPositiveEPS)
	}
	t := inv.neededFromExit()
	if t <= 0 {
		return None[float64](ReasonDividendsReachTarget)
	}
	return finite(math.Pow(t/(inv.EPS*exitPE), 1/float64(inv.Years))-1, ReasonNeedsPositiveEPS)
}

// RequirementResult is what the target return requires at an exit P/E.
type RequirementResult struct {
	Growth      Number  `json:"growth"`
	ExitPrice   Number  `json:"exitPrice"`   // share price needed at the exit
	PriceChange Number  `json:"priceChange"` // ExitPrice against today's price
	EPS         Number  `json:"eps"`         // EPS needed at the exit
	Dividends   float64 `json:"dividends"`   // dividends received over the hold
	// TightFrom and UndemandingFrom are the baseline growth rates where the
	// verdict turns tight and undemanding.
	TightFrom       Number `json:"tightFrom"`
	UndemandingFrom Number `json:"undemandingFrom"`
}

// Requirement returns the required growth with the exit price and EPS it
// implies. All values share the reason of a missing required growth.
func Requirement(inv Investment, exitPE float64) RequirementResult {
	g := RequiredGrowth(inv, exitPE)
	out := RequirementResult{Growth: g, Dividends: inv.Dividend * float64(inv.Years)}
	if !g.Ok() {
		none := None[float64](g.Reason)
		out.ExitPrice, out.PriceChange, out.EPS, out.TightFrom, out.UndemandingFrom = none, none, none, none, none
		return out
	}
	r := g.Get()
	eps := inv.EPS * math.Pow(1+r, float64(inv.Years))
	price := eps * exitPE
	out.ExitPrice = Some(price)
	out.PriceChange = Some(price/inv.Price - 1)
	out.EPS = Some(eps)
	out.TightFrom = Some(r - toneBand)
	out.UndemandingFrom = Some(r + toneBand)
	return out
}

// Projection is the outcome of growing EPS at a fixed rate and selling at
// an exit P/E.
type Projection struct {
	ExitPrice    float64 `json:"exitPrice"`
	TotalReturn  float64 `json:"totalReturn"`  // exit price plus dividends, against the price
	YearlyReturn float64 `json:"yearlyReturn"` // compound; -1 when nothing is left
}

// Project returns the outcome of EPS growth g for the investment, selling
// at exitPE. It ignores the target return.
func Project(inv Investment, g, exitPE float64) Projection {
	n := float64(inv.Years)
	px := inv.EPS * math.Pow(1+g, n) * exitPE
	tot := px + inv.Dividend*n
	return Projection{
		ExitPrice:    px,
		TotalReturn:  tot/inv.Price - 1,
		YearlyReturn: math.Pow(math.Max(tot, 0)/inv.Price, 1/n) - 1,
	}
}

// ExitPENeeded returns the exit P/E at which EPS growth g reaches the target
// return. It is missing with needs_positive_eps when EPS is not positive,
// growth_out_of_range when g is -100% or lower, and dividends_reach_target
// when any exit P/E would do.
func ExitPENeeded(inv Investment, g float64) Number {
	switch {
	case !(inv.EPS > 0):
		return None[float64](ReasonNeedsPositiveEPS)
	case !(g > -1):
		return None[float64](ReasonGrowthOutOfRange)
	}
	t := inv.neededFromExit()
	if !(t > 0) {
		return None[float64](ReasonDividendsReachTarget)
	}
	return Some(t / (inv.EPS * math.Pow(1+g, float64(inv.Years))))
}

// toneBand is the distance from a target that still counts as close to it.
const toneBand = 0.015

// ToneOf classifies a difference from a target: good above +1.5 pp, bad
// below -1.5 pp, warn within.
func ToneOf(diff float64) Tone {
	switch {
	case diff > toneBand:
		return ToneGood
	case diff < -toneBand:
		return ToneBad
	default:
		return ToneWarn
	}
}

// Verdict says how demanding the price is against the growth baseline.
type Verdict string

// The verdicts.
const (
	VerdictUndemanding Verdict = "undemanding"
	VerdictTight       Verdict = "tight"
	VerdictDemanding   Verdict = "demanding"
)

// VerdictOf classifies the gap between the baseline and the required growth.
func VerdictOf(gap float64) Verdict {
	switch ToneOf(gap) {
	case ToneGood:
		return VerdictUndemanding
	case ToneBad:
		return VerdictDemanding
	default:
		return VerdictTight
	}
}

// NiceStep returns a tick step of 1, 2 or 5 times a power of ten that cuts
// rng into about target parts. It returns 1 when rng is not positive.
func NiceStep(rng, target float64) float64 {
	if !(rng > 0) {
		return 1
	}
	raw := rng / target
	m := math.Pow(10, math.Floor(math.Log10(raw)))
	f := raw / m
	switch {
	case f < 1.5:
		return m
	case f < 3:
		return 2 * m
	case f < 7:
		return 5 * m
	default:
		return 10 * m
	}
}
