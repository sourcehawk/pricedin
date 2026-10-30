package fundamentals

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/sec"
)

// Derive turns the SEC facts of the company cik into fiscal quarters.
//
// Only 10-Q, 10-K and 10-KT filings and their amendments feed quarters.
// For each concept and period, the latest filing wins, as of opts.AsOf when
// it is set. A quarter that no filing reports comes from year-to-date
// values (Q2 = H1 − Q1, Q3 = 9M − H1, Q4 = FY − 9M). Per-share values and
// share counts are adjusted for the confirmed splits (opts.Splits and the
// SEC split-ratio concept) unless a filing after the split restated them.
//
// It returns an error when a fact belongs to another company or a split in
// opts.Splits has no positive ratio. Facts that cannot feed a quarter, such
// as instants or values in an unexpected unit, are ignored. A company
// without an annual report gives an empty Series.
func Derive(cik domain.CIK, facts []domain.Fact, opts Options) (Series, error) {
	for _, s := range opts.Splits {
		if s.From <= 0 || s.To <= 0 {
			return Series{}, fmt.Errorf("derive fundamentals: split %s on %s: from %v to %v is not a positive ratio", s.Symbol, s.Date, s.From, s.To)
		}
	}
	d := deriver{periods: map[periodKey][]observation{}}
	cal := newCalendarBuilder()
	var ratios []domain.Fact
	for i := range facts {
		f := &facts[i]
		if f.CIK != cik {
			return Series{}, fmt.Errorf("derive fundamentals: fact %s %s of cik %d, want %d", f.Concept, f.Accession, int64(f.CIK), int64(cik))
		}
		if opts.AsOf != nil && f.Filed.After(*opts.AsOf) {
			continue
		}
		if f.Concept == string(sec.StockholdersEquityNoteStockSplitConversionRatio1) {
			ratios = append(ratios, *f)
			continue
		}
		if f.PeriodStart == nil || !isPeriodicReport(f.Form) || !isMetricConcept(f.Concept) ||
			f.Unit != unitOf(f.Concept) || f.PeriodEnd.Before(*f.PeriodStart) {
			continue
		}
		cal.add(f)
		k := periodKey{f.Concept, *f.PeriodStart, f.PeriodEnd}
		d.periods[k] = append(d.periods[k], observation{fact: *f, value: f.Value, ratio: 1, scale: 1})
	}

	years := cal.build()
	d.snap(years)
	d.splits, d.conflicts = confirmSplits(cik, ratios, opts, d.periods)
	d.settle()

	d.rescaleShares()

	series := Series{Splits: d.splits}
	for _, y := range years {
		start := y.start
		var earlier []span
		for i, end := range y.ends {
			q := quarterSpan{fyStart: y.start, n: i + 1, start: start, end: end, earlier: earlier}
			if i > 0 {
				q.prevEnd = y.ends[i-1]
			}
			earlier = append(slices.Clip(earlier), span{start, end})
			series.Quarters = append(series.Quarters, Quarter{
				FiscalYear:    y.fy,
				FiscalQuarter: q.n,
				Label:         quarterLabel(y.fy, q.n),
				PeriodStart:   start,
				PeriodEnd:     end,
				Values:        d.values(q),
			})
			start = end.AddDays(1)
		}
	}
	series.Conflicts = d.conflicts
	return series, nil
}

type periodKey struct {
	concept    string
	start, end domain.Date
}

// observation is one filing's value for a period. value is the reported
// value after split adjustment by ratio and a scale correction by scale.
type observation struct {
	fact  domain.Fact
	value float64
	ratio float64
	scale float64
}

func (o observation) ref() SourceRef {
	f := o.fact
	return SourceRef{
		Accession:   f.Accession,
		Form:        f.Form,
		Concept:     f.Concept,
		Filed:       f.Filed,
		PeriodStart: f.PeriodStart,
		PeriodEnd:   f.PeriodEnd,
		Value:       f.Value,
		SplitRatio:  o.ratio,
		Scale:       cmp.Or(o.scale, 1),
	}
}

// winner is the observation that a period uses, with the date of the
// period's first filing.
type winner struct {
	obs        observation
	firstFiled domain.Date
}

type deriver struct {
	periods   map[periodKey][]observation
	winners   map[periodKey]winner
	splits    []domain.Split
	conflicts []Conflict
}

