package fundamentals_test

import (
	"math"
	"strconv"
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/fundamentals"
	"github.com/sourcehawk/pricedin/server/internal/sec"
)

const testCIK domain.CIK = 999

func day(t *testing.T, s string) domain.Date {
	t.Helper()
	d, err := domain.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func dayPtr(t *testing.T, s string) *domain.Date {
	t.Helper()
	d := day(t, s)
	return &d
}

// filing is one synthetic SEC filing that reports facts.
type filing struct {
	t     *testing.T
	accn  string
	form  string
	filed string
	fy    int
	fp    string
	facts *[]domain.Fact
	cik   domain.CIK
}

// newFiling starts a filing whose facts land in facts.
func newFiling(t *testing.T, facts *[]domain.Fact, accn, form, filed string, fy int, fp string) *filing {
	return &filing{t: t, accn: accn, form: form, filed: filed, fy: fy, fp: fp, facts: facts, cik: testCIK}
}

func unitOf(concept sec.Concept) string {
	switch concept {
	case sec.EarningsPerShareDiluted, sec.CommonStockDividendsPerShareDeclared, sec.CommonStockDividendsPerShareCashPaid:
		return "USD/shares"
	case sec.WeightedAverageNumberOfDilutedSharesOutstanding:
		return "shares"
	case sec.StockholdersEquityNoteStockSplitConversionRatio1:
		return "pure"
	default:
		return "USD"
	}
}

// add reports concept for the period from start to end (both inclusive).
// An empty start makes an instant fact.
func (f *filing) add(concept sec.Concept, start, end string, v float64) {
	f.t.Helper()
	fact := domain.Fact{
		CIK:          f.cik,
		Taxonomy:     concept.Taxonomy(),
		Concept:      string(concept),
		Unit:         unitOf(concept),
		PeriodEnd:    day(f.t, end),
		Value:        v,
		Form:         f.form,
		FiscalYear:   f.fy,
		FiscalPeriod: f.fp,
		Filed:        day(f.t, f.filed),
		Accession:    f.accn,
	}
	if start != "" {
		fact.PeriodStart = dayPtr(f.t, start)
	}
	*f.facts = append(*f.facts, fact)
}

func derive(t *testing.T, facts []domain.Fact, opts fundamentals.Options) fundamentals.Series {
	t.Helper()
	s, err := fundamentals.Derive(testCIK, facts, opts)
	if err != nil {
		t.Fatalf("Derive() error = %v", err)
	}
	return s
}

// quarter returns the quarter with label, or fails the test.
func quarter(t *testing.T, s fundamentals.Series, label string) fundamentals.Quarter {
	t.Helper()
	for _, q := range s.Quarters {
		if q.Label == label {
			return q
		}
	}
	labels := make([]string, 0, len(s.Quarters))
	for _, q := range s.Quarters {
		labels = append(labels, q.Label)
	}
	t.Fatalf("no quarter %s; have %v", label, labels)
	return fundamentals.Quarter{}
}

func value(t *testing.T, s fundamentals.Series, label string, m fundamentals.Metric) fundamentals.Value {
	t.Helper()
	v, ok := quarter(t, s, label).Values[m]
	if !ok {
		t.Fatalf("%s has no %s", label, m)
	}
	return v
}

func accessions(v fundamentals.Value) []string {
	out := make([]string, 0, len(v.Inputs))
	for _, in := range v.Inputs {
		out = append(out, in.Accession)
	}
	return out
}

func near(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

// fiscalYear is a synthetic fiscal year with one 10-Q per quarter and a
// 10-K. Each filing reports net income for its year-to-date period, so the
// year's quarters are bounded.
type fiscalYear struct {
	q1, q2, q3, k                   *filing
	start, q1End, q2End, q3End, end string
}

// newYear adds a fiscal year with the given boundaries. Its filings are
// named "q1-<fy>" to "q3-<fy>" and "k-<fy>", filed 25 days after each
// quarter and 50 days after the year.
func newYear(t *testing.T, facts *[]domain.Fact, fy int, start, q1End, q2End, q3End, end string) fiscalYear {
	t.Helper()
	after := func(s string, n int) string { return day(t, s).AddDays(n).String() }
	n := strconv.Itoa(fy)
	y := fiscalYear{
		q1:    newFiling(t, facts, "q1-"+n, "10-Q", after(q1End, 25), fy, "Q1"),
		q2:    newFiling(t, facts, "q2-"+n, "10-Q", after(q2End, 25), fy, "Q2"),
		q3:    newFiling(t, facts, "q3-"+n, "10-Q", after(q3End, 25), fy, "Q3"),
		k:     newFiling(t, facts, "k-"+n, "10-K", after(end, 50), fy, "FY"),
		start: start, q1End: q1End, q2End: q2End, q3End: q3End, end: end,
	}
	y.q1.add(sec.NetIncomeLoss, start, q1End, 10)
	y.q2.add(sec.NetIncomeLoss, start, q2End, 20)
	y.q3.add(sec.NetIncomeLoss, start, q3End, 30)
	y.k.add(sec.NetIncomeLoss, start, end, 40)
	return y
}

// newCalYear adds a fiscal year that matches calendar year fy. Its filings
// are filed on April 25, July 25, October 25 and February 19 of the next
// year.
func newCalYear(t *testing.T, facts *[]domain.Fact, fy int) fiscalYear {
	t.Helper()
	n := strconv.Itoa(fy)
	return newYear(t, facts, fy, n+"-01-01", n+"-03-31", n+"-06-30", n+"-09-30", n+"-12-31")
}
