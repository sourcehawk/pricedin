package engine

import (
	"math"
	"testing"
)

func TestEarningsPaths(t *testing.T) {
	q := quarters(grow(16, 0.1)...)
	in := PathsInput{Quarters: q, EPS: 10, Years: 2, Baseline: Some(0.1), RequiredEPS: Some(14.4)}
	p := wantSome(t, "paths", EarningsPaths(in))
	if len(p.History) != 13 || p.History[0].Label != q[3].Label {
		t.Errorf("history has %d quarters from %s, want 13 from %s", len(p.History), p.History[0].Label, q[3].Label)
	}
	if len(p.Future) != 9 {
		t.Fatalf("future has %d points, want 4 × 2 + 1", len(p.Future))
	}
	near(t, "baseline starts at today's EPS", wantSome(t, "baseline", p.Future[0].Baseline), 10, 0)
	near(t, "baseline ends at EPS × 1.1²", wantSome(t, "baseline", p.Future[8].Baseline), 12.1, 1e-12)
	near(t, "required ends at EPS × 1.2²", wantSome(t, "required", p.Future[8].Required), 14.4, 1e-12)
	near(t, "gap", wantSome(t, "gap", p.Future[8].Gap), 12.1-14.4, 1e-12)
	if tone := wantSome(t, "gap tone", p.GapTone); tone != ToneBad {
		t.Errorf("gap tone = %q, want bad when the requirement ends higher", tone)
	}
	if l := p.Future[1].Label; l == nil || *l != "Q1 FY24" {
		t.Errorf("the quarter after Q4 FY23 is %v, want Q1 FY24", l)
	}
}

// The scale covers every plotted point, also the dip inside a year-by-year
// path that ends higher than it starts.
func TestEarningsPathsScaleCoversEveryPoint(t *testing.T) {
	in := PathsInput{Quarters: quarters(grow(16, 0.1)...), EPS: 10, Years: 3, Baseline: Some(0.0), YearlyRates: []float64{-0.6, 0.5, 1.5}, RequiredEPS: Some(11.57625)}
	p := wantSome(t, "paths", EarningsPaths(in))
	for _, f := range p.Future {
		for _, m := range []Number{f.Baseline, f.Required} {
			if v := m.Get(); m.Ok() && (v < p.Scale.Min || v > p.Scale.Max) {
				t.Errorf("point %v at %d quarters is outside the scale %+v", v, f.QuartersAhead, p.Scale)
			}
		}
	}
	neg := quarters(-2, -2, -2, -2, -1, -1, -1, -1, 1, 1, 1, 1)
	pn := wantSome(t, "paths", EarningsPaths(PathsInput{Quarters: neg, EPS: 4, Years: 1, Baseline: Some(0.1), RequiredEPS: Some(4.4)}))
	if pn.Scale.Min > -8 {
		t.Errorf("scale %+v does not cover the TTM loss of -8", pn.Scale)
	}
}

func TestEarningsPathsWithoutValues(t *testing.T) {
	wantNone(t, "paths with negative EPS", EarningsPaths(PathsInput{EPS: -1, Years: 1}), ReasonNeedsPositiveEPS)
	p := wantSome(t, "paths", EarningsPaths(PathsInput{EPS: 5, Years: 1, Baseline: Some(0.1), RequiredEPS: None[float64](ReasonDividendsReachTarget)}))
	wantNone(t, "required path", p.Future[4].Required, ReasonDividendsReachTarget)
	wantNone(t, "gap tone", p.GapTone, ReasonDividendsReachTarget)
	if p.Future[0].Label != nil {
		t.Errorf("label without quarters = %q, want none", *p.Future[0].Label)
	}
}

func TestLabelAfter(t *testing.T) {
	tests := []struct {
		label string
		k     int
		want  string
	}{
		{"Q3 FY26", 0, "Q3 FY26"},
		{"Q3 FY26", 1, "Q4 FY26"},
		{"Q3 FY26", 2, "Q1 FY27"},
		{"Q4 FY99", 1, "Q1 FY00"},
		{"Q1 FY24", 12, "Q1 FY27"},
	}
	for _, tt := range tests {
		if got := labelAfter(tt.label, tt.k); got == nil || *got != tt.want {
			t.Errorf("labelAfter(%q, %d) = %v, want %q", tt.label, tt.k, got, tt.want)
		}
	}
	if got := labelAfter("FY2026 Q3", 1); got != nil {
		t.Errorf("an unknown label format gives %q, want none", *got)
	}
}

func TestHowDemanding(t *testing.T) {
	q := withCloses(quarters(grow(16, 0.1)...), 0, 0, 0, 10, 0, 50, 60, 70, 80, 90, 100, 110, 120, 130, 140, 150)
	inv := Investment{Price: 200, EPS: 6, TargetReturn: 0.15, Years: 3}
	d := HowDemanding(q, inv, Some(20.0))
	if len(d.Points) != 13 {
		t.Fatalf("%d points, want 13", len(d.Points))
	}
	last := d.Points[12]
	if !last.Today || wantSome(t, "price", last.Price) != 200 || wantSome(t, "ttm", last.TTMEPS) != 6 {
		t.Errorf("today's point = %+v, want the price and TTM EPS levers", last)
	}
	near(t, "required today", wantSome(t, "required", last.Required), wantSome(t, "", RequiredGrowth(inv, 20)), 0)
	wantNone(t, "required without a close", d.Points[1].Required, ReasonMissingData)
	if d.Compared != 9 {
		t.Errorf("compared = %d, want the 9 points with both values", d.Compared)
	}
	peak := wantSome(t, "peak", d.Peak)
	if peak.Index != 12 {
		t.Errorf("peak at %d, want today", peak.Index)
	}

	none := HowDemanding(q, inv, None[float64](ReasonNeedsPositiveEPS))
	wantNone(t, "required without an exit P/E", none.Points[3].Required, ReasonNeedsPositiveEPS)
	wantNone(t, "peak without any requirement", none.Peak, ReasonInsufficientHistory)
	wantNone(t, "typical without any requirement", none.TypicalRequired, ReasonInsufficientHistory)
}

// Two quarters at the same P/E need the same growth; rounding noise must not
// pick the later one as the peak.
func TestHowDemandingPeakTiesGoToTheFirst(t *testing.T) {
	q := withCloses(quarters(1, 1, 1, 1, 1, 1), 0, 0, 0, 100, 40, 0)
	d := HowDemanding(q, Investment{Price: 100.00000000000001, EPS: 4, TargetReturn: 0.1, Years: 3}, Some(20.0))
	if p := wantSome(t, "peak", d.Peak); p.Index != 3 {
		t.Errorf("peak at %d, want the first of two equal values", p.Index)
	}
	if math.Abs(wantSome(t, "first", d.Points[3].Required)-wantSome(t, "last", d.Points[5].Required)) > 1e-9 {
		t.Fatal("the test needs two equal requirements")
	}
}
