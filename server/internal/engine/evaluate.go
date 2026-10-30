package engine

import (
	"fmt"
	"math"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// Evaluation is everything the page shows for one stock and one set of
// levers. The panels read it and compute nothing themselves.
type Evaluation struct {
	PENow      Number            `json:"peNow"` // price over TTM EPS (the levers)
	ExitPE     ExitPEView        `json:"exitPe"`
	Baselines  Baselines         `json:"baselines"`
	YearByYear Maybe[YearByYear] `json:"yearByYear"` // only for the custom year-by-year baseline
	// Trajectory is the trajectory fill of the weighted trend over the
	// holding period, whatever the baseline.
	Trajectory Maybe[Trajectory] `json:"trajectory"`
	Required   RequirementResult `json:"required"`
	Gap        Number            `json:"gap"` // baseline minus required growth
	// Verdict is undemanding when the dividends alone reach the target.
	Verdict     Maybe[Verdict]          `json:"verdict"`
	AtBaseline  Maybe[AtBaseline]       `json:"atBaseline"`
	Sensitivity Maybe[LeverSensitivity] `json:"sensitivity"`
	Quarters    []QuarterView           `json:"quarters"`
	// GrowthScale is the growth ruler's scale over the required growth and
	// the baselines.
	GrowthScale      Axis               `json:"growthScale"`
	ReturnGrid       Maybe[Grid]        `json:"returnGrid"`
	EarningsPaths    Maybe[Paths]       `json:"earningsPaths"`
	HowDemanding     Demanding          `json:"howDemanding"`
	Scenarios        EvaluatedScenarios `json:"scenarios"`
	ShareCountEvents []ShareEvent       `json:"shareCountEvents"`
	PriceChart       Maybe[Chart]       `json:"priceChart"`
	Financials       FinancialsView     `json:"financials"`
}

// Baselines are the growth baselines and the one in use.
type Baselines struct {
	Active   BaselineMode `json:"active"`
	Value    Number       `json:"value"` // the growth of the active baseline
	Weighted Number       `json:"weighted"`
	Today    Number       `json:"today"`
	Trailing Trailing     `json:"trailing"`
	// Custom is the flat custom rate or the compound average of the yearly
	// rates; not_applicable unless the custom baseline is active.
	Custom Number `json:"custom"`
	// YoYShift is the latest quarter's YoY growth minus the YoY growth three
	// quarters earlier: the direction of the last four readings.
	YoYShift Number `json:"yoyShift"`
}

// YearByYear summarizes the custom year-by-year rates.
type YearByYear struct {
	Rates           []float64 `json:"rates"` // one per year of the holding period
	CompoundAverage float64   `json:"compoundAverage"`
	SimpleAverage   float64   `json:"simpleAverage"`
	EndEPS          Number    `json:"endEps"` // EPS at the end of the holding period
	// ShowSimpleAverage is set when the simple average differs from the
	// compound one by 0.2 pp or more.
	ShowSimpleAverage bool `json:"showSimpleAverage"`
}

// AtBaseline is the outcome if EPS grows at the baseline and the stock sells
// at the exit P/E in use.
type AtBaseline struct {
	ExitPrice    float64 `json:"exitPrice"`
	TotalReturn  float64 `json:"totalReturn"`
	YearlyReturn float64 `json:"yearlyReturn"`
	VsTarget     float64 `json:"vsTarget"`
	Tone         Tone    `json:"tone"`
	MeetsTarget  bool    `json:"meetsTarget"`
	EPS          float64 `json:"eps"`    // EPS at the exit
	EPSGap       Number  `json:"epsGap"` // EPS minus the EPS needed
	ExitPENeeded Number  `json:"exitPeNeeded"`
	// Fragile is set when the baseline is within 2 pp of the required growth
	// or the exit P/E within 1.5 of the one needed: a small change flips the
	// tone.
	Fragile bool `json:"fragile"`
}

// LeverSensitivity is the required growth after one lever change each.
type LeverSensitivity struct {
	LowerExitPE  SensitivityAt `json:"lowerExitPe"`  // exit P/E 2 lower, at least 1
	LowerTarget  SensitivityAt `json:"lowerTarget"`  // target 2 pp lower, at least 0
	MarketTarget SensitivityAt `json:"marketTarget"` // a 10% target, the S&P 500's long-run average
	OneYear      SensitivityAt `json:"oneYear"`
	FiveYears    SensitivityAt `json:"fiveYears"`
}

// SensitivityAt is the required growth with one lever changed. Only the
// changed lever is set.
type SensitivityAt struct {
	ExitPE         *float64 `json:"exitPe,omitempty"`
	TargetReturn   *float64 `json:"targetReturn,omitempty"`
	Years          *int     `json:"years,omitempty"`
	RequiredGrowth Number   `json:"requiredGrowth"`
}

// QuarterView is one quarter with its derived values.
type QuarterView struct {
	Label     string      `json:"label"`
	PeriodEnd domain.Date `json:"periodEnd"`
	EPS       *float64    `json:"eps"`
	QuarterPoint
}

// EvaluatedScenarios are the cases in use and their outcomes.
type EvaluatedScenarios struct {
	Defaults bool `json:"defaults"` // the cases are the defaults, not the user's
	ScenarioSet
}

// marketReturn is the S&P 500's long-run average yearly return.
const marketReturn = 0.10

// Evaluate computes everything the page shows. It returns an error wrapping
// ErrInvalidInput when the levers or the history are invalid; values that
// cannot be computed from valid inputs are missing with a reason instead.
func Evaluate(h History, l Levers) (Evaluation, error) {
	if err := l.Validate(); err != nil {
		return Evaluation{}, fmt.Errorf("evaluate: %w", err)
	}
	if err := h.Validate(); err != nil {
		return Evaluation{}, fmt.Errorf("evaluate: %w", err)
	}
	s := newSeries(h.Quarters)
	inv := Investment{Price: l.Price, EPS: l.TTMEPS, TargetReturn: l.TargetReturn, Years: l.HoldingYears, Dividend: l.Dividend}
	n := l.HoldingYears

	var ev Evaluation
	ev.PENow = None[float64](ReasonNeedsPositiveEPS)
	if l.TTMEPS > 0 {
		ev.PENow = Some(l.Price / l.TTMEPS)
	}

	// The baselines.
	b := Baselines{Active: l.Baseline.Mode, Weighted: s.weighted(), Today: s.today(), Trailing: s.trailing(l.Baseline.TrailingMonths), Custom: None[float64](ReasonNotApplicable)}
	ev.YearByYear = None[YearByYear](ReasonNotApplicable)
	var yearly []float64
	if l.Baseline.Mode == BaselineCustom {
		c := l.Baseline.Custom
		b.Custom = Some(c.Rate)
		if c.Yearly {
			yearly = YearRates(c, n)
			avg := YearlyAverage(yearly)
			b.Custom = Some(avg)
			simple := 0.0
			for _, r := range yearly {
				simple += r
			}
			simple /= float64(len(yearly))
			y := YearByYear{Rates: yearly, CompoundAverage: avg, SimpleAverage: simple, EndEPS: None[float64](ReasonNeedsPositiveEPS), ShowSimpleAverage: math.Abs(simple-avg) >= 0.002}
			if l.TTMEPS > 0 {
				y.EndEPS = Some(BaselinePath(l.TTMEPS, avg, 4*n, yearly))
			}
			ev.YearByYear = Some(y)
		}
	}
	switch l.Baseline.Mode {
	case BaselineToday:
		b.Value = b.Today
	case BaselineTrailing:
		b.Value = b.Trailing.Value
	case BaselineCustom:
		b.Value = b.Custom
	default:
		b.Value = b.Weighted
	}
	b.YoYShift = None[float64](ReasonInsufficientHistory)
	if s.last >= 3 {
		cur, old := s.yoy[s.last], s.yoy[s.last-3]
		b.YoYShift = None[float64](reasonOf(cur, old))
		if cur.Ok() && old.Ok() {
			b.YoYShift = Some(cur.Get() - old.Get())
		}
	}
	ev.Baselines = b
	g := b.Value

	// The exit P/E and the requirement.
	ev.ExitPE = s.exitPE(l, ev.PENow)
	pe := None[float64](ev.ExitPE.InUse.Reason)
	if ev.ExitPE.InUse.Ok() {
		pe = Some(ev.ExitPE.InUse.Get().Value)
	}
	if pe.Ok() {
		ev.Required = Requirement(inv, pe.Get())
	} else {
		none := None[float64](pe.Reason)
		ev.Required = RequirementResult{Growth: none, ExitPrice: none, PriceChange: none, EPS: none, Dividends: l.Dividend * float64(n), TightFrom: none, UndemandingFrom: none}
	}
	req := ev.Required.Growth
	switch {
	case req.Reason == ReasonDividendsReachTarget:
		ev.Gap, ev.Verdict = None[float64](req.Reason), Some(VerdictUndemanding)
	case !req.Ok():
		ev.Gap, ev.Verdict = None[float64](req.Reason), None[Verdict](req.Reason)
	case !g.Ok():
		ev.Gap, ev.Verdict = None[float64](g.Reason), None[Verdict](g.Reason)
	default:
		gap := g.Get() - req.Get()
		ev.Gap, ev.Verdict = Some(gap), Some(VerdictOf(gap))
	}

	ev.AtBaseline = atBaseline(inv, g, pe, req, ev.Required.EPS)
	ev.Sensitivity = sensitivity(inv, pe)
	ev.Trajectory = s.trajectory(n)

	ev.Quarters = make([]QuarterView, len(h.Quarters))
	for i, q := range h.Quarters {
		ev.Quarters[i] = QuarterView{Label: q.Label, PeriodEnd: q.PeriodEnd, EPS: q.EPS, QuarterPoint: QuarterPoint{TTMEPS: s.ttm[i], YoY: s.yoy[i], TTMYoY: s.ttmYoY[i], WeightedTrend: s.wtd[i]}}
	}
	ev.GrowthScale = GrowthScale([]*float64{req.Value, b.Today.Value, b.Weighted.Value, b.Trailing.Value.Value, g.Value})

	switch {
	case !pe.Ok() && l.TTMEPS > 0:
		ev.ReturnGrid = None[Grid](pe.Reason)
	case !pe.Ok():
		ev.ReturnGrid = None[Grid](ReasonNeedsPositiveEPS)
	default:
		ev.ReturnGrid = ReturnGrid(GridInput{
			Investment: inv, ExitPE: pe.Get(), Required: req,
			Baseline: ActiveBaseline{Mode: l.Baseline.Mode, Value: g},
			Weighted: b.Weighted, Today: b.Today, Trailing: b.Trailing.Value,
		})
	}
	ev.EarningsPaths = EarningsPaths(PathsInput{Quarters: h.Quarters, EPS: l.TTMEPS, Years: n, Baseline: g, YearlyRates: yearly, Required: req})
	ev.HowDemanding = HowDemanding(h.Quarters, inv, pe)

	cases := l.Scenarios
	ev.Scenarios.Defaults = len(cases) == 0
	if ev.Scenarios.Defaults {
		cases = DefaultScenarios(b.Weighted.Value, pe.Value)
	}
	ev.Scenarios.ScenarioSet = Scenarios(inv, cases)
	ev.ShareCountEvents = ShareCountEvents(h.Quarters)
	ev.PriceChart = PriceChart(h.Daily, h.Quarters, l.ChartRange)
	ev.Financials = Financials(h.Quarters)
	return ev, nil
}

func atBaseline(inv Investment, g, pe, req, reqEPS Number) Maybe[AtBaseline] {
	switch {
	case !(inv.EPS > 0):
		return None[AtBaseline](ReasonNeedsPositiveEPS)
	case !pe.Ok():
		return None[AtBaseline](pe.Reason)
	case !g.Ok():
		return None[AtBaseline](g.Reason)
	case !(g.Get() > -1):
		return None[AtBaseline](ReasonGrowthOutOfRange)
	}
	p := Project(inv, g.Get(), pe.Get())
	need := ExitPENeeded(inv, g.Get())
	eps := inv.EPS * math.Pow(1+g.Get(), float64(inv.Years))
	diff := p.YearlyReturn - inv.TargetReturn
	a := AtBaseline{
		ExitPrice: p.ExitPrice, TotalReturn: p.TotalReturn, YearlyReturn: p.YearlyReturn,
		VsTarget: diff, Tone: ToneOf(diff), MeetsTarget: p.YearlyReturn >= inv.TargetReturn,
		EPS: eps, EPSGap: None[float64](reqEPS.Reason), ExitPENeeded: need,
		Fragile: (req.Ok() && math.Abs(g.Get()-req.Get()) < 0.02) || (need.Ok() && math.Abs(need.Get()-pe.Get()) < 1.5),
	}
	if reqEPS.Ok() {
		a.EPSGap = Some(eps - reqEPS.Get())
	}
	return Some(a)
}

func sensitivity(inv Investment, pe Number) Maybe[LeverSensitivity] {
	switch {
	case !(inv.EPS > 0):
		return None[LeverSensitivity](ReasonNeedsPositiveEPS)
	case !pe.Ok():
		return None[LeverSensitivity](pe.Reason)
	}
	lowerPE := math.Max(1, pe.Get()-2)
	lowerTarget := math.Max(0, inv.TargetReturn-0.02)
	market := marketReturn
	one, five := 1, 5
	with := func(f func(*Investment)) Number {
		at := inv
		f(&at)
		return RequiredGrowth(at, pe.Get())
	}
	return Some(LeverSensitivity{
		LowerExitPE:  SensitivityAt{ExitPE: &lowerPE, RequiredGrowth: RequiredGrowth(inv, lowerPE)},
		LowerTarget:  SensitivityAt{TargetReturn: &lowerTarget, RequiredGrowth: with(func(i *Investment) { i.TargetReturn = lowerTarget })},
		MarketTarget: SensitivityAt{TargetReturn: &market, RequiredGrowth: with(func(i *Investment) { i.TargetReturn = market })},
		OneYear:      SensitivityAt{Years: &one, RequiredGrowth: with(func(i *Investment) { i.Years = one })},
		FiveYears:    SensitivityAt{Years: &five, RequiredGrowth: with(func(i *Investment) { i.Years = five })},
	})
}