// snapTolerance is how many days a reported period can start or end away
// from a period of the fiscal calendar and still count as it. Filers
// sometimes move a boundary by a day in a later comparative: Coca-Cola's
// 10-Q of July 2012 gives its Q2 2011 as starting on 2011-04-03, while its
// 10-Q of August 2011 gave 2011-04-02.
const snapTolerance = 3

// snap files each observation under the quarter or year-to-date period of
// years that it matches within snapTolerance days, so that a restatement
// of a shifted period competes with the original for the latest filing.
// An observation keeps its reported dates in its fact.
func (d *deriver) snap(years []fiscalYear) {
	var calendar []span
	for _, y := range years {
		start := y.start
		for _, end := range y.ends {
			calendar = append(calendar, span{start, end}, span{y.start, end})
			start = end.AddDays(1)
		}
	}
	exact := make(map[span]bool, len(calendar))
	for _, c := range calendar {
		exact[c] = true
	}
	snapped := make(map[periodKey][]observation, len(d.periods))
	for k, obs := range d.periods {
		if p := (span{k.start, k.end}); !exact[p] {
			for _, c := range calendar {
				if abs(daysIn(c.start, p.start)-1) <= snapTolerance && abs(daysIn(c.end, p.end)-1) <= snapTolerance {
					k.start, k.end = c.start, c.end
					break
				}
			}
		}
		snapped[k] = append(snapped[k], obs...)
	}
	d.periods = snapped
}

// settle orders each period's filings, adjusts them for splits, picks the
// value each period uses and records the reconciliation conflicts.
func (d *deriver) settle() {
	keys := make([]periodKey, 0, len(d.periods))
	for k := range d.periods {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b periodKey) int {
		return cmp.Or(cmp.Compare(a.concept, b.concept), compareDates(a.end, b.end), compareDates(a.start, b.start))
	})
	d.winners = make(map[periodKey]winner, len(keys))
	for _, k := range keys {
		obs := d.periods[k]
		slices.SortStableFunc(obs, func(a, b observation) int {
			return cmp.Or(compareDates(a.fact.Filed, b.fact.Filed), cmp.Compare(a.fact.Accession, b.fact.Accession))
		})
		for i := range obs {
			adjust(&obs[i], d.splits)
		}
		w := winner{obs: obs[len(obs)-1], firstFiled: obs[0].fact.Filed}
		if kindOf(k.concept) != kindMoney {
			var c []Conflict
			w.obs, c = reconcile(obs)
			d.conflicts = append(d.conflicts, c...)
		}
		d.winners[k] = w
	}
}

func compareDates(a, b domain.Date) int {
	return a.Time().Compare(b.Time())
}

// quarterSpan is one fiscal quarter: the n-th of the fiscal year that
// starts on fyStart. prevEnd is the end of the quarter before, zero for Q1,
// and earlier holds the year's quarters before this one.
type quarterSpan struct {
	fyStart    domain.Date
	n          int
	start, end domain.Date
	prevEnd    domain.Date
	earlier    []span
}

func (d *deriver) lookup(concept sec.Concept, start, end domain.Date) (winner, bool) {
	w, ok := d.winners[periodKey{string(concept), start, end}]
	return w, ok
}

// values derives every metric that the filings give for q.
func (d *deriver) values(q quarterSpan) map[Metric]Value {
	out := map[Metric]Value{}
	for _, m := range Metrics() {
		var v Value
		var ok bool
		if m == DilutedShares {
			v, ok = d.shares(q)
		} else if concepts := conceptsOf(m); concepts != nil {
			v, ok = d.flow(concepts, q)
		}
		if ok {
			out[m] = v
		}
	}
	if v, ok := difference(out, OperatingCashFlow, CapitalExpenditure); ok {
		out[FreeCashFlow] = v
	}
	if v, ok := difference(out, Revenue, OperatingIncome); ok {
		out[OperatingCosts] = v
	}
	return out
}

// flow derives a value that adds up over time, from the first concept that
// gives one. All inputs of a value come from that one concept.
func (d *deriver) flow(concepts []sec.Concept, q quarterSpan) (Value, bool) {
	var out Value
	found := false
	var known domain.Date
	use := func(v Value) {
		known = earliestDate(known, v.FirstFiled)
		if !found {
			out, found = v, true
		}
	}
	for _, c := range concepts {
		if w, ok := d.lookup(c, q.start, q.end); ok {
			use(newValue(w.obs.value, MethodReported, false, w))
		}
		if q.n == 1 {
			continue
		}
		ytd, ok := d.lookup(c, q.fyStart, q.end)
		if !ok {
			continue
		}
		prev, ok := d.lookup(c, q.fyStart, q.prevEnd)
		if !ok {
			continue
		}
		ytd, prev = d.sameBasis(c, span{q.fyStart, q.end}, ytd, span{q.fyStart, q.prevEnd}, prev)
		method := MethodYTDDifference
		if q.n == 4 {
			method = MethodFYDifference
		}
		use(newValue(tidy(ytd.obs.value-prev.obs.value), method, false, ytd, prev))
	}
	out.FirstFiled = known
	return out, found
}

