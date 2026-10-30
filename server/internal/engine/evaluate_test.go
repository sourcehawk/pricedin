package engine

import (
	"encoding/json"
	"errors"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

var examplePath = filepath.Join("..", "..", "..", "testdata", "engine", "evaluation-example.json")

type example struct {
	Input struct {
		History History `json:"history"`
		Levers  Levers  `json:"levers"`
	} `json:"input"`
	Output json.RawMessage `json:"output"`
}

func loadExample(t *testing.T) example {
	t.Helper()
	raw, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("read example: %v", err)
	}
	var ex example
	if err := json.Unmarshal(raw, &ex); err != nil {
		t.Fatalf("decode example: %v", err)
	}
	return ex
}

// The committed example is the oracle's evaluation of the GRMN demo profile
// with every daily close; Evaluate must reproduce it.
func TestEvaluateReproducesTheExample(t *testing.T) {
	ex := loadExample(t)
	ev, err := Evaluate(ex.Input.History, ex.Input.Levers)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got, want any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(ex.Output, &want); err != nil {
		t.Fatal(err)
	}
	var diffs []string
	compareJSON("$", want, got, 1e-9, &diffs)
	if len(diffs) > 0 {
		t.Errorf("Evaluate differs from the example:\n%s", joinLines(diffs[:min(10, len(diffs))]))
	}
	near(t, "required growth of the brief's reference", wantSome(t, "required", ev.Required.Growth), 0.239, 5e-4)
}

