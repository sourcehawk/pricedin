package engine

import (
	"fmt"
	"math"
	"regexp"
	"strconv"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// historyQuarters is how many quarters before the latest one the history
// panels show: three years.
const historyQuarters = 12

// windowStart is the first quarter of the history panels.
func (s series) windowStart() int {
	return max(0, s.last-historyQuarters)
}

// PathsInput is what the earnings trajectory needs.
type PathsInput struct {
	Quarters []Quarter `json:"quarters"`
	EPS      float64   `json:"eps"` // TTM EPS today, where both paths start
	Years    int       `json:"years"`
	Baseline Number    `json:"baseline"` // the baseline growth
	// YearlyRates, when non-nil, make the baseline path follow one rate per
	// year (the custom year-by-year baseline).
	YearlyRates []float64 `json:"yearlyRates"`
	// RequiredEPS is the EPS needed at the exit; the required path runs to it.
	RequiredEPS Number `json:"requiredEps"`
}

// Paths is the earnings trajectory: TTM EPS history, then the baseline and
// the required EPS paths, quarter by quarter.
type Paths struct {
	History []HistoryPoint `json:"history"`
	// Future starts today (QuartersAhead 0) and ends at the exit.
	Future []FuturePoint `json:"future"`
	// GapTone is bad when the required path ends above the baseline path
	// and good otherwise.
	GapTone Maybe[Tone] `json:"gapTone"`
	Scale   Axis        `json:"scale"` // covers every plotted point
}

// HistoryPoint is one reported quarter of the trajectory.
type HistoryPoint struct {
	Label     string      `json:"label"`
	PeriodEnd domain.Date `json:"periodEnd"`
	TTMEPS    Number      `json:"ttmEps"`
	TTMYoY    Number      `json:"ttmYoy"`
}

// FuturePoint is one quarter of the projection.
type FuturePoint struct {
	QuartersAhead int     `json:"quartersAhead"`
	Label         *string `json:"label"` // nil when the latest label is not "Q<n> FY<yy>"
	Baseline      Number  `json:"baseline"`
	Required      Number  `json:"required"`
	Gap           Number  `json:"gap"` // baseline minus required
}

// EarningsPaths returns the earnings trajectory. It is missing when EPS is
// not positive.
func EarningsPaths(in PathsInput) Maybe[Paths] {
	if !(in.EPS > 0) {
		return None[Paths](ReasonNeedsPositiveEPS)
	}
	s := newSeries(in.Quarters)
	out := Paths{History: []HistoryPoint{}}
	for i := s.windowStart(); i <= s.last; i++ {
		q := in.Quarters[i]
		out.History = append(out.History, HistoryPoint{Label: q.Label, PeriodEnd: q.PeriodEnd, TTMEPS: s.ttm[i], TTMYoY: s.ttmYoY[i]})
	}
	g, req := in.Baseline, in.RequiredEPS
	gOk := g.Ok() && growthInRange(g.Get())
	gReason := ReasonGrowthOutOfRange
	if !g.Ok() {
		gReason = g.Reason
	}
	var last string
	if s.last >= 0 {
		last = in.Quarters[s.last].Label
	}
	steps := 4 * in.Years
	for k := 0; k <= steps; k++ {
		p := FuturePoint{QuartersAhead: k, Baseline: None[float64](gReason), Required: None[float64](req.Reason)}
		if s.last >= 0 {
			p.Label = labelAfter(last, k)
		}
		if gOk {
			p.Baseline = Some(BaselinePath(in.EPS, g.Get(), k, in.YearlyRates))
		}
		if req.Ok() {
			p.Required = Some(in.EPS * math.Pow(req.Get()/in.EPS, float64(k)/float64(steps)))
		}
		p.Gap = None[float64](reasonOf(p.Baseline, p.Required))
		if p.Baseline.Ok() && p.Required.Ok() {
			p.Gap = Some(p.Baseline.Get() - p.Required.Get())
		}
		out.Future = append(out.Future, p)
	}
	end := out.Future[steps]
	out.GapTone = None[Tone](end.Gap.Reason)
	if end.Gap.Ok() {
		out.GapTone = Some(ToneGood)
		if end.Required.Get() > end.Baseline.Get() {
			out.GapTone = Some(ToneBad)
		}
	}

	lo, hi := math.Inf(1), math.Inf(-1)
	add := func(m Number) {
		if m.Ok() {
			lo, hi = math.Min(lo, m.Get()), math.Max(hi, m.Get())
		}
	}
	for _, h := range out.History {
		add(h.TTMEPS)
	}
	for _, f := range out.Future {
		add(f.Baseline)
	}
	for _, f := range out.Future {
		add(f.Required)
	}
	add(Some(in.EPS))
	mn := lo - math.Abs(lo)*0.08
	mx := hi + math.Abs(hi)*0.04
	st := NiceStep(mx-mn, 5)
	mn = math.Floor(mn/st) * st
	out.Scale = newAxis(mn, mx, st)
	return Some(out)
}

// labelAfter returns the label k quarters after a "Q<n> FY<yy>" label, or
// nil for any other label. k = 0 returns the label itself.
func labelAfter(label string, k int) *string {
	if k == 0 {
		return &label
	}
	m := regexp.MustCompile(`^Q([1-4]) FY(\d{2})$`).FindStringSubmatch(label)
	if m == nil {
		return nil
	}
	q, _ := strconv.Atoi(m[1])
	y, _ := strconv.Atoi(m[2])
	idx := q - 1 + k
	next := fmt.Sprintf("Q%d FY%02d", idx%4+1, (y+idx/4)%100)
	return &next
}

// Demanding is "how demanding over time": the growth the price required at
// each past quarter, with today's levers, against the TTM EPS growth that
// the company delivered.
type Demanding struct {
	Points []DemandingPoint `json:"points"`
	Scale  Axis             `json:"scale"`
	// TypicalRequired is the median required growth over the points.
	TypicalRequired Number `json:"typicalRequired"`
	// Peak is the most demanding point; the first one wins a tie (values
	// within 1e-9).
	Peak Maybe[Peak] `json:"peak"`
	// RequiredAboveDelivered counts the points, of Compared points with both
	// values, where the requirement exceeded the delivered growth.
	RequiredAboveDelivered int `json:"requiredAboveDelivered"`
	Compared               int `json:"compared"`
}

// DemandingPoint is one quarter. The latest one (Today) uses the price and
// TTM EPS levers instead of the quarter-end values.
type DemandingPoint struct {
	Label     string      `json:"label"`
	PeriodEnd domain.Date `json:"periodEnd"`
	Today     bool        `json:"today"`
	Price     Number      `json:"price"`
	TTMEPS    Number      `json:"ttmEps"`
	PE        Number      `json:"pe"`
	Required  Number      `json:"required"`
	Delivered Number      `json:"delivered"` // TTM EPS growth over the year to this quarter
}

// peakTie is how close to the highest required growth a value counts as
// equal to it.
const peakTie = 1e-9

// Peak is the index and value of the highest required growth.
type Peak struct {
	Index  int     `json:"index"`
	Growth float64 `json:"growth"`
}

// HowDemanding returns the required growth at each quarter of the last three
// years, had the investment been made then with today's levers.
func HowDemanding(quarters []Quarter, inv Investment, exitPE Number) Demanding {
	s := newSeries(quarters)
	out := Demanding{Points: []DemandingPoint{}}
	for i := s.windowStart(); i <= s.last; i++ {
		p := DemandingPoint{Label: quarters[i].Label, PeriodEnd: quarters[i].PeriodEnd, Today: i == s.last, Delivered: s.ttmYoY[i]}
		p.Price, p.TTMEPS = Some(inv.Price), Some(inv.EPS)
		if !p.Today {
			p.Price, p.TTMEPS = None[float64](ReasonMissingData), s.ttm[i]
			if c := quarters[i].Close; c != nil {
				p.Price = Some(*c)
			}
		}
		switch {
		case !p.Price.Ok() || !p.TTMEPS.Ok():
			p.PE = None[float64](reasonOf(p.Price, p.TTMEPS))
		case p.TTMEPS.Get() > 0:
			p.PE = Some(p.Price.Get() / p.TTMEPS.Get())
		default:
			p.PE = None[float64](ReasonNeedsPositiveEPS)
		}
		switch {
		case !exitPE.Ok():
			p.Required = exitPE
		case !p.Price.Ok() || !p.TTMEPS.Ok():
			p.Required = None[float64](reasonOf(p.Price, p.TTMEPS))
		default:
			at := inv
			at.Price, at.EPS = p.Price.Get(), p.TTMEPS.Get()
			p.Required = RequiredGrowth(at, exitPE.Get())
		}
		out.Points = append(out.Points, p)
	}

	mn, mx := 0.0, 0.05
	reqs := make([]float64, 0, len(out.Points))
	for i := range out.Points {
		p := &out.Points[i]
		for _, m := range []Number{p.Required, p.Delivered} {
			if m.Ok() {
				mn, mx = math.Min(mn, m.Get()), math.Max(mx, m.Get())
			}
		}
		if p.Required.Ok() {
			reqs = append(reqs, p.Required.Get())
		} else {
			reqs = append(reqs, math.NaN())
		}
		if p.Required.Ok() && p.Delivered.Ok() {
			out.Compared++
			if p.Required.Get() > p.Delivered.Get() {
				out.RequiredAboveDelivered++
			}
		}
	}
	st := NiceStep(mx-mn, 5)
	out.Scale = newAxis(math.Floor(mn/st)*st, math.Ceil(mx/st)*st, st)
	out.TypicalRequired = median(reqs)
	out.Peak = None[Peak](ReasonInsufficientHistory)
	top := math.Inf(-1)
	for _, v := range reqs {
		if !math.IsNaN(v) {
			top = math.Max(top, v)
		}
	}
	for i, v := range reqs {
		// Rounding noise must not move the peak between equal values.
		if !math.IsNaN(v) && v >= top-peakTie {
			out.Peak = Some(Peak{Index: i, Growth: v})
			break
		}
	}
	return out
}
