package engine

import (
	"encoding/json"
	"errors"
	"math"
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
		{"target of -100%", func(l *Levers, _ *History) { l.TargetReturn = -1 }},
		{"zero years", func(l *Levers, _ *History) { l.HoldingYears = 0 }},
		{"eleven years", func(l *Levers, _ *History) { l.HoldingYears = 11 }},
		{"unknown exit P/E mode", func(l *Levers, _ *History) { l.ExitPE.Mode = "forward" }},
		{"infinite custom exit P/E", func(l *Levers, _ *History) { l.ExitPE = ExitPELever{Mode: ExitPECustom, Custom: math.Inf(1)} }},
		{"unknown baseline mode", func(l *Levers, _ *History) { l.Baseline.Mode = "analyst" }},
		{"yearly rate of -100%", func(l *Levers, _ *History) { l.Baseline.Custom.Rates = []float64{0.1, -1} }},
		{"case growth of -150%", func(l *Levers, _ *History) { l.Scenarios = []ScenarioCase{{Name: "Bear", Growth: -1.5, ExitPE: 10}} }},
		{"negative look-back", func(l *Levers, _ *History) { l.ExitPE.TrailingMonths = -3 }},
		{"unknown chart range", func(l *Levers, _ *History) { l.ChartRange = "5Y" }},
		{"days out of order", func(_ *Levers, h *History) { h.Daily[3].Date = h.Daily[2].Date }},
		{"a zero close", func(_ *Levers, h *History) { h.Daily[5].Close = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := ex.Input.Levers
			h := History{Quarters: ex.Input.History.Quarters, Daily: append([]PricePoint(nil), ex.Input.History.Daily...)}
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
		"full":    full,
		"none":    {},
		"one":     {Quarters: full.Quarters[:1]},
		"short":   {Quarters: full.Quarters[len(full.Quarters)-6:], Daily: full.Daily[len(full.Daily)-30:]},
		"losses":  {Quarters: quarters(-1, -1, -0.5, -0.2, -0.3, 0.1, -0.1, 0.2, 0.4, -2, 0.1, 0.3)},
		"missing": {Quarters: quarters(1, math.NaN(), 1, 1, 1.1, 1.1, math.NaN(), 1.2, 1.3)},
		"one day": {Daily: []PricePoint{{Date: domain.NewDate(2026, 1, 2), Close: 10}}},
	}
	levers := map[string]func(Levers) Levers{
		"default":      func(l Levers) Levers { return l },
		"negative EPS": func(l Levers) Levers { l.TTMEPS = -3; return l },
		"zero EPS":     func(l Levers) Levers { l.TTMEPS = 0; return l },
		"big dividend": func(l Levers) Levers { l.Dividend = 200; return l },
		"ten years":    func(l Levers) Levers { l.HoldingYears = 10; return l },
		"today P/E":    func(l Levers) Levers { l.ExitPE.Mode = ExitPEToday; return l },
		"trailing P/E": func(l Levers) Levers { l.ExitPE.Mode = ExitPETrailing; return l },
		"zero custom":  func(l Levers) Levers { l.ExitPE = ExitPELever{Mode: ExitPECustom}; return l },
		"trailing":     func(l Levers) Levers { l.Baseline.Mode = BaselineTrailing; return l },
		"today":        func(l Levers) Levers { l.Baseline.Mode = BaselineToday; return l },
		"year by year": func(l Levers) Levers {
			l.Baseline = BaselineLever{Mode: BaselineCustom, Custom: CustomGrowth{Yearly: true, Rates: []float64{0.9, -0.9}}}
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
		"huge exit P/E": func(l Levers) Levers { l.ExitPE = ExitPELever{Mode: ExitPECustom, Custom: 1e6}; return l },
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
