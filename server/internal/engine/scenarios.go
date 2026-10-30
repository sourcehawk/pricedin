package engine

import "math"

// fallbackGrowth and fallbackExitPE center the default cases when the
// history has no weighted trend or the exit P/E is missing.
const (
	fallbackGrowth = 0.05
	fallbackExitPE = 20.0
)

// DefaultScenarios returns the default bear, base and bull cases: the
// weighted trend 6 pp lower, as is, and 6 pp higher, at 0.75, 1 and 1.2
// times the exit P/E. Growth is rounded to 0.5 pp and the exit P/E to 0.5;
// the bear exit P/E is at least 6. Nil inputs use 5% growth and 20x.
func DefaultScenarios(weighted, exitPE *float64) []ScenarioCase {
	g, pe := fallbackGrowth, fallbackExitPE
	if weighted != nil && isFinite(*weighted) {
		g = *weighted
	}
	if exitPE != nil && isFinite(*exitPE) {
		pe = *exitPE
	}
	// Rounded in percent, as the case editors show them.
	pctHalf := func(v float64) float64 { return jsRound(v*2) / 2 / 100 }
	return []ScenarioCase{
		{Name: "Bear", Growth: pctHalf((g - 0.06) * 100), ExitPE: roundHalf(math.Max(6, pe*0.75))},
		{Name: "Base", Growth: pctHalf(g * 100), ExitPE: roundHalf(pe)},
		{Name: "Bull", Growth: pctHalf((g + 0.06) * 100), ExitPE: roundHalf(pe * 1.2)},
	}
}

// ScenarioSet is the outcome of the bear, base and bull cases.
type ScenarioSet struct {
	Cases []ScenarioOutcome `json:"cases"`
	// MeetingTarget names the cases whose yearly return reaches the target
	// return, in case order.
	MeetingTarget     []string `json:"meetingTarget"`
	WorstYearlyReturn Number   `json:"worstYearlyReturn"`
}

// ScenarioOutcome is one case with its result.
type ScenarioOutcome struct {
	ScenarioCase
	Result Maybe[ScenarioResult] `json:"result"`
}

// ScenarioResult is the outcome of one case.
type ScenarioResult struct {
	EPS          float64 `json:"eps"` // EPS at the exit
	ExitPrice    float64 `json:"exitPrice"`
	TotalReturn  float64 `json:"totalReturn"`
	YearlyReturn float64 `json:"yearlyReturn"`
	VsTarget     float64 `json:"vsTarget"`
	Tone         Tone    `json:"tone"`
	MeetsTarget  bool    `json:"meetsTarget"`
}

// Scenarios returns the outcome of each case. A result is missing when EPS
// or the case's exit P/E is not positive, or when its growth is -100% or
// lower or above 1000%; WorstYearlyReturn skips missing results.
func Scenarios(inv Investment, cases []ScenarioCase) ScenarioSet {
	out := ScenarioSet{Cases: make([]ScenarioOutcome, len(cases)), MeetingTarget: []string{}}
	worst := math.Inf(1)
	for i, c := range cases {
		out.Cases[i] = ScenarioOutcome{ScenarioCase: c, Result: None[ScenarioResult](ReasonNeedsPositiveEPS)}
		switch {
		case !(inv.EPS > 0):
			continue
		case !growthInRange(c.Growth):
			out.Cases[i].Result = None[ScenarioResult](ReasonGrowthOutOfRange)
			continue
		case !(c.ExitPE > 0):
			continue
		}
		p := Project(inv, c.Growth, c.ExitPE)
		diff := p.YearlyReturn - inv.TargetReturn
		r := ScenarioResult{
			EPS:          inv.EPS * math.Pow(1+c.Growth, float64(inv.Years)),
			ExitPrice:    p.ExitPrice,
			TotalReturn:  p.TotalReturn,
			YearlyReturn: p.YearlyReturn,
			VsTarget:     diff,
			Tone:         ToneOf(diff),
			MeetsTarget:  p.YearlyReturn >= inv.TargetReturn,
		}
		out.Cases[i].Result = Some(r)
		if r.MeetsTarget {
			out.MeetingTarget = append(out.MeetingTarget, c.Name)
		}
		worst = math.Min(worst, r.YearlyReturn)
	}
	switch {
	case !math.IsInf(worst, 1):
		out.WorstYearlyReturn = Some(worst)
	case len(cases) > 0:
		out.WorstYearlyReturn = None[float64](out.Cases[0].Result.Reason)
	case !(inv.EPS > 0):
		out.WorstYearlyReturn = None[float64](ReasonNeedsPositiveEPS)
	default:
		out.WorstYearlyReturn = None[float64](ReasonInsufficientHistory)
	}
	return out
}
