package engine

import "math"

// Typical is the typical P/E: the median of the quarter-end P/Es over the
// look-back, with today's P/E (the price and TTM EPS levers) as the last
// point.
type Typical struct {
	Value Number `json:"value"`
	// Months is the look-back: the requested months rounded to a multiple
	// of 3, from 6 to 36. A shorter history uses every quarter it has.
	Months int `json:"months"`
	Points int `json:"points"` // the P/Es in the median; quarters without a positive TTM EPS or a close are left out
}

// defaultTypicalMonths is the look-back of the typical P/E when none is set.
const defaultTypicalMonths = 36

// TypicalPE returns the typical P/E over the requested months; zero months
// means 36.
func TypicalPE(quarters []Quarter, price, eps float64, months int) Typical {
	return newSeries(quarters).typical(price, eps, months)
}

func (s series) typical(price, eps float64, months int) Typical {
	if months == 0 {
		months = defaultTypicalMonths
	}
	m := int(jsRound(float64(months)/3) * 3)
	m = min(36, max(6, m))
	var pes []float64
	for i := max(0, s.last-m/3); i <= s.last; i++ {
		p, e := price, Some(eps)
		if i != s.last {
			p, e = 0, s.ttm[i]
			if c := s.q[i].Close; c != nil {
				p = *c
			}
		}
		if p != 0 && e.Ok() && e.Get() > 0 {
			pes = append(pes, p/e.Get())
		}
	}
	return Typical{Value: median(pes), Months: m, Points: len(pes)}
}

// ExitPERule names the rule that set an exit P/E.
type ExitPERule string

// The exit P/E rules.
const (
	// RuleFade: the premium of today's P/E over 15x shrinks as growth fades
	// toward 6%.
	RuleFade ExitPERule = "fade"
	// RuleSlowGrowerTypical: slow growth and a P/E more than 5% above the
	// typical P/E; the estimate is the typical P/E.
	RuleSlowGrowerTypical ExitPERule = "slowGrowerTypical"
	// RuleSlowGrowerKeep: slow growth; the estimate keeps today's P/E.
	RuleSlowGrowerKeep ExitPERule = "slowGrowerKeep"
	// RuleLowPEKeep: today's P/E is 15x or less; the estimate keeps it.
	RuleLowPEKeep ExitPERule = "lowPeKeep"
	RuleToday     ExitPERule = "today"
	RuleTrailing  ExitPERule = "trailing"
	RuleCustom    ExitPERule = "custom"
)

// Estimate is the estimated exit P/E with the numbers that explain it.
type Estimate struct {
	Value Number      `json:"value"`
	Rule  *ExitPERule `json:"rule"` // nil when Value is missing
	G0    Number      `json:"g0"`   // the weighted trend the estimate starts from
	GExit Number      `json:"gExit"`
}

const (
	peBase      = 15.0 // the P/E of a company that grows at growthFloor
	growthFloor = 0.06 // the growth that a company's growth fades toward
	fadeHalf    = 3.0  // years for half of the excess growth to fade
)

// EstimatedExitPE estimates the exit P/E after years. weighted is the
// weighted trend and typical the typical P/E; either may be nil. Without a
// weighted trend the stock counts as a slow grower. The result is rounded
// to 0.5 and is at least 6.
func EstimatedExitPE(price, eps float64, years int, weighted, typical *float64) Estimate {
	out := Estimate{G0: None[float64](ReasonInsufficientHistory), GExit: None[float64](ReasonInsufficientHistory)}
	gExit := 0.0
	if weighted != nil {
		gExit = growthFloor + (*weighted-growthFloor)*math.Pow(0.5, float64(years)/fadeHalf)
		out.G0, out.GExit = Some(*weighted), Some(gExit)
	}
	now := price / eps
	if !(eps > 0) || !(now > 0) {
		out.Value = None[float64](ReasonNeedsPositiveEPS)
		return out
	}
	slow := weighted == nil || *weighted <= growthFloor
	var pe float64
	var rule ExitPERule
	switch {
	case slow && typical != nil && now > *typical*1.05:
		pe, rule = *typical, RuleSlowGrowerTypical
	case slow:
		pe, rule = now, RuleSlowGrowerKeep
	case now <= peBase:
		pe, rule = now, RuleLowPEKeep
	default:
		pe, rule = peBase+(now-peBase)*(gExit-growthFloor)/(*weighted-growthFloor), RuleFade
	}
	out.Value = Some(math.Max(6, roundHalf(pe)))
	out.Rule = &rule
	return out
}

