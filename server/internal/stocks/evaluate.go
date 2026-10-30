package stocks

import (
	"context"
	"fmt"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/engine"
)

// LeversInput are the levers of an evaluation. A nil field takes its
// default: the stock's latest close, TTM EPS and TTM dividend, a 15% target
// return, three years, the estimated exit P/E, the weighted baseline, the
// default cases and the 3Y chart range.
type LeversInput struct {
	Price        *float64              `json:"price"`
	TTMEPS       *float64              `json:"ttmEps"`
	Dividend     *float64              `json:"dividend"`
	TargetReturn *float64              `json:"targetReturn"`
	HoldingYears *int                  `json:"holdingYears"`
	ExitPE       *engine.ExitPELever   `json:"exitPe"`
	Baseline     *engine.BaselineLever `json:"baseline"`
	Scenarios    []ScenarioInput       `json:"scenarios"`
	ChartRange   *engine.ChartRange    `json:"chartRange"`
}

// ScenarioInput is one bear, base or bull case of the levers. Every field
// is required; a nil field is missing or null.
type ScenarioInput struct {
	Name   *string  `json:"name"`
	Growth *float64 `json:"growth"`
	ExitPE *float64 `json:"exitPe"`
}

// The defaults of the levers that do not come from the stock.
const (
	DefaultTargetReturn = 0.15
	DefaultHoldingYears = 3
)

// InputSources are the sources of the per-share inputs of an evaluation.
type InputSources struct {
	Price    SourcedNumber `json:"price"`
	TTMEPS   SourcedNumber `json:"ttmEps"`
	Dividend SourcedNumber `json:"dividend"`
}

// Evaluation is the engine output for a stock and a set of levers.
type Evaluation struct {
	Ticker string `json:"ticker"`
	// Levers are the levers the engine ran with, defaults filled in.
	Levers  engine.Levers `json:"levers"`
	Sources InputSources  `json:"sources"`
	// AsOf is the newest as-of date of the inputs: the sources, the newest
	// filing and the newest close.
	AsOf       domain.Date       `json:"asOf"`
	Evaluation engine.Evaluation `json:"evaluation"`
}

// Evaluate runs the engine on the stock's full history with the levers. It
// gives ErrNotLoaded for a company that is not loaded, a
// *MissingInputError when a price or TTM EPS is neither given nor stored,
// and an error wrapping engine.ErrInvalidInput for levers out of bounds
// and for a case without a name, growth or exit P/E.
func (s *Service) Evaluate(ctx context.Context, ticker string, in LeversInput) (Evaluation, error) {
	st, err := s.open(ctx, ticker)
	if err != nil {
		return Evaluation{}, err
	}
	if err := s.withFundamentals(ctx, st); err != nil {
		return Evaluation{}, err
	}
	user := func(v float64) SourcedNumber {
		return SourcedNumber{Value: v, Source: UserSource{}, AsOf: st.today}
	}
	var src InputSources
	switch c := st.latestClose(); {
	case in.Price != nil:
		src.Price = user(*in.Price)
	case c != nil:
		src.Price = *c
	default:
		return Evaluation{}, &MissingInputError{Input: "price"}
	}
	switch t := st.ttmEPS(); {
	case in.TTMEPS != nil:
		src.TTMEPS = user(*in.TTMEPS)
	case t != nil:
		src.TTMEPS = *t
	default:
		return Evaluation{}, &MissingInputError{Input: "ttmEps"}
	}
	if in.Dividend != nil {
		src.Dividend = user(*in.Dividend)
	} else {
		src.Dividend = st.ttmDividend()
	}

	l := engine.Levers{
		Price: src.Price.Value, TTMEPS: src.TTMEPS.Value, Dividend: src.Dividend.Value,
		TargetReturn: DefaultTargetReturn, HoldingYears: DefaultHoldingYears,
		ExitPE:     engine.ExitPELever{Mode: engine.ExitPEEstimated},
		Baseline:   engine.BaselineLever{Mode: engine.BaselineWeighted},
		Scenarios:  make([]engine.ScenarioCase, 0, len(in.Scenarios)),
		ChartRange: engine.Range3Y,
	}
	if in.TargetReturn != nil {
		l.TargetReturn = *in.TargetReturn
	}
	if in.HoldingYears != nil {
		l.HoldingYears = *in.HoldingYears
	}
	if in.ExitPE != nil {
		l.ExitPE = *in.ExitPE
	}
	if in.Baseline != nil {
		l.Baseline = *in.Baseline
	}
	if in.ChartRange != nil && *in.ChartRange != "" {
		l.ChartRange = *in.ChartRange
	}
	for i, c := range in.Scenarios {
		for field, missing := range map[string]bool{"name": c.Name == nil, "growth": c.Growth == nil, "exitPe": c.ExitPE == nil} {
			if missing {
				return Evaluation{}, fmt.Errorf("stocks: evaluate %s: %w: scenarios[%d].%s is required", st.ticker.Symbol, engine.ErrInvalidInput, i, field)
			}
		}
		l.Scenarios = append(l.Scenarios, engine.ScenarioCase{Name: *c.Name, Growth: *c.Growth, ExitPE: *c.ExitPE})
	}
	if l.Baseline.Custom.Rates == nil {
		l.Baseline.Custom.Rates = []float64{}
	}
	if err := l.Validate(); err != nil {
		return Evaluation{}, fmt.Errorf("stocks: evaluate %s: %w", st.ticker.Symbol, err)
	}
	// The engine checks the custom exit P/E only in custom mode, but the
	// levers echo it in every mode, and the contract bounds it there too.
	if l.ExitPE.Custom != 0 && l.ExitPE.Mode != engine.ExitPECustom {
		probe := l
		probe.ExitPE.Mode = engine.ExitPECustom
		if err := probe.Validate(); err != nil {
			return Evaluation{}, fmt.Errorf("stocks: evaluate %s: %w", st.ticker.Symbol, err)
		}
	}

	h := st.history(RangeMax)
	ev, err := engine.Evaluate(h.History, l)
	if err != nil {
		// The levers are valid, so the stored history is at fault, not the
		// caller: the error must not read as engine.ErrInvalidInput.
		return Evaluation{}, fmt.Errorf("stocks: evaluate %s: stored history: %v", st.ticker.Symbol, err) //nolint:errorlint // see above
	}
	asOf := laterDate(laterDate(src.Price.AsOf, src.TTMEPS.AsOf), src.Dividend.AsOf)
	if f := h.Freshness; f.LatestFiled != nil {
		asOf = laterDate(asOf, *f.LatestFiled)
	}
	if f := h.Freshness; f.LatestClose != nil {
		asOf = laterDate(asOf, *f.LatestClose)
	}
	return Evaluation{Ticker: st.ticker.Symbol, Levers: l, Sources: src, AsOf: asOf, Evaluation: ev}, nil
}