// sameScale reports whether the share count v lies within half and twice
// ref. Diluted counts of one company move far less than that within a
// year, while a tagging error moves them 1,000-fold.
func sameScale(v, ref float64) bool {
	return v >= ref/2 && v <= ref*2
}

// rejectEstimate records why the Q4 count of q is left out: v, an input
// or the result, is not on the scale of the full year.
func (d *deriver) rejectEstimate(q quarterSpan, year winner, v float64) {
	start := q.start
	d.conflicts = append(d.conflicts, Conflict{
		Kind:        ConflictShareScale,
		Concept:     year.obs.fact.Concept,
		PeriodStart: &start,
		PeriodEnd:   q.end,
		Restated:    year.obs.ref(),
		Observed:    tidy(v / year.obs.value),
		Expected:    1,
	})
}

// shareNeighborDays is how far apart in days the period ends of two share
// counts can be for one to check the scale of the other.
const shareNeighborDays = 200

// rescaleShares corrects each used share count that is 1,000 or 1,000,000
// times off. The first reference is net income over diluted EPS of the
// same period, which does not depend on how the filing tags share counts.
func (d *deriver) rescaleShares() {
	concept := string(sec.WeightedAverageNumberOfDilutedSharesOutstanding)
	var keys []periodKey
	for k := range d.winners {
		if k.concept == concept {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b periodKey) int {
		return cmp.Or(compareDates(a.end, b.end), compareDates(a.start, b.start))
	})
	var unchecked []periodKey
	for _, k := range keys {
		if ref, ok := d.impliedShares(k); ok {
			d.rescale(k, ref)
		} else {
			unchecked = append(unchecked, k)
		}
	}
	for _, k := range unchecked {
		var near []float64
		for _, o := range keys {
			if o != k && abs(daysIn(o.end, k.end)-1) <= shareNeighborDays {
				near = append(near, d.winners[o].obs.value)
			}
		}
		if len(near) > 0 {
			slices.Sort(near)
			d.rescale(k, near[len(near)/2])
		}
	}
}

// impliedShares returns |net income| / |diluted EPS| for the period of k,
// when EPS is large enough for its rounding to cents not to matter.
func (d *deriver) impliedShares(k periodKey) (float64, bool) {
	ni, ok := d.winners[periodKey{string(sec.NetIncomeLoss), k.start, k.end}]
	if !ok || ni.obs.value == 0 {
		return 0, false
	}
	eps, ok := d.winners[periodKey{string(sec.EarningsPerShareDiluted), k.start, k.end}]
	if !ok || math.Abs(eps.obs.value) < 0.10 {
		return 0, false
	}
	return math.Abs(ni.obs.value / eps.obs.value), true
}

// rescale multiplies the count of k by 1,000 or 1,000,000 when only the
// rescaled count is on the scale of ref, and records the conflict.
func (d *deriver) rescale(k periodKey, ref float64) {
	w := d.winners[k]
	if sameScale(w.obs.value, ref) {
		return
	}
	for _, s := range []float64{1e3, 1e6} {
		if !sameScale(w.obs.value*s, ref) {
			continue
		}
		w.obs.value *= s
		w.obs.scale = s
		d.winners[k] = w
		start := k.start
		d.conflicts = append(d.conflicts, Conflict{
			Kind:        ConflictShareScale,
			Concept:     k.concept,
			PeriodStart: &start,
			PeriodEnd:   k.end,
			Restated:    w.obs.ref(),
			Observed:    s,
			Expected:    1,
		})
		return
	}
}