// ExitPE is the exit P/E in use with its rule.
type ExitPE struct {
	Value float64    `json:"value"`
	Rule  ExitPERule `json:"rule"`
}

// ExitPEView is everything the page shows about the exit P/E.
type ExitPEView struct {
	Mode     ExitPEMode    `json:"mode"`
	InUse    Maybe[ExitPE] `json:"inUse"`    // missing with needs_positive_eps when zero or less
	Estimate Maybe[ExitPE] `json:"estimate"` // the estimated exit P/E, whatever the mode
	G0       Number        `json:"g0"`
	GExit    Number        `json:"gExit"`
	Typical  Typical       `json:"typical"`
	// PENowVsTypical is "above" (more than 10% above the typical P/E),
	// "below" (more than 10% below) or "close".
	PENowVsTypical Maybe[string] `json:"peNowVsTypical"`
	// VsNow is "lower", "higher" or "same" (within 0.25) for the exit P/E in
	// use against today's P/E.
	VsNow     Maybe[string] `json:"vsNow"`
	SliderMax float64       `json:"sliderMax"` // the top of the exit P/E slider
}

func (s series) exitPE(l Levers, peNow Number) ExitPEView {
	typ := s.typical(l.Price, l.TTMEPS, l.ExitPE.TrailingMonths)
	est := EstimatedExitPE(l.Price, l.TTMEPS, l.HoldingYears, s.weighted().Value, typ.Value.Value)
	estimate := None[ExitPE](est.Value.Reason)
	if est.Value.Ok() {
		estimate = Some(ExitPE{Value: est.Value.Get(), Rule: *est.Rule})
	}
	var inUse Maybe[ExitPE]
	switch l.ExitPE.Mode {
	case ExitPEToday:
		inUse = None[ExitPE](ReasonNeedsPositiveEPS)
		if l.TTMEPS > 0 {
			inUse = Some(ExitPE{Value: roundHalf(l.Price / l.TTMEPS), Rule: RuleToday})
		}
	case ExitPETrailing:
		inUse = None[ExitPE](typ.Value.Reason)
		if typ.Value.Ok() {
			inUse = Some(ExitPE{Value: roundHalf(typ.Value.Get()), Rule: RuleTrailing})
		}
	case ExitPECustom:
		inUse = Some(ExitPE{Value: l.ExitPE.Custom, Rule: RuleCustom})
	default:
		inUse = estimate
	}
	if inUse.Ok() && !(inUse.Get().Value > 0) {
		inUse = None[ExitPE](ReasonNeedsPositiveEPS)
	}
	v := ExitPEView{Mode: l.ExitPE.Mode, InUse: inUse, Estimate: estimate, G0: est.G0, GExit: est.GExit, Typical: typ}
	switch {
	case !peNow.Ok():
		v.PENowVsTypical = None[string](peNow.Reason)
	case !typ.Value.Ok():
		v.PENowVsTypical = None[string](typ.Value.Reason)
	default:
		now, t := peNow.Get(), typ.Value.Get()
		v.PENowVsTypical = Some(classify(now > t*1.1, now < t*0.9, "above", "below", "close"))
	}
	switch {
	case !peNow.Ok():
		v.VsNow = None[string](peNow.Reason)
	case !inUse.Ok():
		v.VsNow = None[string](inUse.Reason)
	default:
		now, pe := peNow.Get(), inUse.Get().Value
		v.VsNow = Some(classify(pe < now-0.25, pe > now+0.25, "lower", "higher", "same"))
	}
	top := 0.0
	if l.TTMEPS > 0 {
		top = l.Price / l.TTMEPS
	}
	if inUse.Ok() && inUse.Get().Value > top {
		top = inUse.Get().Value
	}
	v.SliderMax = math.Max(45, math.Min(400, math.Ceil(top*1.3/5)*5))
	return v
}

func classify(first, second bool, a, b, otherwise string) string {
	switch {
	case first:
		return a
	case second:
		return b
	default:
		return otherwise
	}
}
