package fundamentals

import (
	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// durationClass groups a period by its length.
type durationClass int

const (
	classOther durationClass = iota
	classQuarter
	classHalf
	classNine
	classYear
)

// classify returns the class of the period from start to end, both days
// counted. A quarter of a 52/53-week year has 13 or 14 weeks (91 or 98
// days) and a calendar quarter 90 to 92 days, but filers whose quarters end
// on a weekday, such as Coca-Cola, report quarters of 86 to 95 days. The
// windows leave room on both sides of every such length.
func classify(start, end domain.Date) durationClass {
	d := daysIn(start, end)
	switch {
	case d >= 80 && d <= 100:
		return classQuarter
	case d >= 165 && d <= 200:
		return classHalf
	case d >= 255 && d <= 290:
		return classNine
	case d >= 350 && d <= 380:
		return classYear
	default:
		return classOther
	}
}

// daysIn counts the days from start to end, both included.
func daysIn(start, end domain.Date) int {
	return int(end.Time().Sub(start.Time()).Hours()/24) + 1
}

type span struct{ start, end domain.Date }

// fiscalYear is one fiscal year of the calendar. ends holds the quarter
// end dates: four for a complete year, fewer for the year in progress.
type fiscalYear struct {
	fy         int
	start, end domain.Date // end is zero for the year in progress
	ends       []domain.Date
}

// calendarBuilder collects the periods that bound fiscal years and
// quarters.
type calendarBuilder struct {
	annual map[span]int // year-long periods of annual reports, by count
	// bounds counts period ends by start date and class.
	bounds map[domain.Date]map[durationClass]map[domain.Date]int
	votes  map[string]vote // by accession of an annual report
}

// vote is the fiscal year that one annual report gives its latest year.
type vote struct {
	fy    int
	filed domain.Date
	year  span
}

func newCalendarBuilder() *calendarBuilder {
	return &calendarBuilder{
		annual: map[span]int{},
		bounds: map[domain.Date]map[durationClass]map[domain.Date]int{},
		votes:  map[string]vote{},
	}
}

// add counts a duration fact of a periodic report.
func (b *calendarBuilder) add(f *domain.Fact) {
	start := *f.PeriodStart
	class := classify(start, f.PeriodEnd)
	if class == classOther {
		return
	}
	byClass, ok := b.bounds[start]
	if !ok {
		byClass = map[durationClass]map[domain.Date]int{}
		b.bounds[start] = byClass
	}
	if byClass[class] == nil {
		byClass[class] = map[domain.Date]int{}
	}
	byClass[class][f.PeriodEnd]++

	if class != classYear || !isAnnualReport(f.Form) {
		return
	}
	y := span{start, f.PeriodEnd}
	b.annual[y]++
	if f.FiscalYear <= 0 {
		return
	}
	if v, ok := b.votes[f.Accession]; !ok || v.year.end.Before(y.end) {
		b.votes[f.Accession] = vote{fy: f.FiscalYear, filed: f.Filed, year: y}
	}
}

// build returns the fiscal years, oldest first. They are consecutive and
// every one has bounded quarters: the run starts after the last break,
// such as a change of the fiscal year end or a year whose quarters no
// filing bounds. The year in progress comes last when a filing bounds at
// least its first quarter.
func (b *calendarBuilder) build() []fiscalYear {
	chain := b.chain()
	if len(chain) == 0 {
		return nil
	}
	base := b.labelBase(chain)
	var years []fiscalYear
	for i, y := range chain {
		end := y.end
		ends := b.quarterEnds(y.start, &end)
		if ends == nil {
			years = years[:0] // a break: keep only the years after it
			continue
		}
		years = append(years, fiscalYear{fy: base + i, start: y.start, end: y.end, ends: ends})
	}
	next := chain[len(chain)-1].end.AddDays(1)
	if ends := b.quarterEnds(next, nil); len(ends) > 0 {
		years = append(years, fiscalYear{fy: base + len(chain), start: next, ends: ends})
	}
	return years
}

// chain returns the consecutive annual periods that end with the latest
// one, oldest first.
func (b *calendarBuilder) chain() []span {
	var latest domain.Date
	for y := range b.annual {
		if y.end.After(latest) {
			latest = y.end
		}
	}
	var chain []span
	for end := latest; ; {
		y, ok := b.yearEnding(end)
		if !ok {
			break
		}
		chain = append([]span{y}, chain...)
		end = y.start.AddDays(-1)
	}
	return chain
}

// yearEnding returns the annual period that ends on end and that the most
// facts report.
func (b *calendarBuilder) yearEnding(end domain.Date) (span, bool) {
	var best span
	bestCount := 0
	for y, n := range b.annual {
		if !y.end.Equal(end) {
			continue
		}
		if n > bestCount || (n == bestCount && y.start.Before(best.start)) {
			best, bestCount = y, n
		}
	}
	return best, bestCount > 0
}

// labelBase returns the fiscal year of chain[0]. Each annual report votes
// with the fiscal year it gives its latest year; the most common answer
// wins, and on a tie the latest report. Company facts give some filings the
// wrong fiscal year (NVIDIA's 10-Ks for fiscal 2011 to 2014 say one year
// less), so one report does not decide alone. Without votes, a year is
// named for the calendar year it ends in, or the year before when it ends
// in the first week of January.
func (b *calendarBuilder) labelBase(chain []span) int {
	index := make(map[span]int, len(chain))
	for i, y := range chain {
		index[y] = i
	}
	counts := map[int]int{}
	latest := map[int]domain.Date{}
	for _, v := range b.votes {
		i, ok := index[v.year]
		if !ok {
			continue
		}
		base := v.fy - i
		counts[base]++
		if v.filed.After(latest[base]) {
			latest[base] = v.filed
		}
	}
	best, bestCount := 0, 0
	for base, n := range counts {
		if n > bestCount || (n == bestCount && latest[base].After(latest[best])) {
			best, bestCount = base, n
		}
	}
	if bestCount > 0 {
		return best
	}
	end := chain[0].end
	if end.Month == 1 && end.Day <= 7 {
		return end.Year - 1
	}
	return end.Year
}

// quarterEnds returns the quarter end dates of the fiscal year that starts
// on start. For a complete year (end not nil) it returns four dates, or nil
// when the filings do not bound every quarter. For the year in progress it
// returns the dates that the filings bound so far.
func (b *calendarBuilder) quarterEnds(start domain.Date, end *domain.Date) []domain.Date {
	var ends []domain.Date
	prev := start.AddDays(-1)
	for _, ytd := range []durationClass{classQuarter, classHalf, classNine} {
		e, ok := b.commonEnd(start, ytd)
		if !ok {
			e, ok = b.commonEnd(prev.AddDays(1), classQuarter)
		}
		if !ok || !e.After(prev) || classify(prev.AddDays(1), e) != classQuarter ||
			(end != nil && !e.Before(*end)) {
			break
		}
		ends = append(ends, e)
		prev = e
	}
	if end == nil {
		return ends
	}
	if len(ends) != 3 || classify(prev.AddDays(1), *end) != classQuarter {
		return nil
	}
	return append(ends, *end)
}

// commonEnd returns the end date that the most periods of class starting
// on start report, the earliest on a tie.
func (b *calendarBuilder) commonEnd(start domain.Date, class durationClass) (domain.Date, bool) {
	var best domain.Date
	bestCount := 0
	for e, n := range b.bounds[start][class] {
		if n > bestCount || (n == bestCount && e.Before(best)) {
			best, bestCount = e, n
		}
	}
	return best, bestCount > 0
}
