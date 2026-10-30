package engine

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// withFinancials sets revenue, operating income, free cash flow and diluted
// shares per quarter; operating costs are revenue minus operating income.
func withFinancials(q []Quarter, rev, op, fcf, shares []float64) []Quarter {
	for i := range q {
		q[i].Revenue, q[i].OperatingIncome = ptr(rev[i]), ptr(op[i])
		q[i].OperatingCosts = ptr(rev[i] - op[i])
		q[i].FreeCashFlow, q[i].DilutedShares = ptr(fcf[i]), ptr(shares[i])
	}
	return q
}

func TestFinancialsTones(t *testing.T) {
	q := withFinancials(quarters(1, 1, 1, 1, 1.2, 1.2, 1.2, 1.2, 1.3, 1.3),
		[]float64{90, 90, 100, 100, 100, 100, 100, 100, 110, 110},
		[]float64{20, 20, 20, 20, 20, 20, 20, 20, 21, 21},
		[]float64{10, 10, 10, 10, 10, 10, 10, 10, 8, 8},
		[]float64{50, 50, 50, 50, 50, 50, 50, 50, 51, 51},
	)
	f := Financials(q)
	if len(f.Quarters) != 8 || f.Quarters[0].Label != q[2].Label {
		t.Fatalf("window = %+v, want the last 8 quarters", f.Quarters)
	}
	last := 7
	// Costs grew 11.25% against revenue's 10%: a squeeze.
	if tone := wantSome(t, "cost tone", f.Metrics[MetricOperatingCosts].YoYTone[last]); tone != ToneWarn {
		t.Errorf("cost tone = %q, want warn when costs outgrow revenue", tone)
	}
	// Rising share counts are never good.
	if tone := wantSome(t, "share tone", f.Metrics[MetricDilutedShares].YoYTone[last]); tone != ToneBad {
		t.Errorf("share tone = %q, want bad for a rising count", tone)
	}
	if tone := wantSome(t, "fcf tone", f.Metrics[MetricFreeCashFlow].YoYTone[last]); tone != ToneBad {
		t.Errorf("free cash flow tone = %q, want bad for a fall", tone)
	}
	if tone := wantSome(t, "share QoQ tone", f.Metrics[MetricDilutedShares].QoQTone[last]); tone != ToneNeutral {
		t.Errorf("share QoQ tone = %q, want neutral without a change", tone)
	}
	wantNone(t, "YoY in the first year of the window", f.Metrics[MetricRevenue].YoY[3], ReasonInsufficientHistory)

	tiles := wantSome(t, "tiles", f.Tiles)
	near(t, "revenue change", wantSome(t, "revenue", tiles.Revenue.Change), 0.1, 1e-12)
	near(t, "margin change", wantSome(t, "margin", tiles.OperatingMargin.Change), 21.0/110-0.2, 1e-12)
	if tone := wantSome(t, "shares tile", tiles.DilutedShares.Tone); tone != ToneBad {
		t.Errorf("shares tile tone = %q, want bad", tone)
	}
	if tone := wantSome(t, "costs tile", tiles.OperatingCosts.Tone); tone != ToneWarn {
		t.Errorf("costs tile tone = %q, want warn", tone)
	}

	scale := wantSome(t, "revenue scale", f.Metrics[MetricRevenue].Scale)
	near(t, "revenue scale minimum", scale.Min, 0, 0)
	near(t, "revenue scale maximum", scale.Max, 110*1.12, 1e-9)
	shares := wantSome(t, "share scale", f.Metrics[MetricDilutedShares].Scale)
	if shares.Min <= 49 || shares.Max >= 52 {
		t.Errorf("share scale = %+v, want a zoom on 50 to 51", shares)
	}
}

func TestFinancialsCompare(t *testing.T) {
	q := withFinancials(quarters(1, 1, 1, 1, 1, 1),
		[]float64{100, 110, 99, 120, 108, 130},
		[]float64{10, 11, 9.9, 12, 10.8, 13},
		[]float64{5, -5, 5, 5, 5, 5},
		[]float64{50, 50, 50, 50, 50, 50},
	)
	f := Financials(q)
	near(t, "revenue since the start", wantSome(t, "since", f.Compare.SinceStart[MetricRevenue][5]), 0.3, 1e-12)
	if f.Compare.Changes != 5 || len(f.Compare.Correlations) != 15 {
		t.Fatalf("changes, pairs = %d, %d, want 5, 15", f.Compare.Changes, len(f.Compare.Correlations))
	}
	for _, c := range f.Compare.Correlations {
		switch {
		case c.A == MetricRevenue && c.B == MetricOperatingIncome:
			r := wantSome(t, "revenue and operating income", c.Result)
			if r.Class != "strongSame" || r.Correlation < 0.999 {
				t.Errorf("revenue and operating income = %+v, want strongSame", r)
			}
		case c.B == MetricDilutedShares:
			wantNone(t, string(c.A)+" and shares", c.Result, ReasonZeroVariance)
		}
	}
	wantNone(t, "change from a negative base", f.Metrics[MetricFreeCashFlow].QoQ[2], ReasonNonPositiveBase)
}

func TestFinancialsShortHistory(t *testing.T) {
	f := Financials(quarters(1, 1, 1))
	wantNone(t, "tiles", f.Tiles, ReasonInsufficientHistory)
	wantNone(t, "revenue scale", f.Metrics[MetricRevenue].Scale, ReasonMissingData)
	wantNone(t, "margin", f.OperatingMargin[0], ReasonMissingData)
}

func TestCompareScale(t *testing.T) {
	got := CompareScale([]*float64{ptr(0.1), ptr(-0.05), nil, ptr(0.32)})
	if diff := cmp.Diff(Axis{Min: -0.1, Max: 0.4, Step: 0.1}, got, cmpopts.EquateApprox(0, 1e-12)); diff != "" {
		t.Errorf("CompareScale (-want +got):\n%s", diff)
	}
	if got := CompareScale(nil); got.Max <= got.Min {
		t.Errorf("CompareScale of nothing = %+v, want a visible range", got)
	}
}
