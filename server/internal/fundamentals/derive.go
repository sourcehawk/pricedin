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
		d.periods[k] = append(d.periods[k], observation{fact: *f, value: f.Value, ratio: 1})
	}

	d.splits, d.conflicts = confirmSplits(cik, ratios, opts)
	d.settle()

	series := Series{Splits: d.splits, Conflicts: d.conflicts}
	for _, y := range cal.build() {
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
	return series, nil
}

type periodKey struct {
	concept    string
	start, end domain.Date
}

// observation is one filing's value for a period. value is the reported
// value after split adjustment by ratio.
type observation struct {
	fact  domain.Fact
	value float64
	ratio float64
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
		method := MethodYTDDifference
		if q.n == 4 {
			method = MethodFYDifference
		}
		use(newValue(tidy(ytd.obs.value-prev.obs.value), method, false, ytd, prev))
	}
	out.FirstFiled = known
	return out, found
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
		inputs = append(inputs, w)
		weighted -= w.obs.value * float64(daysIn(e.start, e.end))
	}
	v := math.Round(weighted / float64(daysIn(q.start, q.end)))
	if v <= 0 {
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
