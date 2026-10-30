package engine

import "math"

// Trailing is trailing growth: the compound yearly growth of TTM EPS over
// Months.
type Trailing struct {
	Growth Number `json:"growth"`
	// Months is the look-back in use: the requested months rounded to a
	// multiple of 3, at least 6 and at most MaxMonths (when that is 6 or
	// more).
	Months    int    `json:"months"`
	MaxMonths int    `json:"maxMonths"` // the longest look-back the history allows
	FromTTM   Number `json:"fromTtm"`   // TTM EPS at the start of the look-back
	ToTTM     Number `json:"toTtm"`     // TTM EPS today
}

// defaultTrailingMonths is the look-back of trailing growth when none is set.
const defaultTrailingMonths = 12

// TrailingGrowth returns trailing growth over the requested months; zero
// months means 12.
func TrailingGrowth(quarters []Quarter, months int) Trailing {
	return newSeries(quarters).trailing(months)
}

func (s series) trailing(months int) Trailing {
	last := s.last
	maxMonths := max(0, 3*(last-3))
	if months == 0 {
		months = defaultTrailingMonths
	}
	m := int(jsRound(float64(months)/3) * 3)
	m = min(max(6, maxMonths), max(6, m))
	q := m / 3
	out := Trailing{Months: m, MaxMonths: maxMonths, FromTTM: None[float64](ReasonInsufficientHistory), ToTTM: None[float64](ReasonInsufficientHistory)}
	if last-q >= 0 {
		out.FromTTM = s.ttm[last-q]
	}
	if last >= 0 {
		out.ToTTM = s.ttm[last]
	}
	switch {
	case last-q < 3:
		out.Growth = None[float64](ReasonInsufficientHistory)
	case !out.FromTTM.Ok():
		out.Growth = out.FromTTM
	case !out.ToTTM.Ok():
		out.Growth = out.ToTTM
	case !(out.FromTTM.Get() > 0) || !(out.ToTTM.Get() > 0):
		out.Growth = None[float64](ReasonNeedsPositiveEPS)
	default:
		out.Growth = Some(math.Pow(out.ToTTM.Get()/out.FromTTM.Get(), 12/float64(m)) - 1)
	}
	return out
}

// Trajectory is the trajectory fill: one growth rate per year that
// continues last year's change in the weighted trend.
type Trajectory struct {
	// Rates are rounded to 0.5 pp and stay within -50% to 200%, the bounds
	// of a custom rate, so they can fill the custom year-by-year rates.
	// Within those bounds they never exceed Highest (rounded down to 0.5 pp).
	Rates    []float64 `json:"rates"`
	Slope    float64   `json:"slope"`    // the change of the weighted trend over the last year; 0 without Previous
	Previous Number    `json:"previous"` // the weighted trend four quarters ago
	Highest  float64   `json:"highest"`  // the highest weighted trend of the last 8 quarters
	Capped   bool      `json:"capped"`   // the trend speeds up and a rate sits at Highest
}

// TrajectoryFill returns the trajectory over years. It is missing when the
// history has no weighted trend.
func TrajectoryFill(quarters []Quarter, years int) Maybe[Trajectory] {
	return newSeries(quarters).trajectory(years)
}

func (s series) trajectory(years int) Maybe[Trajectory] {
	wtd := s.weighted()
	if !wtd.Ok() {
		return None[Trajectory](wtd.Reason)
	}
	hi := math.Inf(-1)
	for k := 0; k < 8; k++ {
		if v := s.weightedAt(s.last - k); v.Ok() {
			hi = math.Max(hi, v.Get())
		}
	}
	prev := s.weightedAt(s.last - 4)
	slope := 0.0
	if prev.Ok() {
		slope = wtd.Get() - prev.Get()
	}
	g, st := wtd.Get(), slope
	rates := make([]float64, 0, years)
	capped := false
	for y := 0; y < years; y++ {
		st *= 0.5
		g = math.Min(hi, math.Max(-0.5, g+st))
		rate := math.Min(math.Floor(hi*200)/2, jsRound(g*200)/2)
		rate = math.Max(minCustomGrowth*100, math.Min(maxCustomGrowth*100, rate))
		rates = append(rates, rate/100)
		if math.Abs(rate/100-hi) < 0.006 {
			capped = true
		}
	}
	return Some(Trajectory{Rates: rates, Slope: slope, Previous: prev, Highest: hi, Capped: slope > 0 && capped})
}

// YearRates returns one custom growth rate per year of the holding period.
// Years without a rate repeat the last rate, or the flat Rate when there is
// none; rates past the holding period are dropped.
func YearRates(c CustomGrowth, years int) []float64 {
	out := make([]float64, 0, years)
	for y := 0; y < years; y++ {
		switch {
		case y < len(c.Rates):
			out = append(out, c.Rates[y])
		case y > 0:
			out = append(out, out[y-1])
		default:
			out = append(out, c.Rate)
		}
	}
	return out
}

// YearlyAverage returns the compound average of yearly rates, each above
// -100%: the flat rate that ends at the same EPS.
func YearlyAverage(rates []float64) float64 {
	prod := 1.0
	for _, r := range rates {
		prod *= 1 + r
	}
	if !(prod > 0) {
		return -1
	}
	return math.Pow(prod, 1/float64(len(rates))) - 1
}

// BaselinePath returns EPS after k quarters on the baseline path from eps.
// With yearlyRates (non-nil) the path follows the rate of each year and
// ignores g; otherwise it compounds g. Both compound within a year.
func BaselinePath(eps, g float64, k int, yearlyRates []float64) float64 {
	if yearlyRates == nil {
		return eps * math.Pow(1+g, float64(k)/4)
	}
	e := eps
	full := k / 4
	for y := 0; y < full && y < len(yearlyRates); y++ {
		e *= 1 + yearlyRates[y]
	}
	part := float64(k)/4 - float64(full)
	if part > 0 && full < len(yearlyRates) {
		e *= math.Pow(1+yearlyRates[full], part)
	}
	return e
}

// GrowthScale returns the scale of the growth ruler for the required growth
// and the baselines. Missing values are ignored; the scale always spans 0%
// to 30%, and it has at most about 12 steps.
func GrowthScale(values []*float64) Axis {
	lo, hi := 0.0, 0.3
	for _, v := range values {
		if v != nil && isFinite(*v) {
			lo, hi = math.Min(lo, *v), math.Max(hi, *v)
		}
	}
	lo = math.Floor((lo-0.02)*20) / 20
	hi = math.Ceil((hi+0.03)*20) / 20
	step := 0.05
	switch {
	case hi-lo > 0.8:
		step = 0.2
	case hi-lo > 0.4:
		step = 0.1
	}
	if (hi-lo)/step > 12 {
		step = NiceStep(hi-lo, 6)
	}
	return newAxis(lo, hi, step)
}
