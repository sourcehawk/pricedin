package engine

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// quarters returns one quarter per EPS value, labeled from Q1 FY20 on; a NaN
// EPS is not reported.
func quarters(eps ...float64) []Quarter {
	out := make([]Quarter, len(eps))
	for i, e := range eps {
		out[i] = Quarter{
			Label: fmt.Sprintf("Q%d FY%02d", i%4+1, 20+i/4),
			// Day 0 of the next month is the last day of the quarter.
			PeriodEnd: domain.NewDate(2020+i/4, time.Month(3*(i%4)+4), 0),
		}
		if !math.IsNaN(e) {
			out[i].EPS = ptr(e)
		}
	}
	return out
}

// grow returns n quarters of EPS that grow by g a year, starting at 1.
func grow(n int, g float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = math.Pow(1+g, float64(i)/4)
	}
	return out
}

func TestQuarterSeries(t *testing.T) {
	s := QuarterSeries(quarters(1, 1, 1, 1, 1.1, 1.2, 1, 1.5))
	near(t, "TTM at Q4", wantSome(t, "ttm[3]", s[3].TTMEPS), 4, 1e-12)
	near(t, "TTM at Q8", wantSome(t, "ttm[7]", s[7].TTMEPS), 4.8, 1e-12)
	near(t, "YoY at Q8", wantSome(t, "yoy[7]", s[7].YoY), 0.5, 1e-12)
	near(t, "TTM YoY at Q8", wantSome(t, "ttmYoy[7]", s[7].TTMYoY), 0.2, 1e-12)
	w := []float64{1, math.Pow(0.5, 0.5), 0.5, math.Pow(0.5, 1.5)}
	want := (0.5*w[0] + 0*w[1] + 0.2*w[2] + 0.1*w[3]) / (w[0] + w[1] + w[2] + w[3])
	near(t, "weighted trend halves the weight every 2 quarters", wantSome(t, "wtd[7]", s[7].WeightedTrend), want, 1e-12)

	wantNone(t, "TTM before four quarters", s[2].TTMEPS, ReasonInsufficientHistory)
	wantNone(t, "YoY before five quarters", s[3].YoY, ReasonInsufficientHistory)
	wantNone(t, "TTM YoY before eight quarters", s[6].TTMYoY, ReasonInsufficientHistory)
	wantNone(t, "weighted trend without any YoY", s[3].WeightedTrend, ReasonInsufficientHistory)
}

func TestQuarterSeriesWithMissingAndNegativeEPS(t *testing.T) {
	s := QuarterSeries(quarters(-1, 1, 1, 1, 1.2, math.NaN(), 1, 1, 1.5))
	wantNone(t, "YoY against a loss", s[4].YoY, ReasonNeedsPositiveEPS)
	wantNone(t, "YoY of a missing quarter", s[5].YoY, ReasonMissingData)
	wantNone(t, "TTM over a missing quarter", s[8].TTMEPS, ReasonMissingData)
	wantNone(t, "TTM YoY over a missing TTM", s[8].TTMYoY, ReasonMissingData)
	// The missing quarters drop out of the weighted trend: only Q9 (0.25),
	// Q8 (0) and Q7 (0) count.
	want := 0.25 / (1 + math.Pow(0.5, 0.5) + 0.5)
	near(t, "weighted trend at Q9", wantSome(t, "wtd[8]", s[8].WeightedTrend), want, 1e-12)
}

func TestWeightedTrendOutOfRange(t *testing.T) {
	q := quarters(grow(9, 0.1)...)
	wantNone(t, "negative index", WeightedTrend(q, -1), ReasonInsufficientHistory)
	wantNone(t, "past the end", WeightedTrend(q, 9), ReasonInsufficientHistory)
	near(t, "steady growth", wantSome(t, "weighted", WeightedTrend(q, 8)), 0.1, 1e-12)
}

func TestTrailingGrowth(t *testing.T) {
	q := quarters(grow(20, 0.2)...) // 20 quarters: TTM from index 3 to 19
	tests := []struct {
		months, want int
	}{
		{0, 12},  // the default
		{13, 12}, // rounded to a multiple of 3
		{14, 15},
		{3, 6},   // at least 6
		{60, 48}, // limited by the history: 3 × (19 − 3)
		{48, 48},
	}
	for _, tt := range tests {
		got := TrailingGrowth(q, tt.months)
		if got.Months != tt.want || got.MaxMonths != 48 {
			t.Errorf("TrailingGrowth(%d months) uses %d of %d months, want %d of 48", tt.months, got.Months, got.MaxMonths, tt.want)
		}
		near(t, fmt.Sprintf("growth over %d months", tt.months), wantSome(t, "trailing", got.Growth), 0.2, 1e-9)
	}

	short := TrailingGrowth(quarters(grow(6, 0.2)...), 12)
	if short.Months != 6 || short.MaxMonths != 6 {
		t.Errorf("short history uses %d of %d months, want 6 of 6", short.Months, short.MaxMonths)
	}
	tiny := TrailingGrowth(quarters(grow(5, 0.2)...), 12)
	wantNone(t, "trailing growth without a start TTM", tiny.Growth, ReasonInsufficientHistory)

	loss := quarters(-1, -1, -1, -1, 1, 1, 1, 1)
	wantNone(t, "trailing growth from a loss", TrailingGrowth(loss, 12).Growth, ReasonNeedsPositiveEPS)
}

