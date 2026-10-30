package engine

import (
	"math"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// Metric names a financial metric of the financials panel.
type Metric string

// The financial metrics, in the order the panel shows them.
const (
	MetricRevenue         Metric = "revenue"
	MetricOperatingCosts  Metric = "operatingCosts"
	MetricOperatingIncome Metric = "operatingIncome"
	MetricEPS             Metric = "eps"
	MetricFreeCashFlow    Metric = "freeCashFlow"
	MetricDilutedShares   Metric = "dilutedShares"
)

// AllMetrics returns every financial metric in panel order.
func AllMetrics() []Metric {
	return []Metric{MetricRevenue, MetricOperatingCosts, MetricOperatingIncome, MetricEPS, MetricFreeCashFlow, MetricDilutedShares}
}

// riseIsBad reports whether a rise of the metric is bad news. Rising costs
// and rising share counts are never good.
func (m Metric) riseIsBad() bool {
	return m == MetricOperatingCosts || m == MetricDilutedShares
}

func (m Metric) of(q Quarter) *float64 {
	switch m {
	case MetricRevenue:
		return q.Revenue
	case MetricOperatingCosts:
		return q.OperatingCosts
	case MetricOperatingIncome:
		return q.OperatingIncome
	case MetricEPS:
		return q.EPS
	case MetricFreeCashFlow:
		return q.FreeCashFlow
	default:
		return q.DilutedShares
	}
}

// financialQuarters is how many quarters the financials panel shows.
const financialQuarters = 8

// FinancialsView is the financials panel over the last eight quarters.
type FinancialsView struct {
	Quarters           []QuarterRef            `json:"quarters"`
	Metrics            map[Metric]MetricSeries `json:"metrics"`
	OperatingMargin    []Number                `json:"operatingMargin"`    // operating income over revenue
	CostShareOfRevenue []Number                `json:"costShareOfRevenue"` // operating costs over revenue
	Tiles              Maybe[Tiles]            `json:"tiles"`
	Compare            Compare                 `json:"compare"`
}

// QuarterRef names a quarter.
type QuarterRef struct {
	Label     string      `json:"label"`
	PeriodEnd domain.Date `json:"periodEnd"`
}

// MetricSeries is one metric per quarter with its changes and their tones.
// For operating costs, a YoY change is warn when it outgrows revenue by
// more than 0.5 pp and neutral otherwise.
type MetricSeries struct {
	Values  []*float64    `json:"values"`
	YoY     []Number      `json:"yoy"`
	YoYTone []Maybe[Tone] `json:"yoyTone"`
	QoQ     []Number      `json:"qoq"`
	QoQTone []Maybe[Tone] `json:"qoqTone"` // neutral for a change below 0.0001%
	// Scale is the bar scale from zero; for diluted shares it zooms in on
	// the values instead.
	Scale Maybe[Range] `json:"scale"`
}

// Tiles compare the latest quarter with the same quarter a year earlier.
type Tiles struct {
	Label           string     `json:"label"`
	CompareLabel    string     `json:"compareLabel"`
	Revenue         Tile       `json:"revenue"`
	OperatingCosts  Tile       `json:"operatingCosts"`
	OperatingIncome Tile       `json:"operatingIncome"`
	OperatingMargin MarginTile `json:"operatingMargin"`
	EPS             Tile       `json:"eps"`
	FreeCashFlow    Tile       `json:"freeCashFlow"`
	DilutedShares   Tile       `json:"dilutedShares"`
}

// Tile is one metric of the tiles.
type Tile struct {
	Value  Number      `json:"value"`
	Change Number      `json:"change"`
	Tone   Maybe[Tone] `json:"tone"`
}

// MarginTile is the operating margin tile; Change is in fractions (0.01 is
// one pp).
type MarginTile struct {
	Value    Number `json:"value"`
	Previous Number `json:"previous"`
	Change   Number `json:"change"`
}

// Compare is the compare view: each metric's change since the first
// quarter and from quarter to quarter, and how those changes correlate.
type Compare struct {
	SinceStart         map[Metric][]Number `json:"sinceStart"`
	QuarterOverQuarter map[Metric][]Number `json:"quarterOverQuarter"`
	// Correlations hold every pair of metrics, in AllMetrics order, a before b.
	Correlations []PairCorrelation `json:"correlations"`
	Changes      int               `json:"changes"` // quarter-to-quarter changes per metric
}

// PairCorrelation is the correlation of two metrics' quarter-to-quarter
// changes, over the quarters where both changes exist.
type PairCorrelation struct {
	A      Metric                  `json:"a"`
	B      Metric                  `json:"b"`
	Result Maybe[PairCorrelationV] `json:"result"`
}

// PairCorrelationV is a correlation and its class: strongSame (0.6 or
// more), moderateSame (0.3 or more), weak, moderateOpposite (above -0.6) or
// strongOpposite.
type PairCorrelationV struct {
	Correlation float64 `json:"correlation"`
	Class       string  `json:"class"`
}

func change(a, b *float64) Number {
	switch {
	case a == nil || b == nil:
		return None[float64](ReasonMissingData)
	case !(*b > 0):
		return None[float64](ReasonNonPositiveBase)
	}
	return Some(*a / *b - 1)
}

func ratio(a, b *float64) Number {
	switch {
	case a == nil || b == nil:
		return None[float64](ReasonMissingData)
	case !(*b > 0):
		return None[float64](ReasonNonPositiveBase)
	}
	return Some(*a / *b)
}

// riseTone is good for a rise of a metric where rising is good, and bad
// otherwise.
func riseTone(m Metric, y float64) Tone {
	if (y >= 0) != m.riseIsBad() {
		return ToneGood
	}
	return ToneBad
}

// Financials returns the financials panel for the last eight quarters.
func Financials(quarters []Quarter) FinancialsView {
	w := quarters[max(0, len(quarters)-financialQuarters):]
	out := FinancialsView{
		Quarters:           make([]QuarterRef, len(w)),
		Metrics:            map[Metric]MetricSeries{},
		OperatingMargin:    make([]Number, len(w)),
		CostShareOfRevenue: make([]Number, len(w)),
		Compare: Compare{
			SinceStart:         map[Metric][]Number{},
			QuarterOverQuarter: map[Metric][]Number{},
			Correlations:       []PairCorrelation{},
			Changes:            max(0, len(w)-1),
		},
	}
	for i, q := range w {
		out.Quarters[i] = QuarterRef{Label: q.Label, PeriodEnd: q.PeriodEnd}
		out.OperatingMargin[i] = ratio(q.OperatingIncome, q.Revenue)
		out.CostShareOfRevenue[i] = ratio(q.OperatingCosts, q.Revenue)
	}
	values := func(m Metric) []*float64 {
		v := make([]*float64, len(w))
		for i, q := range w {
			v[i] = m.of(q)
		}
		return v
	}
	yoyOf := func(d []*float64) []Number {
		y := make([]Number, len(d))
		for i := range d {
			y[i] = None[float64](ReasonInsufficientHistory)
			if i >= 4 {
				y[i] = change(d[i], d[i-4])
			}
		}
		return y
	}
	rev := values(MetricRevenue)
	revYoY := yoyOf(rev)
	metrics := AllMetrics()
	for _, m := range metrics {
		d := values(m)
		s := MetricSeries{Values: d, YoY: yoyOf(d), YoYTone: make([]Maybe[Tone], len(d)), QoQ: make([]Number, len(d)), QoQTone: make([]Maybe[Tone], len(d))}
		for i := range d {
			if y := s.YoY[i]; !y.Ok() {
				s.YoYTone[i] = None[Tone](y.Reason)
			} else if m == MetricOperatingCosts {
				s.YoYTone[i] = Some(ToneNeutral)
				if revYoY[i].Ok() && y.Get() > revYoY[i].Get()+0.005 {
					s.YoYTone[i] = Some(ToneWarn)
				}
			} else {
				s.YoYTone[i] = Some(riseTone(m, y.Get()))
			}
			s.QoQ[i] = None[float64](ReasonInsufficientHistory)
			if i >= 1 {
				s.QoQ[i] = change(d[i], d[i-1])
			}
			switch q := s.QoQ[i]; {
			case !q.Ok():
				s.QoQTone[i] = None[Tone](q.Reason)
			case math.Abs(q.Get()) < 1e-6:
				s.QoQTone[i] = Some(ToneNeutral)
			default:
				s.QoQTone[i] = Some(riseTone(m, q.Get()))
			}
		}
		s.Scale = metricScale(m, d)
		out.Metrics[m] = s

		since := make([]Number, len(d))
		for i := range d {
			switch {
			case d[i] == nil || d[0] == nil:
				since[i] = None[float64](ReasonMissingData)
			case !(*d[0] > 0):
				since[i] = None[float64](ReasonNonPositiveBase)
			default:
				since[i] = Some(*d[i] / *d[0] - 1)
			}
		}
		out.Compare.SinceStart[m] = since
		out.Compare.QuarterOverQuarter[m] = s.QoQ
	}
	for a := 0; a < len(metrics); a++ {
		for b := a + 1; b < len(metrics); b++ {
			ma, mb := metrics[a], metrics[b]
			out.Compare.Correlations = append(out.Compare.Correlations, pairCorrelation(ma, mb, out.Metrics[ma].QoQ, out.Metrics[mb].QoQ))
		}
	}
	out.Tiles = tiles(w, rev, values, out.OperatingMargin)
	return out
}

func metricScale(m Metric, d []*float64) Maybe[Range] {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range d {
		if v != nil {
			lo, hi = math.Min(lo, *v), math.Max(hi, *v)
		}
	}
	if math.IsInf(lo, 1) {
		return None[Range](ReasonMissingData)
	}
	if m == MetricDilutedShares {
		pad := math.Max((hi-lo)*0.35, hi*0.004)
		return Some(newRange(lo-pad, hi+pad))
	}
	return Some(newRange(math.Min(0, lo*1.12), math.Max(0, hi*1.12)))
}

func pairCorrelation(a, b Metric, qa, qb []Number) PairCorrelation {
	var xs, ys []float64
	for i := 1; i < len(qa); i++ {
		if qa[i].Ok() && qb[i].Ok() {
			xs, ys = append(xs, qa[i].Get()), append(ys, qb[i].Get())
		}
	}
	out := PairCorrelation{A: a, B: b, Result: None[PairCorrelationV](ReasonMissingData)}
	if len(xs) == 0 {
		return out
	}
	n := float64(len(xs))
	ma, mb := 0.0, 0.0
	for i := range xs {
		ma += xs[i]
	}
	for i := range ys {
		mb += ys[i]
	}
	ma, mb = ma/n, mb/n
	sab, saa, sbb := 0.0, 0.0, 0.0
	for i := range xs {
		sab += (xs[i] - ma) * (ys[i] - mb)
		saa += (xs[i] - ma) * (xs[i] - ma)
		sbb += (ys[i] - mb) * (ys[i] - mb)
	}
	if saa == 0 || sbb == 0 {
		out.Result = None[PairCorrelationV](ReasonZeroVariance)
		return out
	}
	r := sab / math.Sqrt(saa*sbb)
	class := "strongOpposite"
	switch {
	case r >= 0.6:
		class = "strongSame"
	case r >= 0.3:
		class = "moderateSame"
	case r > -0.3:
		class = "weak"
	case r > -0.6:
		class = "moderateOpposite"
	}
	out.Result = Some(PairCorrelationV{Correlation: r, Class: class})
	return out
}

func tiles(w []Quarter, rev []*float64, values func(Metric) []*float64, margin []Number) Maybe[Tiles] {
	if len(w) < 5 {
		return None[Tiles](ReasonInsufficientHistory)
	}
	i, j := len(w)-1, len(w)-5
	revChange := change(rev[i], rev[j])
	tile := func(m Metric) Tile {
		d := values(m)
		t := Tile{Value: None[float64](ReasonMissingData), Change: change(d[i], d[j])}
		if d[i] != nil {
			t.Value = Some(*d[i])
		}
		switch c := t.Change; {
		case !c.Ok():
			t.Tone = None[Tone](c.Reason)
		case m == MetricOperatingCosts:
			t.Tone = Some(ToneNeutral)
			if revChange.Ok() && c.Get() > revChange.Get()+0.005 {
				t.Tone = Some(ToneWarn)
			}
		case math.Abs(c.Get()) < 1e-9:
			t.Tone = Some(ToneNeutral)
		default:
			t.Tone = Some(riseTone(m, c.Get()))
		}
		return t
	}
	mt := MarginTile{Value: margin[i], Previous: margin[j], Change: None[float64](reasonOf(margin[i], margin[j]))}
	if margin[i].Ok() && margin[j].Ok() {
		mt.Change = Some(margin[i].Get() - margin[j].Get())
	}
	return Some(Tiles{
		Label:           w[i].Label,
		CompareLabel:    w[j].Label,
		Revenue:         tile(MetricRevenue),
		OperatingCosts:  tile(MetricOperatingCosts),
		OperatingIncome: tile(MetricOperatingIncome),
		OperatingMargin: mt,
		EPS:             tile(MetricEPS),
		FreeCashFlow:    tile(MetricFreeCashFlow),
		DilutedShares:   tile(MetricDilutedShares),
	})
}

// CompareScale returns the axis of the compare chart for the values of the
// metrics the user selected. It always includes 0.
func CompareScale(values []*float64) Axis {
	mn, mx := 0.0, 0.0
	for _, v := range values {
		if v != nil && isFinite(*v) {
			mn, mx = math.Min(mn, *v), math.Max(mx, *v)
		}
	}
	rng := mx - mn
	if rng == 0 {
		rng = 1
	}
	st := NiceStep(rng, 5)
	return newAxis(math.Floor(mn/st)*st, math.Ceil(mx/st)*st, st)
}
