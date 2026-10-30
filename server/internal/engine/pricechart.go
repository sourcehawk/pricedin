package engine

import (
	"math"
	"slices"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// ShareEvent is a quarter in which the diluted share count changed by 2% or
// more against the quarter before.
type ShareEvent struct {
	Label     string      `json:"label"`
	PeriodEnd domain.Date `json:"periodEnd"`
	From      float64     `json:"from"`
	To        float64     `json:"to"`
	Change    float64     `json:"change"`
	Kind      string      `json:"kind"` // "dilution" (more shares) or "buyback"
}

// shareEventMin is the smallest change in the share count that counts as
// an event.
const shareEventMin = 0.02

// ShareCountEvents returns the share-count events, oldest first. Quarters
// without a count on either side are skipped.
func ShareCountEvents(quarters []Quarter) []ShareEvent {
	out := []ShareEvent{}
	for i := 1; i < len(quarters); i++ {
		a, b := quarters[i-1].DilutedShares, quarters[i].DilutedShares
		if a == nil || b == nil || !(*a > 0) {
			continue
		}
		c := *b / *a - 1
		if math.Abs(c) < shareEventMin {
			continue
		}
		kind := "buyback"
		if c > 0 {
			kind = "dilution"
		}
		out = append(out, ShareEvent{Label: quarters[i].Label, PeriodEnd: quarters[i].PeriodEnd, From: *a, To: *b, Change: c, Kind: kind})
	}
	return out
}

// Link is a correlation with its class.
type Link struct {
	Correlation float64 `json:"correlation"`
	// Class is strong (0.6 or more), moderate (0.3 to 0.6), weak, or
	// negative (-0.3 or less).
	Class string `json:"class"`
}

const maxBands = 50

// minLinkPoints is the fewest points a price-earnings link needs.
const minLinkPoints = 20

// Correlation returns the Pearson correlation of xs and ys (equal length)
// as a price-earnings link. It is missing with too_few_points below 20
// points and with zero_variance when either series is constant.
func Correlation(xs, ys []float64) Maybe[Link] {
	if len(xs) < minLinkPoints {
		return None[Link](ReasonTooFewPoints)
	}
	if constant(xs) || constant(ys) {
		return None[Link](ReasonZeroVariance)
	}
	r := pearson(xs, ys)
	if math.IsNaN(r) || math.IsInf(r, 0) {
		return None[Link](ReasonZeroVariance)
	}
	class := "weak"
	switch {
	case r <= -0.3:
		class = "negative"
	case r >= 0.6:
		class = "strong"
	case r >= 0.3:
		class = "moderate"
	}
	return Some(Link{Correlation: r, Class: class})
}

// constant reports whether every value equals the first. A constant series
// does not vary, although rounding can leave its values off its mean.
func constant(vs []float64) bool {
	for _, v := range vs {
		if v != vs[0] {
			return false
		}
	}
	return true
}

func pearson(xs, ys []float64) float64 {
	n := float64(len(xs))
	mx, my := 0.0, 0.0
	for i := range xs {
		mx += xs[i]
	}
	for i := range ys {
		my += ys[i]
	}
	mx, my = mx/n, my/n
	sxy, sxx, syy := 0.0, 0.0, 0.0
	for i := range xs {
		dx, dy := xs[i]-mx, ys[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	return sxy / math.Sqrt(sxx*syy)
}

// Bands are the P/E bands: lines at fixed multiples of TTM EPS that cover
// the P/E range of the visible period.
type Bands struct {
	Multiples []Band `json:"multiples"`
	FirstPE   Number `json:"firstPe"`   // the P/E on the first day
	CurrentPE Number `json:"currentPe"` // the P/E on the last day
	// Current is the band the price sits in today; missing when it sits on
	// a line or outside the bands.
	Current Maybe[BandPair] `json:"current"`
	Nearest Number          `json:"nearest"` // the multiple nearest to today's P/E
	// Crossed lists the multiples that the P/E crossed between the first
	// and the last day, by more than 0.3.
	Crossed []float64 `json:"crossed"`
}

// Band is one multiple with its price today.
type Band struct {
	Multiple   float64       `json:"multiple"`
	PriceToday Number        `json:"priceToday"` // today's TTM EPS times the multiple
	VsPrice    Maybe[string] `json:"vsPrice"`    // "above", "below" or "at" (within 1%) today's close
}

// BandPair is the band between two neighboring multiples.
type BandPair struct {
	Lower float64 `json:"lower"`
	Upper float64 `json:"upper"`
}

// PEBands returns the P/E bands for the visible points, about 5 of them.
// BandPrice gives the line of a band on each day. The bands are missing
// when no point has a positive TTM EPS.
func PEBands(points []PricePoint) Maybe[Bands] {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, d := range points {
		if pe := pointPE(d); pe.Ok() && pe.Get() > 0 {
			lo, hi = math.Min(lo, pe.Get()), math.Max(hi, pe.Get())
		}
	}
	if math.IsInf(lo, 1) {
		return None[Bands](ReasonNeedsPositiveEPS)
	}
	pLo, pHi := lo*0.9, hi*1.05
	// The powers of ten widen until the largest step gives at most 5 bands
	// and the smallest at least 3, so a P/E in the billions (a near-zero
	// EPS) still gets a few bands.
	kLo, kHi := -1, 3
	for (pHi-pLo)/(5*math.Pow(10, float64(kHi))) > 5 && kHi < 400 {
		kHi++
	}
	for (pHi-pLo)/math.Pow(10, float64(kLo)) < 3 && kLo > -400 {
		kLo--
	}
	stp, best, digits := 1.0, 1e9, 2
	for k := kLo; k <= kHi; k++ {
		for _, f := range []float64{1, 2, 2.5, 5} {
			c := f * math.Pow(10, float64(k))
			n := math.Floor(pHi/c) - math.Ceil(pLo/c) + 1
			if n < 3 {
				continue
			}
			if sc := math.Abs(n - 5); sc < best {
				best, stp = sc, c
				// Enough decimals for the step: 0.025 needs three.
				digits = max(2, -k)
				if f == 2.5 {
					digits = max(2, 1-k)
				}
			}
		}
	}
	var ms []float64
	// The end slack shrinks with the step, so a tiny P/E range does not fill up.
	slack := math.Min(1e-9, stp*1e-6)
	// At a huge P/E a step can be too small to change m.
	for m := math.Ceil(pLo/stp) * stp; m <= pHi+slack && len(ms) < maxBands && m+stp > m; m += stp {
		if m > 0 {
			ms = append(ms, toFixed(m, digits))
		}
	}

	first, last := points[0], points[len(points)-1]
	out := Bands{Multiples: make([]Band, len(ms)), FirstPE: pointPE(first), CurrentPE: pointPE(last), Crossed: []float64{}}
	for i, m := range ms {
		b := Band{Multiple: m}
		if out.CurrentPE.Ok() {
			v := *last.TTMEPS * m
			b.PriceToday = Some(v)
			b.VsPrice = Some(classify(math.Abs(v/last.Close-1) < 0.01, v > last.Close, "at", "above", "below"))
		} else {
			b.PriceToday, b.VsPrice = None[float64](out.CurrentPE.Reason), None[string](out.CurrentPE.Reason)
		}
		out.Multiples[i] = b
	}
	if !out.CurrentPE.Ok() {
		out.Current, out.Nearest = None[BandPair](out.CurrentPE.Reason), None[float64](out.CurrentPE.Reason)
		return Some(out)
	}
	now := out.CurrentPE.Get()
	out.Current, out.Nearest = None[BandPair](ReasonNotApplicable), None[float64](ReasonNotApplicable)
	up := slices.IndexFunc(ms, func(m float64) bool { return m >= now })
	down := -1
	for i, m := range ms {
		if m <= now {
			down = i
		}
	}
	if up >= 0 && down >= 0 && ms[up] != ms[down] {
		out.Current = Some(BandPair{Lower: ms[down], Upper: ms[up]})
	}
	if len(ms) > 0 {
		out.Nearest = Some(ms[nearest(ms, now)])
	}
	if out.FirstPE.Ok() {
		a := out.FirstPE.Get()
		lo, hi := math.Min(a, now), math.Max(a, now)
		for _, m := range ms {
			if m > lo+0.3 && m < hi-0.3 {
				out.Crossed = append(out.Crossed, m)
			}
		}
	}
	return Some(out)
}

// BandPrice returns the price on the line of a P/E band on one day: the
// day's TTM EPS times the band's multiple. It is missing when the TTM EPS
// is null (missing_data) or not positive (needs_positive_eps).
func BandPrice(ttmEPS *float64, multiple float64) Number {
	switch {
	case ttmEPS == nil:
		return None[float64](ReasonMissingData)
	case !(*ttmEPS > 0):
		return None[float64](ReasonNeedsPositiveEPS)
	}
	return Some(*ttmEPS * multiple)
}

// pointPE is the P/E of a daily point; it needs a positive TTM EPS.
func pointPE(d PricePoint) Number {
	switch {
	case d.TTMEPS == nil:
		return None[float64](ReasonMissingData)
	case !(*d.TTMEPS > 0):
		return None[float64](ReasonNeedsPositiveEPS)
	}
	return finite(d.Close / *d.TTMEPS, ReasonNeedsPositiveEPS)
}

// Chart is the price chart of the visible range.
type Chart struct {
	Range  ChartRange   `json:"range"`
	Start  domain.Date  `json:"start"` // the first visible day
	End    domain.Date  `json:"end"`   // the last day
	Points []ChartPoint `json:"points"`
	// PriceAxis is the left axis. The overlays have free scales that fill
	// the plot: TTM EPS, P/E, and the two 12-month changes, which share
	// ChangeRange and always include 0.
	PriceAxis   Axis         `json:"priceAxis"`
	EPSRange    Maybe[Range] `json:"epsRange"`
	PERange     Maybe[Range] `json:"peRange"`
	ChangeRange Range        `json:"changeRange"`
	PriceChange float64      `json:"priceChange"` // over the range
	High        float64      `json:"high"`
	Low         float64      `json:"low"`
	EPSChange   Number       `json:"epsChange"` // TTM EPS over the range
	PEStart     Number       `json:"peStart"`
	PEEnd       Number       `json:"peEnd"`
	// PETrend is "expanded" (the P/E rose by more than 5%), "contracted" (fell
	// by more than 5%) or "steady".
	PETrend Maybe[string] `json:"peTrend"`
	// EarningsShareOfGain is the part of the price gain that came from
	// higher TTM EPS, on a log scale; the rest came from a higher P/E. It
	// exists only when both the price and TTM EPS rose.
	EarningsShareOfGain Number       `json:"earningsShareOfGain"`
	Last12Months        Last12Months `json:"last12Months"`
	Link                Maybe[Link]  `json:"link"` // the price-earnings link over the range
	Bands               Maybe[Bands] `json:"bands"`
	ShareCountEvents    []ShareEvent `json:"shareCountEvents"` // within the range
}

// ChartPoint is one day of the chart.
type ChartPoint struct {
	Date   domain.Date `json:"date"`
	Close  float64     `json:"close"`
	TTMEPS *float64    `json:"ttmEps"`
	// TTMQuarters are the labels of the quarters that TTMEPS adds up, oldest
	// first. It is empty without a TTM EPS, or when the day's TTMQuarter
	// names no quarter of the history.
	TTMQuarters    []string `json:"ttmQuarters"`
	PE             Number   `json:"pe"`
	PriceChange12M Number   `json:"priceChange12m"` // against the last close at least 365 days earlier
	EPSGrowth12M   Number   `json:"epsGrowth12m"`   // TTM EPS against the same day
}

// Last12Months are the 12-month changes on the last day.
type Last12Months struct {
	PriceChange Number `json:"priceChange"`
	EPSGrowth   Number `json:"epsGrowth"`
	PEChange    Number `json:"peChange"` // the change in P/E that the two imply
}

// PriceChart returns the price chart for the range. The 12-month changes use
// the days before the range too. It is missing without daily data.
func PriceChart(daily []PricePoint, quarters []Quarter, rng ChartRange) Maybe[Chart] {
	if len(daily) == 0 {
		return None[Chart](ReasonInsufficientHistory)
	}
	if rng == "" {
		rng = Range3Y
	}
	all := enrich(daily, quarters)
	end := daily[len(daily)-1].Date
	start := domain.DateOf(end.Time().AddDate(0, -rng.months(), 0))
	from := slices.IndexFunc(daily, func(d PricePoint) bool { return !d.Date.Before(start) })
	pts := all[from:]
	vis := daily[from:]

	out := Chart{Range: rng, Start: pts[0].Date, End: end, Points: pts}
	lo, hi := math.Inf(1), math.Inf(-1)
	for i := range pts {
		lo, hi = math.Min(lo, pts[i].Close), math.Max(hi, pts[i].Close)
	}
	out.High, out.Low = hi, lo
	mn, mx := lo*0.9, hi*1.06
	st := NiceStep(mx-mn, 5)
	out.PriceAxis = newAxis(math.Floor(mn/st)*st, math.Ceil(mx/st)*st, st)
	var eps, pes []float64
	changes := []float64{0}
	for i := range pts {
		p := &pts[i]
		if p.TTMEPS != nil {
			eps = append(eps, *p.TTMEPS)
		}
		if p.PE.Ok() {
			pes = append(pes, p.PE.Get())
		}
		if p.PriceChange12M.Ok() {
			changes = append(changes, p.PriceChange12M.Get())
		}
		if p.EPSGrowth12M.Ok() {
			changes = append(changes, p.EPSGrowth12M.Get())
		}
	}
	out.EPSRange = fit(eps, ReasonMissingData)
	out.PERange = fit(pes, ReasonNeedsPositiveEPS)
	out.ChangeRange = fit(changes, "").Get()

	f, l := pts[0], pts[len(pts)-1]
	out.PriceChange = l.Close/f.Close - 1
	switch {
	case f.TTMEPS == nil || l.TTMEPS == nil:
		out.EPSChange = None[float64](ReasonMissingData)
	case *f.TTMEPS > 0:
		out.EPSChange = Some(*l.TTMEPS / *f.TTMEPS - 1)
	default:
		out.EPSChange = None[float64](ReasonNeedsPositiveEPS)
	}
	out.PEStart, out.PEEnd = f.PE, l.PE
	out.PETrend = None[string](reasonOf(f.PE, l.PE))
	if f.PE.Ok() && l.PE.Ok() {
		a, b := f.PE.Get(), l.PE.Get()
		out.PETrend = Some(classify(b > a*1.05, b < a*0.95, "expanded", "contracted", "steady"))
	}
	out.EarningsShareOfGain = None[float64](ReasonNotApplicable)
	switch {
	case f.TTMEPS == nil || l.TTMEPS == nil:
		out.EarningsShareOfGain = None[float64](ReasonMissingData)
	case *f.TTMEPS > 0 && *l.TTMEPS > 0:
		lp, le := math.Log(l.Close/f.Close), math.Log(*l.TTMEPS / *f.TTMEPS)
		if lp > 0 && le > 0 {
			out.EarningsShareOfGain = Some(le / lp)
		}
	}
	r12, g12 := l.PriceChange12M, l.EPSGrowth12M
	out.Last12Months = Last12Months{PriceChange: r12, EPSGrowth: g12, PEChange: None[float64](reasonOf(r12, g12))}
	switch {
	case !r12.Ok() || !g12.Ok():
	case l.TTMEPS == nil || !(*l.TTMEPS > 0):
		out.Last12Months.PEChange = None[float64](ReasonNeedsPositiveEPS)
	default:
		out.Last12Months.PEChange = Some((1+r12.Get())/(1+g12.Get()) - 1)
	}
	var xs, ys []float64
	for i := range pts {
		p := &pts[i]
		if p.PriceChange12M.Ok() && p.EPSGrowth12M.Ok() {
			xs, ys = append(xs, p.PriceChange12M.Get()), append(ys, p.EPSGrowth12M.Get())
		}
	}
	out.Link = Correlation(xs, ys)
	out.Bands = PEBands(vis)
	out.ShareCountEvents = []ShareEvent{}
	for _, e := range ShareCountEvents(quarters) {
		if !e.PeriodEnd.Before(out.Start) && !e.PeriodEnd.After(end) {
			out.ShareCountEvents = append(out.ShareCountEvents, e)
		}
	}
	return Some(out)
}

// enrich adds the TTM quarters, the P/E and the 12-month changes to every
// daily point.
func enrich(daily []PricePoint, quarters []Quarter) []ChartPoint {
	index := make(map[string]int, len(quarters))
	for i, q := range quarters {
		index[q.Label] = i
	}
	out := make([]ChartPoint, len(daily))
	j := 0
	for i, d := range daily {
		back := d.Date.AddDays(-365)
		for j < len(daily)-1 && !daily[j+1].Date.After(back) {
			j++
		}
		p := ChartPoint{Date: d.Date, Close: d.Close, TTMEPS: d.TTMEPS, TTMQuarters: []string{}, PE: pointPE(d)}
		if k, ok := ttmIndex(d, index); ok {
			for _, q := range quarters[max(0, k-3) : k+1] {
				p.TTMQuarters = append(p.TTMQuarters, q.Label)
			}
		}
		ref := daily[j]
		if daily[0].Date.After(back) {
			p.PriceChange12M = None[float64](ReasonInsufficientHistory)
			p.EPSGrowth12M = None[float64](ReasonInsufficientHistory)
		} else {
			p.PriceChange12M = Some(d.Close/ref.Close - 1)
			switch {
			case d.TTMEPS == nil || ref.TTMEPS == nil:
				p.EPSGrowth12M = None[float64](ReasonMissingData)
			case !(*ref.TTMEPS > 0):
				p.EPSGrowth12M = None[float64](ReasonNeedsPositiveEPS)
			default:
				p.EPSGrowth12M = Some(*d.TTMEPS / *ref.TTMEPS - 1)
			}
		}
		out[i] = p
	}
	return out
}

// ttmIndex returns the index of the newest quarter of a day's TTM EPS.
func ttmIndex(d PricePoint, index map[string]int) (int, bool) {
	if d.TTMEPS == nil || d.TTMQuarter == nil {
		return 0, false
	}
	k, ok := index[*d.TTMQuarter]
	return k, ok
}

// fit returns a free scale that fills the plot with 8% padding; missing
// with reason when there are no values.
func fit(vals []float64, reason Reason) Maybe[Range] {
	if len(vals) == 0 {
		return None[Range](reason)
	}
	a, b := math.Inf(1), math.Inf(-1)
	for _, v := range vals {
		a, b = math.Min(a, v), math.Max(b, v)
	}
	r := b - a
	if r == 0 {
		r = math.Abs(b)
	}
	if r == 0 {
		r = 1
	}
	return Some(newRange(a-r*0.08, b+r*0.08))
}