func TestTrajectoryFill(t *testing.T) {
	// The weighted trend sped up over the last year, so the rates rise
	// toward the highest reading and stop there.
	eps := []float64{1, 1, 1, 1, 1.05, 1.05, 1.05, 1.05, 1.2, 1.25, 1.3, 1.4}
	tr := wantSome(t, "trajectory", TrajectoryFill(quarters(eps...), 4))
	if tr.Slope <= 0 {
		t.Fatalf("slope = %v, want a speed-up", tr.Slope)
	}
	for i, r := range tr.Rates {
		if r > math.Floor(tr.Highest*200)/200+1e-12 {
			t.Errorf("rate %d = %v is above the highest reading %v", i, r, tr.Highest)
		}
		if math.Abs(r*200-math.Round(r*200)) > 1e-9 {
			t.Errorf("rate %d = %v is not rounded to 0.5 pp", i, r)
		}
	}

	// A collapse is never projected below -50%.
	fall := wantSome(t, "trajectory", TrajectoryFill(quarters(1, 1, 1, 1, 0.9, 0.8, 0.5, 0.2, 0.1, 0.05, 0.02, 0.01), 3))
	for _, r := range fall.Rates {
		if r < -0.5 {
			t.Errorf("rate %v is below -50%%", r)
		}
	}
	if fall.Capped {
		t.Error("a slowing trend is never capped")
	}

	wantNone(t, "trajectory without a weighted trend", TrajectoryFill(quarters(1, 1, 1, 1), 3), ReasonInsufficientHistory)
}

func TestYearRatesAndAverage(t *testing.T) {
	if diff := cmp.Diff([]float64{0.2, 0.15, 0.15, 0.15}, YearRates(CustomGrowth{Rate: 0.1, Rates: []float64{0.2, 0.15}}, 4)); diff != "" {
		t.Errorf("padding repeats the last rate (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]float64{0.08, 0.08}, YearRates(CustomGrowth{Rate: 0.08}, 2)); diff != "" {
		t.Errorf("no rates start from the flat rate (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]float64{0.3}, YearRates(CustomGrowth{Rates: []float64{0.3, 0.2}}, 1)); diff != "" {
		t.Errorf("rates past the holding period are dropped (-want +got):\n%s", diff)
	}
	near(t, "compound average", YearlyAverage([]float64{-0.5, 0.5}), math.Sqrt(0.75)-1, 1e-12)
	near(t, "one rate", YearlyAverage([]float64{0.07}), 0.07, 1e-12)
}

func TestBaselinePath(t *testing.T) {
	near(t, "flat path after a year", BaselinePath(10, 0.2, 4, nil), 12, 1e-12)
	near(t, "flat path compounds within a year", BaselinePath(10, 0.21, 2, nil), 11, 1e-12)
	rates := []float64{0.3, -0.1, 0.15}
	near(t, "year path after two years", BaselinePath(10, 0, 8, rates), 10*1.3*0.9, 1e-12)
	near(t, "year path in the second year", BaselinePath(10, 0, 6, rates), 10*1.3*math.Sqrt(0.9), 1e-12)
	near(t, "year path ignores g", BaselinePath(10, 0.99, 12, rates), 10*1.3*0.9*1.15, 1e-12)
}

func TestGrowthScale(t *testing.T) {
	opt := cmpopts.EquateApprox(0, 1e-12)
	if diff := cmp.Diff(Axis{Min: -0.05, Max: 0.35, Step: 0.05}, GrowthScale([]*float64{ptr(0.149), nil, ptr(0.13)}), opt); diff != "" {
		t.Errorf("a small range always spans 0%% to 30%% (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(Axis{Min: -0.4, Max: 1.0, Step: 0.2}, GrowthScale([]*float64{ptr(0.95), ptr(-0.35)}), opt); diff != "" {
		t.Errorf("a wide range steps by 20 pp (-want +got):\n%s", diff)
	}
}

// The trajectory fills the custom year-by-year rates, so its rates stay
// within the custom-rate bounds, also when the highest reading is lower.
func TestTrajectoryFillStaysWithinTheCustomRateBounds(t *testing.T) {
	tr := wantSome(t, "trajectory", TrajectoryFill(quarters(1, 1, 1, 1, 0.1), 3))
	for _, r := range tr.Rates {
		if r != -0.5 {
			t.Errorf("rate %v, want -50%% when the highest reading is %v", r, tr.Highest)
		}
	}
	up := wantSome(t, "trajectory", TrajectoryFill(quarters(0.1, 0.1, 0.1, 0.1, 1, 1, 1, 1, 10), 3))
	for _, r := range up.Rates {
		if r > 2 {
			t.Errorf("rate %v is above 200%%", r)
		}
	}
}

// A huge growth still gives a scale with a handful of ticks.
func TestGrowthScaleKeepsTheTickCountBounded(t *testing.T) {
	a := GrowthScale([]*float64{ptr(0.15), ptr(999999.0)})
	if n := (a.Max - a.Min) / a.Step; n > 12 {
		t.Errorf("scale %+v has %.0f steps, want at most 12", a, n)
	}
}
