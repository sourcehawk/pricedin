package engine

import (
	"math"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %v, want %v (±%v)", name, got, want, tol)
	}
}

func wantNone[T any](t *testing.T, name string, m Maybe[T], r Reason) {
	t.Helper()
	if m.Ok() || m.Reason != r {
		t.Errorf("%s = %+v, want missing with reason %q", name, m, r)
	}
}

func wantSome[T any](t *testing.T, name string, m Maybe[T]) T {
	t.Helper()
	if !m.Ok() {
		t.Fatalf("%s is missing (%s), want a value", name, m.Reason)
	}
	return m.Get()
}

var grmn = Investment{Price: 270, EPS: 10.80, TargetReturn: 0.15, Years: 3}

func TestRequiredGrowth(t *testing.T) {
	tests := []struct {
		name   string
		inv    Investment
		exitPE float64
		want   float64
		tol    float64
	}{
		{"brief reference at 20x", grmn, 20, 0.239, 5e-4},
		{"brief reference at 10% and 22x", Investment{Price: 270, EPS: 10.80, TargetReturn: 0.10, Years: 3}, 22, 0.148, 5e-4},
		{"one year is the plain ratio", Investment{Price: 100, EPS: 5, TargetReturn: 0.1, Years: 1}, 20, 100*1.1/(5*20) - 1, 1e-12},
		{"a cheap stock needs negative growth", Investment{Price: 50, EPS: 10, TargetReturn: 0.1, Years: 2}, 15, math.Sqrt(50*1.21/150) - 1, 1e-12},
		{"dividends lower the requirement", Investment{Price: 100, EPS: 5, TargetReturn: 0.1, Years: 1, Dividend: 10}, 20, (110.0-10)/100 - 1, 1e-12},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			near(t, "required growth", wantSome(t, "required growth", RequiredGrowth(tt.inv, tt.exitPE)), tt.want, tt.tol)
		})
	}
}

func TestRequiredGrowthIsMissingWithAReason(t *testing.T) {
	tests := []struct {
		name   string
		inv    Investment
		exitPE float64
		want   Reason
	}{
		{"zero EPS", Investment{Price: 270, EPS: 0, TargetReturn: 0.15, Years: 3}, 20, ReasonNeedsPositiveEPS},
		{"negative EPS", Investment{Price: 270, EPS: -1, TargetReturn: 0.15, Years: 3}, 20, ReasonNeedsPositiveEPS},
		{"zero exit P/E", grmn, 0, ReasonNeedsPositiveEPS},
		{"negative exit P/E", grmn, -3, ReasonNeedsPositiveEPS},
		{"NaN exit P/E", grmn, math.NaN(), ReasonNeedsPositiveEPS},
		{"dividends alone reach the target", Investment{Price: 50, EPS: 2, TargetReturn: 0.1, Years: 3, Dividend: 30}, 20, ReasonDividendsReachTarget},
		{"dividends exactly reach the target", Investment{Price: 100, EPS: 2, TargetReturn: 0.25, Years: 1, Dividend: 125}, 20, ReasonDividendsReachTarget},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantNone(t, "required growth", RequiredGrowth(tt.inv, tt.exitPE), tt.want)
		})
	}
}

// Growing at the required rate and selling at the exit P/E earns exactly the
// target return, and the exit P/E needed at that growth is the exit P/E.
func TestRequiredGrowthInvertsProject(t *testing.T) {
	for _, years := range []int{1, 3, 10} {
		for _, target := range []float64{0, 0.08, 0.15, 0.4} {
			for _, exitPE := range []float64{6, 15, 22, 80} {
				for _, div := range []float64{0, 2.5} {
					inv := Investment{Price: 270, EPS: 10.8, TargetReturn: target, Years: years, Dividend: div}
					g := RequiredGrowth(inv, exitPE)
					if !g.Ok() {
						t.Fatalf("%+v at %v: required growth missing (%s)", inv, exitPE, g.Reason)
					}
					near(t, "yearly return at the required growth", Project(inv, g.Get(), exitPE).YearlyReturn, target, 1e-9)
					near(t, "exit P/E needed at the required growth", wantSome(t, "exit P/E needed", ExitPENeeded(inv, g.Get())), exitPE, 1e-9)
				}
			}
		}
	}
}

func TestProject(t *testing.T) {
	p := Project(Investment{Price: 270, EPS: 10.8, Years: 3}, 0.12, 22)
	near(t, "brief reference exit price", p.ExitPrice, 334, 0.5)
	near(t, "brief reference yearly return", p.YearlyReturn, 0.073, 5e-4)
	near(t, "total return", p.TotalReturn, p.ExitPrice/270-1, 1e-12)

	// Dividends count as cash received, not reinvested.
	d := Project(Investment{Price: 100, EPS: 5, Years: 2, Dividend: 3}, 0, 20)
	near(t, "total return with dividends", d.TotalReturn, (100.0+6)/100-1, 1e-12)
	near(t, "yearly return with dividends", d.YearlyReturn, math.Sqrt(1.06)-1, 1e-12)

	// EPS that falls to zero leaves only the dividends.
	z := Project(Investment{Price: 100, EPS: 5, Years: 2}, -1, 20)
	near(t, "yearly return when nothing is left", z.YearlyReturn, -1, 0)
}

