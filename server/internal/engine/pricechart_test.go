package engine

import (
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

func TestShareCountEvents(t *testing.T) {
	q := quarters(1, 1, 1, 1, 1)
	for i, s := range []float64{100, 101.9, 104, 0, 101} {
		if s != 0 {
			q[i].DilutedShares = ptr(s)
		}
	}
	got := ShareCountEvents(q)
	if len(got) != 1 {
		t.Fatalf("events = %+v, want only the 2%% rise", got)
	}
	if got[0].Kind != "dilution" || got[0].Label != q[2].Label || got[0].From != 101.9 || got[0].To != 104 {
		t.Errorf("event = %+v, want the dilution of Q3", got[0])
	}
	q[1].DilutedShares = ptr(102.1)
	if ev := ShareCountEvents(q); len(ev) != 1 || ev[0].Kind != "dilution" || ev[0].Label != q[1].Label {
		t.Errorf("events = %+v, want the 2.1%% rise of Q2 only", ev)
	}
	q[2].DilutedShares = ptr(100.0)
	ev := ShareCountEvents(q)
	if len(ev) != 2 || ev[1].Kind != "buyback" {
		t.Errorf("events = %+v, want a dilution and a buyback", ev)
	}
	if got := ShareCountEvents(nil); got == nil {
		t.Error("no quarters give a nil list, want an empty one")
	}
}

func series20(f func(i int) float64) []float64 {
	out := make([]float64, 20)
	for i := range out {
		out[i] = f(i)
	}
	return out
}

func TestCorrelation(t *testing.T) {
	xs := series20(func(i int) float64 { return float64(i) })
	tests := []struct {
		name  string
		ys    []float64
		class string
	}{
		{"same direction", series20(func(i int) float64 { return 2 * float64(i) }), "strong"},
		{"opposite direction", series20(func(i int) float64 { return -float64(i) }), "negative"},
		{"unrelated", series20(func(i int) float64 { return float64(i%2) * 10 }), "weak"},
	}
	for _, tt := range tests {
		l := wantSome(t, tt.name, Correlation(xs, tt.ys))
		if l.Class != tt.class {
			t.Errorf("%s: class %q (r = %v), want %q", tt.name, l.Class, l.Correlation, tt.class)
		}
	}
	wantNone(t, "19 points", Correlation(xs[:19], xs[:19]), ReasonTooFewPoints)
	wantNone(t, "a constant series", Correlation(xs, series20(func(int) float64 { return 3 })), ReasonZeroVariance)
}

func days(start domain.Date, n int, f func(i int) (float64, *float64)) []PricePoint {
	out := make([]PricePoint, n)
	for i := range out {
		c, e := f(i)
		out[i] = PricePoint{Date: start.AddDays(i), Close: c, TTMEPS: e}
	}
	return out
}

func TestPEBands(t *testing.T) {
	// P/E climbs from 16 to 25 at a constant TTM EPS of 10.
	pts := days(domain.NewDate(2025, 1, 1), 10, func(i int) (float64, *float64) { return 160 + 10*float64(i), ptr(10.0) })
	b := wantSome(t, "bands", PEBands(pts))
	ms := make([]float64, 0, len(b.Multiples))
	for _, m := range b.Multiples {
		ms = append(ms, m.Multiple)
	}
	if diff := cmp.Diff([]float64{15, 17.5, 20, 22.5, 25}, ms); diff != "" {
		t.Errorf("multiples (-want +got):\n%s", diff)
	}
	last := b.Multiples[4]
	if wantSome(t, "price", last.PriceToday) != 250 || wantSome(t, "vs price", last.VsPrice) != "at" {
		t.Errorf("25x band = %+v, want $250 at today's price", last)
	}
	if wantSome(t, "vs price", b.Multiples[0].VsPrice) != "below" {
		t.Error("the 15x band is not below today's price")
	}
	if diff := cmp.Diff([]float64{17.5, 20, 22.5}, b.Crossed); diff != "" {
		t.Errorf("crossed (-want +got):\n%s", diff)
	}
	near(t, "nearest", wantSome(t, "nearest", b.Nearest), 25, 0)
	wantNone(t, "current band on a line", b.Current, ReasonNotApplicable)

	pts[9].Close = 235 // P/E 23.5; the P/Es now run 16 to 24, so the bands step by 2
	b = wantSome(t, "bands", PEBands(pts))
	if diff := cmp.Diff(BandPair{Lower: 22, Upper: 24}, wantSome(t, "current", b.Current)); diff != "" {
		t.Errorf("current band (-want +got):\n%s", diff)
	}

	pts[9].TTMEPS = nil
	b = wantSome(t, "bands", PEBands(pts))
	wantNone(t, "current P/E without EPS", b.CurrentPE, ReasonMissingData)
	wantNone(t, "price today without EPS", b.Multiples[0].PriceToday, ReasonMissingData)

	loss := days(domain.NewDate(2025, 1, 1), 3, func(int) (float64, *float64) { return 10, ptr(-1.0) })
	wantNone(t, "bands of a loss", PEBands(loss), ReasonNeedsPositiveEPS)
}

func TestPriceChartUsesTheDaysBeforeTheRange(t *testing.T) {
	// Two years of daily closes at 20 times a TTM EPS that grows and wobbles,
	// so the price follows earnings exactly.
	start := domain.NewDate(2024, 1, 1)
	daily := days(start, 731, func(i int) (float64, *float64) {
		eps := 5 * math.Pow(1.5, float64(i)/365) * (1 + 0.2*math.Sin(float64(i)/30))
		return 20 * eps, ptr(eps)
	})
	c := wantSome(t, "chart", PriceChart(daily, nil, Range6M))
	if c.Start != domain.NewDate(2025, 7, 1) || c.End != daily[730].Date {
		t.Errorf("range %s to %s, want 2025-07-01 to %s", c.Start, c.End, daily[730].Date)
	}
	k := 730 - len(c.Points) + 1 // index of the first visible day
	first := c.Points[0]
	near(t, "12-month price change at the range start", wantSome(t, "r12", first.PriceChange12M), daily[k].Close/daily[k-365].Close-1, 1e-12)
	near(t, "12-month EPS growth at the range start", wantSome(t, "g12", first.EPSGrowth12M), *daily[k].TTMEPS / *daily[k-365].TTMEPS - 1, 1e-12)
	near(t, "P/E", wantSome(t, "pe", first.PE), 20, 1e-12)
	if c.ChangeRange.Min > 0 {
		t.Errorf("change range %+v does not include 0", c.ChangeRange)
	}
	l := wantSome(t, "link", c.Link)
	if l.Class != "strong" || l.Correlation < 0.999 {
		t.Errorf("link = %+v, want strong for a price that follows earnings", l)
	}
	near(t, "P/E change implied by the 12-month lines", wantSome(t, "pe change", c.Last12Months.PEChange), 0, 1e-12)
	if trend := wantSome(t, "pe trend", c.PETrend); trend != "steady" {
		t.Errorf("P/E trend = %q, want steady at a constant P/E", trend)
	}

	whole := wantSome(t, "chart", PriceChart(daily, nil, Range3Y))
	if whole.Start != start {
		t.Errorf("3Y range starts at %s, want the first day %s", whole.Start, start)
	}
	wantNone(t, "12-month change in the first year", whole.Points[10].PriceChange12M, ReasonInsufficientHistory)
	wantNone(t, "chart without days", PriceChart(nil, nil, Range3Y), ReasonInsufficientHistory)
}

// A near-zero positive EPS gives a P/E in the billions or beyond; the bands
// still come out as a handful of lines.
func TestPEBandsAtANearZeroEPS(t *testing.T) {
	for _, eps := range []float64{1e-9, 1e-16, 5e-324} {
		pts := days(domain.NewDate(2026, 1, 1), 10, func(i int) (float64, *float64) { return 90 + 2*float64(i), ptr(eps) })
		b := PEBands(pts)
		if b.Ok() && (len(b.Get().Multiples) < 1 || len(b.Get().Multiples) > 50) {
			t.Errorf("EPS %g: %d bands, want 1 to 50", eps, len(b.Get().Multiples))
		}
	}
	pts := days(domain.NewDate(2026, 1, 1), 10, func(i int) (float64, *float64) { return 90 + 2*float64(i), ptr(1e-9) })
	if n := len(wantSome(t, "bands", PEBands(pts)).Multiples); n < 3 || n > 7 {
		t.Errorf("EPS 1e-9: %d bands, want about 5", n)
	}
}

func TestPriceChartPEChangeNeedsAPositiveEPSToday(t *testing.T) {
	for _, last := range []float64{0, -2} {
		daily := days(domain.NewDate(2025, 1, 1), 400, func(i int) (float64, *float64) {
			if i == 399 {
				return 100, ptr(last)
			}
			return 100 + float64(i%7), ptr(5.0)
		})
		c := wantSome(t, "chart", PriceChart(daily, nil, Range1Y))
		wantNone(t, "P/E change", c.Last12Months.PEChange, ReasonNeedsPositiveEPS)
		wantSome(t, "EPS growth", c.Last12Months.EPSGrowth)
	}
}

// A series of equal values does not vary, even when its mean carries
// rounding noise.
func TestCorrelationOfConstantSeries(t *testing.T) {
	xs := series20(func(int) float64 { return 0.1 })
	ys := series20(func(int) float64 { return 0.2 })
	wantNone(t, "two constant series", Correlation(xs, ys), ReasonZeroVariance)
	wantNone(t, "one constant series", Correlation(series20(func(i int) float64 { return float64(i) }), xs), ReasonZeroVariance)
}

// A P/E near 0.001 still gets distinct bands.
func TestPEBandsAtATinyPE(t *testing.T) {
	pts := days(domain.NewDate(2026, 1, 1), 10, func(i int) (float64, *float64) { return 0.01 + 0.001*float64(i), ptr(10.0) })
	b := wantSome(t, "bands", PEBands(pts))
	seen := map[float64]bool{}
	for _, m := range b.Multiples {
		if m.Multiple <= 0 || seen[m.Multiple] {
			t.Errorf("multiples %+v hold a zero or a repeat", b.Multiples)
			break
		}
		seen[m.Multiple] = true
	}
}

// The lowest valid P/E (a close of 0.0001 at a TTM EPS of 1,000,000) gets a
// handful of bands, not 50.
func TestPEBandsAtTheLowestValidPE(t *testing.T) {
	b := wantSome(t, "bands", PEBands([]PricePoint{{Date: domain.NewDate(2026, 1, 2), Close: 1e-4, TTMEPS: ptr(1e6)}}))
	if n := len(b.Multiples); n < 3 || n > 7 {
		t.Errorf("%d bands, want about 5", n)
	}
}