func TestEvaluateRejectsInvalidInput(t *testing.T) {
	ex := loadExample(t)
	tests := []struct {
		name   string
		change func(*Levers, *History)
	}{
		{"zero price", func(l *Levers, _ *History) { l.Price = 0 }},
		{"NaN EPS", func(l *Levers, _ *History) { l.TTMEPS = math.NaN() }},
		{"negative dividend", func(l *Levers, _ *History) { l.Dividend = -1 }},
		{"negative target", func(l *Levers, _ *History) { l.TargetReturn = -0.01 }},
		{"target above 50%", func(l *Levers, _ *History) { l.TargetReturn = 0.51 }},
		{"price above a million", func(l *Levers, _ *History) { l.Price = 2e6 }},
		{"EPS too close to zero", func(l *Levers, _ *History) { l.TTMEPS = 1e-9 }},
		{"dividend above a million", func(l *Levers, _ *History) { l.Dividend = 2e6 }},
		{"zero custom exit P/E", func(l *Levers, _ *History) { l.ExitPE = ExitPELever{Mode: ExitPECustom} }},
		{"custom exit P/E below 0.5", func(l *Levers, _ *History) { l.ExitPE = ExitPELever{Mode: ExitPECustom, Custom: 1e-310} }},
		{"negative custom exit P/E", func(l *Levers, _ *History) { l.ExitPE = ExitPELever{Mode: ExitPECustom, Custom: -5} }},
		{"custom exit P/E above 1000", func(l *Levers, _ *History) { l.ExitPE = ExitPELever{Mode: ExitPECustom, Custom: 1001} }},
		{"custom rate above 200%", func(l *Levers, _ *History) { l.Baseline.Custom.Rate = 2.01 }},
		{"look-back above 120 months", func(l *Levers, _ *History) { l.Baseline.TrailingMonths = 121 }},
		{"case growth above 10^30", func(l *Levers, _ *History) { l.Scenarios = []ScenarioCase{{Name: "Bull", Growth: 2e30, ExitPE: 10}} }},
		{"case exit P/E above 10^30", func(l *Levers, _ *History) { l.Scenarios = []ScenarioCase{{Name: "Bull", Growth: 0.1, ExitPE: 2e30}} }},
		{"NaN quarter EPS", func(_ *Levers, h *History) { h.Quarters[3].EPS = ptr(math.NaN()) }},
		{"infinite revenue", func(_ *Levers, h *History) { h.Quarters[18].Revenue = ptr(math.Inf(1)) }},
		{"revenue above 10^15", func(_ *Levers, h *History) { h.Quarters[18].Revenue = ptr(2e15) }},
		{"infinite daily EPS", func(_ *Levers, h *History) { h.Daily[4].TTMEPS = ptr(math.Inf(-1)) }},
		{"zero years", func(l *Levers, _ *History) { l.HoldingYears = 0 }},
		{"eleven years", func(l *Levers, _ *History) { l.HoldingYears = 11 }},
		{"unknown exit P/E mode", func(l *Levers, _ *History) { l.ExitPE.Mode = "forward" }},
		{"infinite custom exit P/E", func(l *Levers, _ *History) { l.ExitPE = ExitPELever{Mode: ExitPECustom, Custom: math.Inf(1)} }},
		{"unknown baseline mode", func(l *Levers, _ *History) { l.Baseline.Mode = "analyst" }},
		{"yearly rate below -50%", func(l *Levers, _ *History) { l.Baseline.Custom.Rates = []float64{0.1, -0.51} }},
		{"negative look-back", func(l *Levers, _ *History) { l.ExitPE.TrailingMonths = -3 }},
		{"unknown chart range", func(l *Levers, _ *History) { l.ChartRange = "5Y" }},
		{"days out of order", func(_ *Levers, h *History) { h.Daily[3].Date = h.Daily[2].Date }},
		{"a zero close", func(_ *Levers, h *History) { h.Daily[5].Close = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := ex.Input.Levers
			h := History{Quarters: append([]Quarter(nil), ex.Input.History.Quarters...), Daily: append([]PricePoint(nil), ex.Input.History.Daily...)}
			tt.change(&l, &h)
			if _, err := Evaluate(h, l); !errors.Is(err, ErrInvalidInput) {
				t.Errorf("Evaluate error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

// Every combination of edge levers and histories gives an evaluation that
// marshals to JSON: no NaN or Inf anywhere.
func TestEvaluateAlwaysMarshals(t *testing.T) {
	ex := loadExample(t)
	full := ex.Input.History
	histories := map[string]History{
		"full":        full,
		"none":        {},
		"one":         {Quarters: full.Quarters[:1]},
		"short":       {Quarters: full.Quarters[len(full.Quarters)-6:], Daily: full.Daily[len(full.Daily)-30:]},
		"losses":      {Quarters: quarters(-1, -1, -0.5, -0.2, -0.3, 0.1, -0.1, 0.2, 0.4, -2, 0.1, 0.3)},
		"missing":     {Quarters: quarters(1, math.NaN(), 1, 1, 1.1, 1.1, math.NaN(), 1.2, 1.3)},
		"one day":     {Daily: []PricePoint{{Date: domain.NewDate(2026, 1, 2), Close: 10}}},
		"EPS to zero": {Quarters: full.Quarters, Daily: zeroLastDay(full.Daily)},
	}
	levers := map[string]func(Levers) Levers{
		"default":      func(l Levers) Levers { return l },
		"negative EPS": func(l Levers) Levers { l.TTMEPS = -3; return l },
		"zero EPS":     func(l Levers) Levers { l.TTMEPS = 0; return l },
		"big dividend": func(l Levers) Levers { l.Dividend = 200; return l },
		"ten years":    func(l Levers) Levers { l.HoldingYears = 10; return l },
		"today P/E":    func(l Levers) Levers { l.ExitPE.Mode = ExitPEToday; return l },
		"trailing P/E": func(l Levers) Levers { l.ExitPE.Mode = ExitPETrailing; return l },
		"today P/E 0":  func(l Levers) Levers { l.Price, l.TTMEPS, l.ExitPE.Mode = 1, 10, ExitPEToday; return l },
		"trailing":     func(l Levers) Levers { l.Baseline.Mode = BaselineTrailing; return l },
		"today":        func(l Levers) Levers { l.Baseline.Mode = BaselineToday; return l },
		"year by year": func(l Levers) Levers {
			l.Baseline = BaselineLever{Mode: BaselineCustom, Custom: CustomGrowth{Yearly: true, Rates: []float64{2, -0.5}}}
			return l
		},
		"zero target": func(l Levers) Levers { l.TargetReturn = 0; return l },
		"6M chart":    func(l Levers) Levers { l.ChartRange = Range6M; return l },
		"empty range": func(l Levers) Levers { l.ChartRange = ""; return l },
		"user cases": func(l Levers) Levers {
			l.Scenarios = []ScenarioCase{{Name: "Only", Growth: -0.99, ExitPE: -5}}
			return l
		},
		"tiny price":    func(l Levers) Levers { l.Price = 0.01; return l },
		"huge exit P/E": func(l Levers) Levers { l.ExitPE = ExitPELever{Mode: ExitPECustom, Custom: 1000}; return l },
		"tiny exit P/E": func(l Levers) Levers { l.ExitPE = ExitPELever{Mode: ExitPECustom, Custom: 0.5}; return l },
		"big EPS, tiny exit P/E": func(l Levers) Levers {
			l.Price, l.TTMEPS, l.ExitPE = 1, 1e6, ExitPELever{Mode: ExitPECustom, Custom: 0.5}
			return l
		},
	}
	for hn, h := range histories {
		for ln, change := range levers {
			ev, err := Evaluate(h, change(ex.Input.Levers))
			if err != nil {
				t.Errorf("%s / %s: %v", hn, ln, err)
				continue
			}
			if _, err := json.Marshal(ev); err != nil {
				t.Errorf("%s / %s: marshal: %v", hn, ln, err)
			}
		}
	}
}

func TestEvaluateWhenDividendsReachTheTarget(t *testing.T) {
	ex := loadExample(t)
	l := ex.Input.Levers
	l.Price, l.Dividend = 50, 30
	ev, err := Evaluate(ex.Input.History, l)
	if err != nil {
		t.Fatal(err)
	}
	wantNone(t, "required growth", ev.Required.Growth, ReasonDividendsReachTarget)
	wantNone(t, "gap", ev.Gap, ReasonDividendsReachTarget)
	if v := wantSome(t, "verdict", ev.Verdict); v != VerdictUndemanding {
		t.Errorf("verdict = %q, want undemanding", v)
	}
}

func TestEvaluateWithNegativeEPS(t *testing.T) {
	ex := loadExample(t)
	l := ex.Input.Levers
	l.TTMEPS = -2
	ev, err := Evaluate(ex.Input.History, l)
	if err != nil {
		t.Fatal(err)
	}
	for name, r := range map[string]Reason{
		"pe now":        ev.PENow.Reason,
		"required":      ev.Required.Growth.Reason,
		"verdict":       ev.Verdict.Reason,
		"at baseline":   ev.AtBaseline.Reason,
		"return grid":   ev.ReturnGrid.Reason,
		"paths":         ev.EarningsPaths.Reason,
		"sensitivity":   ev.Sensitivity.Reason,
		"exit P/E used": ev.ExitPE.InUse.Reason,
	} {
		if r != ReasonNeedsPositiveEPS {
			t.Errorf("%s reason = %q, want needs_positive_eps", name, r)
		}
	}
	// The history still shows.
	wantSome(t, "weighted trend", ev.Baselines.Weighted)
	wantSome(t, "price chart", ev.PriceChart)
}

// A custom exit P/E of zero or less is invalid input; today's P/E can still
// round to zero, and then the exit P/E in use is missing.
func TestEvaluateTreatsAnExitPEOfZeroAsMissing(t *testing.T) {
	ex := loadExample(t)
	l := ex.Input.Levers
	l.Price, l.TTMEPS, l.ExitPE = 1, 10, ExitPELever{Mode: ExitPEToday}
	ev, err := Evaluate(ex.Input.History, l)
	if err != nil {
		t.Fatal(err)
	}
	wantNone(t, "exit P/E in use", ev.ExitPE.InUse, ReasonNeedsPositiveEPS)
	wantNone(t, "at baseline", ev.AtBaseline, ReasonNeedsPositiveEPS)
	wantNone(t, "return grid", ev.ReturnGrid, ReasonNeedsPositiveEPS)
	wantNone(t, "required", ev.Required.Growth, ReasonNeedsPositiveEPS)
	wantNone(t, "sensitivity", ev.Sensitivity, ReasonNeedsPositiveEPS)
}

// zeroLastDay returns daily with a TTM EPS of exactly 0 on the last day.
func zeroLastDay(daily []PricePoint) []PricePoint {
	out := append([]PricePoint(nil), daily...)
	out[len(out)-1].TTMEPS = ptr(0.0)
	return out
}

// Extreme values at the edges of the input bounds never put NaN or Inf in
// the output.
func TestEvaluateMarshalsExtremeValidInputs(t *testing.T) {
	rnd := rand.New(rand.NewPCG(3, 21))
	pick := func(vs ...float64) float64 { return vs[rnd.IntN(len(vs))] }
	perShare := func() float64 { return pick(0, 1e-4, -1e-4, 1.0001e-4, 1e6, -1e6, 999999.9999, 0.5, -0.5) }
	for run := range 300 {
		n := 2 + rnd.IntN(20)
		qs := make([]Quarter, n)
		for i := range qs {
			qs[i] = Quarter{
				Label: "Q1 FY20", PeriodEnd: domain.NewDate(2020, 3, 31).AddDays(91 * i), EPS: ptr(perShare()), Close: ptr(pick(1e-4, 1e6, 3)),
				DilutedShares: ptr(pick(0, 1e-6, 1e15)), Revenue: ptr(pick(0, 1e-6, -1e-6, 1e15, -1e15)), OperatingIncome: ptr(pick(1e-6, -1e15, 1e15)),
				OperatingCosts: ptr(pick(0, 1e15)), FreeCashFlow: ptr(pick(-1e15, 1e-6)),
			}
		}
		daily := make([]PricePoint, 30+rnd.IntN(500))
		for i := range daily {
			daily[i] = PricePoint{Date: domain.NewDate(2023, 1, 1).AddDays(i), Close: pick(1e-4, 1e6, 7), TTMEPS: ptr(perShare())}
		}
		l := Levers{
			Price: pick(1e-4, 1e6, 1), TTMEPS: perShare(), Dividend: pick(0, 1e6), TargetReturn: pick(0, 0.5),
			HoldingYears: 1 + rnd.IntN(10),
			ExitPE: ExitPELever{
				Mode:   []ExitPEMode{ExitPEEstimated, ExitPEToday, ExitPETrailing, ExitPECustom}[rnd.IntN(4)],
				Custom: pick(0.5, 1000), TrailingMonths: rnd.IntN(121),
			},
			Baseline: BaselineLever{
				Mode:           []BaselineMode{BaselineWeighted, BaselineToday, BaselineTrailing, BaselineCustom}[rnd.IntN(4)],
				TrailingMonths: rnd.IntN(121),
				Custom:         CustomGrowth{Yearly: rnd.IntN(2) == 0, Rate: pick(-0.5, 2), Rates: []float64{pick(-0.5, 2), pick(-0.5, 2)}},
			},
			ChartRange: []ChartRange{Range6M, Range1Y, Range3Y}[rnd.IntN(3)],
		}
		if rnd.IntN(2) == 0 {
			l.Scenarios = []ScenarioCase{{Name: "Edge", Growth: pick(-0.999999, 10), ExitPE: pick(-1e4, 1e4, 0)}}
		}
		ev, err := Evaluate(History{Quarters: qs, Daily: daily}, l)
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if _, err := json.Marshal(ev); err != nil {
			t.Fatalf("run %d: marshal: %v\nlevers: %+v", run, err, l)
		}
		// The UI pins the default cases and evaluates again with them.
		pinned := l
		pinned.Scenarios = nil
		for _, c := range ev.Scenarios.Cases {
			pinned.Scenarios = append(pinned.Scenarios, c.ScenarioCase)
		}
		if _, err := Evaluate(History{Quarters: qs, Daily: daily}, pinned); err != nil {
			t.Fatalf("run %d: evaluate with the pinned cases: %v", run, err)
		}
	}
}

// A TTM EPS that cancels to near zero gives a trailing P/E near 1e14; the
// default cases built on it must still pass when the UI pins them.
func TestEvaluateAcceptsPinnedDefaultsAtAHugeTrailingPE(t *testing.T) {
	q := withCloses(quarters(0, 1.0001e-4, -1e-4, 0, 0, 1), 0, 0, 0, 1e6, 1e6, 0)
	l := Levers{Price: 1, TTMEPS: 1, TargetReturn: 0.15, HoldingYears: 3, ExitPE: ExitPELever{Mode: ExitPETrailing}, Baseline: BaselineLever{Mode: BaselineWeighted}}
	ev, err := Evaluate(History{Quarters: q}, l)
	if err != nil {
		t.Fatal(err)
	}
	if pe := wantSome(t, "exit P/E", ev.ExitPE.InUse).PE; pe < 1e12 {
		t.Fatalf("exit P/E %g, want a P/E above 10^12 for this test", pe)
	}
	for _, c := range ev.Scenarios.Cases {
		l.Scenarios = append(l.Scenarios, c.ScenarioCase)
	}
	if _, err := Evaluate(History{Quarters: q}, l); err != nil {
		t.Errorf("evaluate with the pinned cases: %v", err)
	}
}

// The lower-exit-P/E experiment never goes above the exit P/E in use, also
// at the lowest custom exit P/E.
func TestSensitivityLowerExitPEIsNotHigher(t *testing.T) {
	ex := loadExample(t)
	l := ex.Input.Levers
	l.Price, l.TTMEPS, l.ExitPE = 1, 10, ExitPELever{Mode: ExitPECustom, Custom: 0.5}
	ev, err := Evaluate(ex.Input.History, l)
	if err != nil {
		t.Fatal(err)
	}
	s := wantSome(t, "sensitivity", ev.Sensitivity)
	if got := *s.LowerExitPE.ExitPE; got != 0.5 {
		t.Errorf("lower exit P/E = %v, want 0.5", got)
	}
}
