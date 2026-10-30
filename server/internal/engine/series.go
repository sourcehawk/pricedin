package engine

import "math"

// QuarterPoint holds the derived values of one quarter.
type QuarterPoint struct {
	TTMEPS        Number `json:"ttmEps"`        // the last four quarters' EPS added up
	YoY           Number `json:"yoy"`           // EPS against the same quarter a year earlier
	TTMYoY        Number `json:"ttmYoy"`        // TTM EPS against four quarters earlier
	WeightedTrend Number `json:"weightedTrend"` // the weighted trend as it stood at this quarter
}

// series holds the derived quarterly values that most functions need.
type series struct {
	q      []Quarter
	last   int // index of the latest quarter; -1 without quarters
	ttm    []Number
	yoy    []Number
	ttmYoY []Number
	wtd    []Number
}

func newSeries(quarters []Quarter) series {
	n := len(quarters)
	s := series{q: quarters, last: n - 1, ttm: make([]Number, n), yoy: make([]Number, n), ttmYoY: make([]Number, n), wtd: make([]Number, n)}
	for i := range quarters {
		s.ttm[i] = s.ttmAt(i)
		s.yoy[i] = s.yoyAt(i)
	}
	for i := range quarters {
		s.ttmYoY[i] = s.ttmYoYAt(i)
		s.wtd[i] = s.weightedAt(i)
	}
	return s
}

func (s series) ttmAt(i int) Number {
	if i < 3 {
		return None[float64](ReasonInsufficientHistory)
	}
	sum := 0.0
	// Newest first, as the TypeScript engine adds them: floating-point
	// addition is not associative.
	for k := 0; k < 4; k++ {
		e := s.q[i-k].EPS
		if e == nil {
			return None[float64](ReasonMissingData)
		}
		sum += *e
	}
	return Some(sum)
}

func (s series) yoyAt(i int) Number {
	if i < 4 {
		return None[float64](ReasonInsufficientHistory)
	}
	cur, base := s.q[i].EPS, s.q[i-4].EPS
	switch {
	case cur == nil || base == nil:
		return None[float64](ReasonMissingData)
	case !(*base > 0):
		return None[float64](ReasonNeedsPositiveEPS)
	}
	return Some(*cur / *base - 1)
}

func (s series) ttmYoYAt(i int) Number {
	if i < 7 {
		return None[float64](ReasonInsufficientHistory)
	}
	cur, base := s.ttm[i], s.ttm[i-4]
	switch {
	case !cur.Ok():
		return cur
	case !base.Ok():
		return base
	case !(base.Get() > 0):
		return None[float64](ReasonNeedsPositiveEPS)
	}
	return Some(cur.Get()/base.Get() - 1)
}

// weightedAt is the weighted trend at quarter t: the average YoY growth of
// the 8 quarters up to t, where a quarter's weight halves every 2 quarters
// back. Missing quarters drop out of the average.
func (s series) weightedAt(t int) Number {
	sw, sv := 0.0, 0.0
	for k := 0; k < 8; k++ {
		i := t - k
		if i < 0 || i >= len(s.yoy) || !s.yoy[i].Ok() {
			continue
		}
		w := math.Pow(0.5, float64(k)/2)
		sw += w
		sv += w * s.yoy[i].Get()
	}
	if sw == 0 {
		return None[float64](ReasonInsufficientHistory)
	}
	return Some(sv / sw)
}

// weighted is the weighted trend today.
func (s series) weighted() Number {
	if s.last < 0 {
		return None[float64](ReasonInsufficientHistory)
	}
	return s.wtd[s.last]
}

// today is the YoY growth of the latest quarter.
func (s series) today() Number {
	if s.last < 0 {
		return None[float64](ReasonInsufficientHistory)
	}
	return s.yoy[s.last]
}

// QuarterSeries returns the derived values of each quarter, in the order of
// quarters.
func QuarterSeries(quarters []Quarter) []QuarterPoint {
	s := newSeries(quarters)
	out := make([]QuarterPoint, len(quarters))
	for i := range quarters {
		out[i] = QuarterPoint{TTMEPS: s.ttm[i], YoY: s.yoy[i], TTMYoY: s.ttmYoY[i], WeightedTrend: s.wtd[i]}
	}
	return out
}

// WeightedTrend returns the weighted trend as it stood at quarter index at.
func WeightedTrend(quarters []Quarter, at int) Number {
	if at < 0 || at >= len(quarters) {
		return None[float64](ReasonInsufficientHistory)
	}
	return newSeries(quarters).wtd[at]
}