// sameBasis returns the inputs a and b of a difference on one split basis.
// A restatement after a split is rounded on the new basis, so subtracting
// an unrestated value of the old basis, divided by the ratio, loses
// precision: NVIDIA's FY23 EPS of 1.74 became 0.17 after its 10-for-1
// split, while the 9M value of 1.17 was never restated.
func (d *deriver) sameBasis(c sec.Concept, pa span, a winner, pb span, b winner) (winner, winner) {
	switch {
	case a.obs.ratio < b.obs.ratio:
		if o, ok := d.onBasis(c, pa, b.obs.ratio); ok {
			a.obs = o
		}
	case b.obs.ratio < a.obs.ratio:
		if o, ok := d.onBasis(c, pb, a.obs.ratio); ok {
			b.obs = o
		}
	}
	return a, b
}

// onBasis returns the latest filing for the period p of concept c that was
// adjusted by ratio.
func (d *deriver) onBasis(c sec.Concept, p span, ratio float64) (observation, bool) {
	obs := d.periods[periodKey{string(c), p.start, p.end}]
	for i := len(obs) - 1; i >= 0; i-- {
		if obs[i].ratio == ratio {
			return obs[i], true
		}
	}
	return observation{}, false
}

// shares derives the diluted share count. A weighted average does not add
// up over time, so only Q4 is derived, with each quarter weighted by its
// days, and the result is an estimate.
func (d *deriver) shares(q quarterSpan) (Value, bool) {
	c := sec.WeightedAverageNumberOfDilutedSharesOutstanding
	weighted, wok := d.weightedQ4(c, q)
	w, ok := d.lookup(c, q.start, q.end)
	if !ok {
		return weighted, wok
	}
	v := newValue(w.obs.value, MethodReported, false, w)
	if wok {
		v.FirstFiled = earliestDate(v.FirstFiled, weighted.FirstFiled)
	}
	return v, true
}

func (d *deriver) weightedQ4(c sec.Concept, q quarterSpan) (Value, bool) {
	if q.n != 4 {
		return Value{}, false
	}
	year, ok := d.lookup(c, q.fyStart, q.end)
	if !ok {
		return Value{}, false
	}
	inputs := []winner{year}
	weighted := year.obs.value * float64(daysIn(q.fyStart, q.end))
	for _, e := range q.earlier {
		w, ok := d.lookup(c, e.start, e.end)
		if !ok {
			return Value{}, false
		}
		if !sameScale(w.obs.value, year.obs.value) {
			d.rejectEstimate(q, year, w.obs.value)
			return Value{}, false
		}
		inputs = append(inputs, w)
		weighted -= w.obs.value * float64(daysIn(e.start, e.end))
	}
	v := math.Round(weighted / float64(daysIn(q.start, q.end)))
	if !sameScale(v, year.obs.value) {
		d.rejectEstimate(q, year, v)
		return Value{}, false
	}
	return newValue(v, MethodWeightedAverageQ4, true, inputs...), true
}

// difference returns a − b for two metrics of out.
func difference(out map[Metric]Value, a, b Metric) (Value, bool) {
	va, ok := out[a]
	if !ok {
		return Value{}, false
	}
	vb, ok := out[b]
	if !ok {
		return Value{}, false
	}
	v := Value{
		V:          tidy(va.V - vb.V),
		Method:     MethodDerived,
		Estimate:   va.Estimate || vb.Estimate,
		Inputs:     append(slices.Clone(va.Inputs), vb.Inputs...),
		Filed:      latestDate(va.Filed, vb.Filed),
		FirstFiled: latestDate(va.FirstFiled, vb.FirstFiled),
	}
	return v, true
}

func newValue(v float64, method Method, estimate bool, inputs ...winner) Value {
	out := Value{V: v, Method: method, Estimate: estimate}
	for i := range inputs {
		w := &inputs[i]
		out.Inputs = append(out.Inputs, w.obs.ref())
		out.Filed = latestDate(out.Filed, w.obs.fact.Filed)
		out.FirstFiled = latestDate(out.FirstFiled, w.firstFiled)
	}
	return out
}

// earliestDate returns the earlier of a and b, where a zero date counts as
// no date.
func earliestDate(a, b domain.Date) domain.Date {
	if a.IsZero() || b.Before(a) {
		return b
	}
	return a
}

func latestDate(a, b domain.Date) domain.Date {
	if b.After(a) {
		return b
	}
	return a
}

// tidy rounds v to 15 significant digits. Filings report at most a few
// decimals, so this removes only the binary noise of a subtraction or a
// division, such as 4.9 − 3.14 = 1.7600000000000002.
func tidy(v float64) float64 {
	r, err := strconv.ParseFloat(strconv.FormatFloat(v, 'g', 15, 64), 64)
	if err != nil {
		return v
	}
	return r
}