func TestExitPENeeded(t *testing.T) {
	near(t, "one year", wantSome(t, "exit P/E needed", ExitPENeeded(Investment{Price: 100, EPS: 5, TargetReturn: 0.1, Years: 1}, 0.1)), 110/5.5, 1e-12)
	wantNone(t, "zero EPS", ExitPENeeded(Investment{Price: 100, EPS: 0, TargetReturn: 0.1, Years: 1}, 0.1), ReasonNeedsPositiveEPS)
	wantNone(t, "growth of -100%", ExitPENeeded(grmn, -1), ReasonGrowthOutOfRange)
	wantNone(t, "dividends reach the target", ExitPENeeded(Investment{Price: 50, EPS: 2, TargetReturn: 0.1, Years: 3, Dividend: 30}, 0.1), ReasonDividendsReachTarget)
}

func TestRequirement(t *testing.T) {
	r := Requirement(grmn, 20)
	g := wantSome(t, "growth", r.Growth)
	eps := wantSome(t, "eps", r.EPS)
	near(t, "EPS needed", eps, 10.8*(1+g)*(1+g)*(1+g), 1e-12)
	near(t, "exit price needed", wantSome(t, "exit price", r.ExitPrice), eps*20, 1e-9)
	near(t, "price change", wantSome(t, "price change", r.PriceChange), eps*20/270-1, 1e-12)
	near(t, "tight from", wantSome(t, "tight from", r.TightFrom), g-0.015, 1e-15)
	near(t, "undemanding from", wantSome(t, "undemanding from", r.UndemandingFrom), g+0.015, 1e-15)

	d := Requirement(Investment{Price: 50, EPS: 2, TargetReturn: 0.1, Years: 3, Dividend: 30}, 20)
	near(t, "dividends received", d.Dividends, 90, 0)
	for name, m := range map[string]Number{"growth": d.Growth, "exit price": d.ExitPrice, "eps": d.EPS, "price change": d.PriceChange} {
		wantNone(t, name, m, ReasonDividendsReachTarget)
	}
}

func TestToneAndVerdictUseThe15PPBand(t *testing.T) {
	tests := []struct {
		diff    float64
		tone    Tone
		verdict Verdict
	}{
		{0.0151, ToneGood, VerdictUndemanding},
		{0.015, ToneWarn, VerdictTight},
		{0, ToneWarn, VerdictTight},
		{-0.015, ToneWarn, VerdictTight},
		{-0.0151, ToneBad, VerdictDemanding},
	}
	for _, tt := range tests {
		if got := ToneOf(tt.diff); got != tt.tone {
			t.Errorf("ToneOf(%v) = %q, want %q", tt.diff, got, tt.tone)
		}
		if got := VerdictOf(tt.diff); got != tt.verdict {
			t.Errorf("VerdictOf(%v) = %q, want %q", tt.diff, got, tt.verdict)
		}
	}
}

func TestNiceStep(t *testing.T) {
	tests := []struct{ rng, want float64 }{
		{0, 1},
		{-3, 1},
		{math.NaN(), 1},
		{5, 1},
		{12.3, 2},
		{30, 5},
		{40, 10},
		{1000, 200},
		{0.07, 0.01},
	}
	for _, tt := range tests {
		near(t, "NiceStep", NiceStep(tt.rng, 5), tt.want, 1e-12)
	}
}

// A growth above 1000% a year comes from a near-zero EPS base; compounding
// it over ten years could overflow.
func TestGrowthAbove1000PercentIsOutOfRange(t *testing.T) {
	wantNone(t, "exit P/E needed", ExitPENeeded(grmn, 10.01), ReasonGrowthOutOfRange)
	near(t, "exit P/E needed at 1000%", wantSome(t, "exit P/E needed", ExitPENeeded(grmn, 10)), 270*1.15*1.15*1.15/(10.8*11*11*11), 1e-12)
	grid := wantSome(t, "grid", ReturnGrid(gridAt(BaselineWeighted, Some(1e30))))
	wantNone(t, "YOU cell", grid.You, ReasonGrowthOutOfRange)
	paths := wantSome(t, "paths", EarningsPaths(PathsInput{EPS: 5, Years: 10, Baseline: Some(1e30), RequiredEPS: Some(5 * math.Pow(1.1, 10))}))
	wantNone(t, "baseline path", paths.Future[40].Baseline, ReasonGrowthOutOfRange)
	set := Scenarios(grmn, []ScenarioCase{{Name: "Bull", Growth: 1e30, ExitPE: 20}})
	wantNone(t, "case", set.Cases[0].Result, ReasonGrowthOutOfRange)
}

// At a huge exit P/E the required growth rounds to -100%, but the price
// that the exit must deliver is still the target value.
func TestRequirementAtAHugeExitPE(t *testing.T) {
	r := Requirement(Investment{Price: 100, EPS: 4, TargetReturn: 0.15, Years: 1}, 7.38e25)
	near(t, "exit price needed", wantSome(t, "exit price", r.ExitPrice), 115, 1e-9)
	near(t, "EPS needed", wantSome(t, "eps", r.EPS), 115/7.38e25, 1e-30)
	near(t, "required growth", wantSome(t, "growth", r.Growth), -1, 1e-12)
}

// A gap computed to land on 1.5 pp is tight, not undemanding.
func TestToneAtAComputedCutoff(t *testing.T) {
	if got := ToneOf(0.165 - 0.15); got != ToneWarn {
		t.Errorf("ToneOf(0.165-0.15) = %q, want warn", got)
	}
	if got := ToneOf(0.135 - 0.15); got != ToneWarn {
		t.Errorf("ToneOf(0.135-0.15) = %q, want warn", got)
	}
}
