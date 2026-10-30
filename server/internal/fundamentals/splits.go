package fundamentals

import (
	"cmp"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// sameSplitDays is how far apart two dates of one split can be. Filings
// date a split by its approval, record or distribution date, and the
// provider by the first trading day after it; these lie weeks apart, while
// two splits of one company lie years apart.
const sameSplitDays = 180

// confirmSplits returns the confirmed splits, oldest first, the conflicts
// of their evidence, and the confirmed splits that opts.SplitsAsOf left
// out. ratios are the split-ratio facts filed by AsOf, and periods the
// reported values that date an SEC split.
func confirmSplits(cik domain.CIK, ratios []domain.Fact, opts Options, periods map[periodKey][]observation) (splits []domain.Split, conflicts []Conflict, deferred []domain.Split) {
	for _, s := range opts.Splits {
		if (opts.AsOf == nil || !s.Date.After(*opts.AsOf)) && s.Ratio() != 1 {
			splits = append(splits, s)
		}
	}
	provider := len(splits)

	for _, group := range groupRatios(ratios) {
		r := group[0].Value
		date, open := dateSplit(group, r, periods)
		evidence := firstReport(ratios, group)
		if opts.AsOf != nil && date.After(*opts.AsOf) {
			continue
		}
		split := domain.Split{CIK: cik, Date: date, From: 1, To: r, Source: "sec", Evidence: evidence.Accession}
		if r < 1 {
			split.From, split.To = 1/r, 1
		}
		// A provider event near the same date is the same split, and its
		// date and ratio win. Applying both would compound the ratios.
		near := func(p domain.Split) bool { return abs(daysIn(p.Date, date)-1) <= sameSplitDays }
		if slices.ContainsFunc(splits[:provider], func(p domain.Split) bool {
			return near(p) && math.Abs(p.Ratio()-r) <= 1e-6*r
		}) {
			continue
		}
		if !slices.ContainsFunc(splits[:provider], near) {
			splits = append(splits, split)
			if open != nil {
				start := window(group).start
				conflicts = append(conflicts, Conflict{
					Kind:        ConflictSplitDate,
					Concept:     open.Concept,
					PeriodStart: &start,
					PeriodEnd:   window(group).end,
					Restated:    observation{fact: *open, value: open.Value, ratio: 1, scale: 1}.ref(),
					Split:       &split,
				})
			}
		}
		if len(opts.Splits) > 0 || opts.ProviderChecked {
			conflicts = append(conflicts, Conflict{
				Kind:        ConflictSplitEvidence,
				Concept:     evidence.Concept,
				PeriodStart: evidence.PeriodStart,
				PeriodEnd:   evidence.PeriodEnd,
				Restated:    observation{fact: evidence, value: evidence.Value, ratio: 1, scale: 1}.ref(),
				Split:       &split,
			})
		}
	}
	if cut := opts.SplitsAsOf; cut != nil {
		// After the matching, so that SEC evidence dated before the cutoff
		// does not stand in for a provider event after it.
		splits = slices.DeleteFunc(splits, func(s domain.Split) bool {
			if s.Date.After(*cut) {
				deferred = append(deferred, s)
				return true
			}
			return false
		})
		slices.SortStableFunc(deferred, func(a, b domain.Split) int { return compareDates(a.Date, b.Date) })
		conflicts = slices.DeleteFunc(conflicts, func(c Conflict) bool { return c.Split != nil && c.Split.Date.After(*cut) })
	}
	slices.SortStableFunc(splits, func(a, b domain.Split) int { return compareDates(a.Date, b.Date) })
	return splits, conflicts, deferred
}

// groupRatios groups split-ratio facts by split: the same ratio with
// period ends close together. Facts without a positive ratio other than 1
// are dropped.
func groupRatios(ratios []domain.Fact) [][]domain.Fact {
	latest := latestByPeriod(ratios)
	var valid []domain.Fact
	for i := range latest {
		if f := &latest[i]; f.Value > 0 && f.Value != 1 {
			valid = append(valid, *f)
		}
	}
	slices.SortFunc(valid, func(a, b domain.Fact) int {
		return cmp.Or(cmp.Compare(a.Value, b.Value), compareDates(a.PeriodEnd, b.PeriodEnd))
	})
	var groups [][]domain.Fact
	for i := range valid {
		f := &valid[i]
		if i > 0 {
			prev := &valid[i-1]
			if prev.Value == f.Value && daysIn(prev.PeriodEnd, f.PeriodEnd)-1 <= sameSplitDays {
				groups[len(groups)-1] = append(groups[len(groups)-1], *f)
				continue
			}
		}
		groups = append(groups, []domain.Fact{*f})
	}
	return groups
}

// latestByPeriod keeps the latest filing of each split-ratio period, so an
// amendment that corrects a ratio replaces it and does not add a split.
func latestByPeriod(ratios []domain.Fact) []domain.Fact {
	type period struct{ start, end domain.Date }
	latest := map[period]domain.Fact{}
	for i := range ratios {
		f := &ratios[i]
		k := period{end: f.PeriodEnd}
		if f.PeriodStart != nil {
			k.start = *f.PeriodStart
		}
		cur, ok := latest[k]
		if !ok || f.Filed.After(cur.Filed) || (f.Filed.Equal(cur.Filed) && f.Accession > cur.Accession) {
			latest[k] = *f
		}
	}
	return slices.Collect(maps.Values(latest))
}

// window returns the span that the split-ratio facts of one split cover:
// from the earliest start, or instant, to the latest end.
func window(group []domain.Fact) span {
	w := span{group[0].PeriodEnd, group[0].PeriodEnd}
	for i := range group {
		f := &group[i]
		start := f.PeriodEnd
		if f.PeriodStart != nil {
			start = *f.PeriodStart
		}
		if start.Before(w.start) {
			w.start = start
		}
		w.end = latestDate(w.end, f.PeriodEnd)
	}
	return w
}

// dateSplit dates an SEC split of ratio r within the window of its facts.
// A filing inside the window that repeats an earlier per-share value came
// before the split, and one that restates it at r came after. The split is
// dated on the day after the last filing that repeats a value, or at the
// start of the window. It also returns the first filing on or after that
// date whose basis is unknown, or nil.
func dateSplit(group []domain.Fact, r float64, periods map[periodKey][]observation) (domain.Date, *domain.Fact) {
	w := window(group)
	votes := map[string]int{} // by accession: > 0 after the split, < 0 before
	filings := map[string]*domain.Fact{}
	for k, obs := range periods {
		if kindOf(k.concept) != kindPerShare {
			continue
		}
		for i := range obs {
			f := &obs[i].fact
			if f.Filed.Before(w.start) || f.Filed.After(w.end) {
				continue
			}
			if _, ok := filings[f.Accession]; !ok {
				filings[f.Accession] = f
				votes[f.Accession] += 0
			}
			prior, ok := latestBefore(obs, w.start)
			if !ok || prior.Value == 0 {
				continue
			}
			switch {
			case restatedAt(kindPerShare, prior.Value, f.Value, 1, 0):
				votes[f.Accession]--
			case restatedAt(kindPerShare, prior.Value, f.Value, r, splitSlack):
				votes[f.Accession]++
			}
		}
	}
	date := w.start
	for accn, f := range filings {
		if votes[accn] < 0 && !f.Filed.Before(date) {
			date = f.Filed.AddDays(1)
		}
	}
	var open *domain.Fact
	for accn, f := range filings {
		if votes[accn] == 0 && !f.Filed.Before(date) && (open == nil || f.Filed.Before(open.Filed)) {
			open = f
		}
	}
	return date, open
}

// latestBefore returns the latest fact of obs filed before day.
func latestBefore(obs []observation, day domain.Date) (domain.Fact, bool) {
	var found domain.Fact
	ok := false
	for i := range obs {
		f := &obs[i].fact
		if f.Filed.Before(day) && (!ok || f.Filed.After(found.Filed)) {
			found, ok = *f, true
		}
	}
	return found, ok
}

// firstReport returns the earliest filing among ratios that reports the
// ratio of group for one of the group's periods.
func firstReport(ratios, group []domain.Fact) domain.Fact {
	first := group[0]
	for i := range ratios {
		f := &ratios[i]
		if f.Value != first.Value || !slices.ContainsFunc(group, func(g domain.Fact) bool { return samePeriod(f, &g) }) {
			continue
		}
		if f.Filed.Before(first.Filed) || (f.Filed.Equal(first.Filed) && f.Accession < first.Accession) {
			first = *f
		}
	}
	return first
}

func samePeriod(a, b *domain.Fact) bool {
	if (a.PeriodStart == nil) != (b.PeriodStart == nil) || !a.PeriodEnd.Equal(b.PeriodEnd) {
		return false
	}
	return a.PeriodStart == nil || a.PeriodStart.Equal(*b.PeriodStart)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// adjust applies the splits that a filing could not know: those dated after
// both the end of the period and the filing date. A filing made after a
// split restates earlier periods itself.
func adjust(o *observation, splits []domain.Split) {
	kind := kindOf(o.fact.Concept)
	if kind == kindMoney {
		return
	}
	ratio := 1.0
	for _, s := range splits {
		if o.fact.PeriodEnd.Before(s.Date) && o.fact.Filed.Before(s.Date) {
			ratio *= s.Ratio()
		}
	}
	o.ratio = tidy(ratio)
	if kind == kindPerShare {
		o.value = tidy(o.fact.Value / ratio)
	} else {
		o.value = math.Round(o.fact.Value * ratio)
	}
}

// reconcile returns the observation that a period of a per-share or share
// concept uses, and the conflicts between its first and latest filing. obs
// is sorted by filing and adjusted.
func reconcile(obs []observation) (observation, []Conflict) {
	first, last := obs[0], obs[len(obs)-1]
	kind := kindOf(last.fact.Concept)
	use := last
	if kind == kindShares {
		for i := range obs {
			if o := obs[i]; o.value > use.value && (scaled(use.value, o.value, 1e3) || scaled(use.value, o.value, 1e6)) {
				use = o
			}
		}
	}

	a, b := first.fact.Value, last.fact.Value
	if a == 0 || b == 0 {
		return use, nil
	}
	expected := first.ratio / last.ratio
	observed := a / b
	if kind == kindShares {
		observed = b / a
	}
	c := Conflict{
		Concept:     last.fact.Concept,
		PeriodStart: last.fact.PeriodStart,
		PeriodEnd:   last.fact.PeriodEnd,
		Original:    first.ref(),
		Restated:    last.ref(),
		Observed:    tidy(observed),
		Expected:    tidy(expected),
	}
	switch {
	case restatedAt(kind, a, b, expected, splitSlack):
		return use, nil
	case kind == kindShares && slices.ContainsFunc([]float64{1e3, 1e6, 1e-3, 1e-6}, func(s float64) bool {
		return restatedAt(kind, a, b, expected*s, splitSlack)
	}):
		c.Kind = ConflictShareScale
	case expected != 1:
		c.Kind = ConflictSplitMismatch
		// A filing after the split that repeats the old value has not
		// restated it, so the adjusted value of the old basis stays.
		if restatedAt(kind, a, b, 1, 0) {
			use = latestOnBasis(obs, first.ratio)
		}
	case cleanRatio(kind, a, b):
		c.Kind = ConflictUnconfirmedSplit
	default:
		return use, nil
	}
	return use, []Conflict{c}
}

// latestOnBasis returns the latest observation adjusted by ratio.
func latestOnBasis(obs []observation, ratio float64) observation {
	for i := len(obs) - 1; i >= 0; i-- {
		if obs[i].ratio == ratio {
			return obs[i]
		}
	}
	return obs[0]
}

// scaled reports whether big is small times factor, to the rounding of
// share counts.
func scaled(small, big, factor float64) bool {
	return restatedAt(kindShares, small, big, factor, splitSlack)
}

// cleanRatio reports whether exactly one common split ratio turns a into b
// within the rounding of both. A small value fits several ratios, and then
// the restatement proves nothing. Reverse splits are 1-for-N.
func cleanRatio(kind conceptKind, a, b float64) bool {
	n := 0
	for _, r := range splitRatios() {
		if restatedAt(kind, a, b, r, 0) {
			n++
		}
		if r == math.Trunc(r) && restatedAt(kind, a, b, 1/r, 0) {
			n++
		}
	}
	return n == 1
}

// splitRatios returns the forward split ratios that cleanRatio tries.
func splitRatios() []float64 {
	rs := []float64{1.25, 1.5}
	for r := 2.0; r <= 20; r++ {
		rs = append(rs, r)
	}
	return append(rs, 25, 30, 40, 50, 100)
}

// splitSlack is the share of the restated value that restatedAt allows on
// top of rounding when it checks a confirmed split. A restated
// year-to-date value is often the sum of quarters that were each rounded
// after the split.
const splitSlack = 0.01

// restatedAt reports whether the original value a, after a split of ratio r,
// equals the restated value b within the rounding of both plus slack × |b|.
// Per-share values divide by r, share counts multiply.
func restatedAt(kind conceptKind, a, b, r, slack float64) bool {
	want := a * r
	tol := precision(kind, b)/2 + precision(kind, a)*r/2
	if kind == kindPerShare {
		want = a / r
		tol = precision(kind, b)/2 + precision(kind, a)/(2*r)
	}
	return math.Abs(want-b) <= tol+slack*math.Abs(b)+1e-9
}

// precision returns the rounding step of a reported value. Per-share values
// are taken to be rounded to at least cents, and share counts to at most
// millions.
func precision(kind conceptKind, v float64) float64 {
	s := strconv.FormatFloat(math.Abs(v), 'f', -1, 64)
	if kind == kindPerShare {
		decimals := 0
		if i := strings.IndexByte(s, '.'); i >= 0 {
			decimals = len(s) - i - 1
		}
		return math.Min(math.Pow10(-decimals), 0.01)
	}
	if strings.IndexByte(s, '.') >= 0 {
		return 1
	}
	zeros := len(s) - len(strings.TrimRight(s, "0"))
	return math.Pow10(min(zeros, 6))
}
