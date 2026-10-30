package fundamentals_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/fundamentals"
	"github.com/sourcehawk/pricedin/server/internal/sec"
)

type period struct {
	Label      string
	Start, End string
}

func periods(s fundamentals.Series) []period {
	out := make([]period, 0, len(s.Quarters))
	for _, q := range s.Quarters {
		out = append(out, period{q.Label, q.PeriodStart.String(), q.PeriodEnd.String()})
	}
	return out
}

func TestDeriveGivesConsecutiveQuartersWithLabelsAndPeriods(t *testing.T) {
	var facts []domain.Fact
	newCalYear(t, &facts, 2024)
	newCalYear(t, &facts, 2025)

	got := periods(derive(t, facts, fundamentals.Options{}))
	want := []period{
		{"Q1 FY24", "2024-01-01", "2024-03-31"},
		{"Q2 FY24", "2024-04-01", "2024-06-30"},
		{"Q3 FY24", "2024-07-01", "2024-09-30"},
		{"Q4 FY24", "2024-10-01", "2024-12-31"},
		{"Q1 FY25", "2025-01-01", "2025-03-31"},
		{"Q2 FY25", "2025-04-01", "2025-06-30"},
		{"Q3 FY25", "2025-07-01", "2025-09-30"},
		{"Q4 FY25", "2025-10-01", "2025-12-31"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("quarters (-want +got):\n%s", diff)
	}
}

func TestDeriveEndsWithTheReportedQuartersOfTheYearInProgress(t *testing.T) {
	var facts []domain.Fact
	newCalYear(t, &facts, 2025)
	newFiling(t, &facts, "q1-2026", "10-Q", "2026-04-25", 2026, "Q1").add(sec.NetIncomeLoss, "2026-01-01", "2026-03-31", 1)
	newFiling(t, &facts, "q2-2026", "10-Q", "2026-07-25", 2026, "Q2").add(sec.NetIncomeLoss, "2026-01-01", "2026-06-30", 2)

	s := derive(t, facts, fundamentals.Options{})
	last := s.Quarters[len(s.Quarters)-1]
	if last.Label != "Q2 FY26" || last.PeriodStart.String() != "2026-04-01" || last.PeriodEnd.String() != "2026-06-30" {
		t.Errorf("last quarter = %s %s..%s, want Q2 FY26 2026-04-01..2026-06-30", last.Label, last.PeriodStart, last.PeriodEnd)
	}
}

func TestDeriveLabelsFiscalYearsByTheMajorityOfThe10KFiscalYears(t *testing.T) {
	// Years end in late January, as NVIDIA's do. The 10-K of the middle year
	// gives the wrong fiscal year, as some filings in SEC company facts do.
	var facts []domain.Fact
	newYear(t, &facts, 2024, "2023-01-30", "2023-04-30", "2023-07-30", "2023-10-29", "2024-01-28")
	newYear(t, &facts, 2025, "2024-01-29", "2024-04-28", "2024-07-28", "2024-10-27", "2025-01-26")
	newYear(t, &facts, 2026, "2025-01-27", "2025-04-27", "2025-07-27", "2025-10-26", "2026-01-25")
	for i := range facts {
		if facts[i].Accession == "k-2025" {
			facts[i].FiscalYear = 2024
		}
	}

	s := derive(t, facts, fundamentals.Options{})
	if got := quarter(t, s, "Q4 FY25").PeriodEnd.String(); got != "2025-01-26" {
		t.Errorf("Q4 FY25 ends %s, want 2025-01-26", got)
	}
	if got := s.Quarters[0].Label; got != "Q1 FY24" {
		t.Errorf("first quarter = %s, want Q1 FY24", got)
	}
}

func TestDeriveCountsA14WeekQuarterAsAQuarter(t *testing.T) {
	// A 53-week year: Q4 has 98 days.
	var facts []domain.Fact
	newYear(t, &facts, 2025, "2024-12-29", "2025-03-29", "2025-06-28", "2025-09-27", "2026-01-03")

	q := quarter(t, derive(t, facts, fundamentals.Options{}), "Q4 FY25")
	if q.PeriodStart.String() != "2025-09-28" || q.PeriodEnd.String() != "2026-01-03" {
		t.Errorf("Q4 FY25 = %s..%s, want 2025-09-28..2026-01-03", q.PeriodStart, q.PeriodEnd)
	}
}

func TestDeriveStartsAfterAChangeOfTheFiscalYearEnd(t *testing.T) {
	// The fiscal year ended in June until 2023, then moved to December after
	// a transition period from July to December 2023.
	var facts []domain.Fact
	newYear(t, &facts, 2023, "2022-07-01", "2022-09-30", "2022-12-31", "2023-03-31", "2023-06-30")
	newCalYear(t, &facts, 2024)
	newCalYear(t, &facts, 2025)

	s := derive(t, facts, fundamentals.Options{})
	if len(s.Quarters) != 8 || s.Quarters[0].Label != "Q1 FY24" {
		t.Errorf("quarters = %v, want 8 from Q1 FY24", periods(s))
	}
}

func TestDeriveDerivesMissingQuartersFromYearToDateValues(t *testing.T) {
	// 10-Qs report cash flows year to date only.
	var facts []domain.Fact
	y := newCalYear(t, &facts, 2025)
	y.q1.add(sec.NetCashProvidedByUsedInOperatingActivities, "2025-01-01", "2025-03-31", 100)
	y.q2.add(sec.NetCashProvidedByUsedInOperatingActivities, "2025-01-01", "2025-06-30", 250)
	y.q3.add(sec.NetCashProvidedByUsedInOperatingActivities, "2025-01-01", "2025-09-30", 420)
	y.k.add(sec.NetCashProvidedByUsedInOperatingActivities, "2025-01-01", "2025-12-31", 600)

	s := derive(t, facts, fundamentals.Options{})
	tests := []struct {
		label  string
		want   float64
		method fundamentals.Method
		inputs []string
	}{
		{"Q1 FY25", 100, fundamentals.MethodReported, []string{"q1-2025"}},
		{"Q2 FY25", 150, fundamentals.MethodYTDDifference, []string{"q2-2025", "q1-2025"}},
		{"Q3 FY25", 170, fundamentals.MethodYTDDifference, []string{"q3-2025", "q2-2025"}},
		{"Q4 FY25", 180, fundamentals.MethodFYDifference, []string{"k-2025", "q3-2025"}},
	}
	for _, tt := range tests {
		v := value(t, s, tt.label, fundamentals.OperatingCashFlow)
		if v.V != tt.want || v.Method != tt.method || !cmp.Equal(accessions(v), tt.inputs) {
			t.Errorf("%s = %v %s %v, want %v %s %v", tt.label, v.V, v.Method, accessions(v), tt.want, tt.method, tt.inputs)
		}
	}
}

func TestDerivePrefersAReportedQuarterOverADifference(t *testing.T) {
	var facts []domain.Fact
	y := newCalYear(t, &facts, 2025)
	y.q1.add(sec.EarningsPerShareDiluted, "2025-01-01", "2025-03-31", 1.00)
	y.q2.add(sec.EarningsPerShareDiluted, "2025-04-01", "2025-06-30", 1.10)
	y.q2.add(sec.EarningsPerShareDiluted, "2025-01-01", "2025-06-30", 2.11)

	v := value(t, derive(t, facts, fundamentals.Options{}), "Q2 FY25", fundamentals.EPSDiluted)
	if v.V != 1.10 || v.Method != fundamentals.MethodReported {
		t.Errorf("Q2 FY25 EPS = %v %s, want 1.10 reported", v.V, v.Method)
	}
}

func TestDeriveRemovesFloatingPointNoiseFromDifferences(t *testing.T) {
	var facts []domain.Fact
	y := newCalYear(t, &facts, 2025)
	y.q3.add(sec.EarningsPerShareDiluted, "2025-01-01", "2025-09-30", 3.14)
	y.k.add(sec.EarningsPerShareDiluted, "2025-01-01", "2025-12-31", 4.9)

	if got := value(t, derive(t, facts, fundamentals.Options{}), "Q4 FY25", fundamentals.EPSDiluted).V; got != 1.76 {
		t.Errorf("Q4 FY25 EPS = %v, want exactly 1.76", got)
	}
}

func TestDeriveUsesTheLatestFilingOfEachPeriodAsOfADate(t *testing.T) {
	var facts []domain.Fact
	newCalYear(t, &facts, 2024)
	y := newCalYear(t, &facts, 2025)
	y.q1.add(sec.EarningsPerShareDiluted, "2025-01-01", "2025-03-31", 1.00)
	newFiling(t, &facts, "q1a-2025", "10-Q/A", "2025-05-30", 2025, "Q1").add(sec.EarningsPerShareDiluted, "2025-01-01", "2025-03-31", 1.05)

	tests := []struct {
		asOf  string
		want  float64
		accn  string
		filed string
	}{
		{"2025-05-29", 1.00, "q1-2025", "2025-04-25"},
		{"2025-05-30", 1.05, "q1a-2025", "2025-05-30"},
		{"", 1.05, "q1a-2025", "2025-05-30"},
	}
	for _, tt := range tests {
		t.Run(tt.asOf, func(t *testing.T) {
			var opts fundamentals.Options
			if tt.asOf != "" {
				opts.AsOf = dayPtr(t, tt.asOf)
			}
			v := value(t, derive(t, facts, opts), "Q1 FY25", fundamentals.EPSDiluted)
			if v.V != tt.want || !cmp.Equal(accessions(v), []string{tt.accn}) || v.Filed.String() != tt.filed {
				t.Errorf("Q1 FY25 EPS = %v from %v filed %s, want %v from %s filed %s", v.V, accessions(v), v.Filed, tt.want, tt.accn, tt.filed)
			}
			if v.FirstFiled.String() != "2025-04-25" {
				t.Errorf("FirstFiled = %s, want 2025-04-25", v.FirstFiled)
			}
		})
	}
}

func TestDeriveAsOfHidesEveryLaterFiling(t *testing.T) {
	var facts []domain.Fact
	for _, year := range []int{2024, 2025} {
		y := newCalYear(t, &facts, year)
		y.q1.add(sec.EarningsPerShareDiluted, y.start, y.q1End, 1)
		y.q2.add(sec.EarningsPerShareDiluted, y.start, y.q2End, 2)
		y.q3.add(sec.EarningsPerShareDiluted, y.start, y.q3End, 3)
		y.k.add(sec.EarningsPerShareDiluted, y.start, y.end, 4)
	}
	asOf := day(t, "2025-08-01")

	s := derive(t, facts, fundamentals.Options{AsOf: &asOf})
	if last := s.Quarters[len(s.Quarters)-1].Label; last != "Q2 FY25" {
		t.Errorf("last quarter = %s, want Q2 FY25", last)
	}
	for _, q := range s.Quarters {
		for m, v := range q.Values {
			for _, in := range v.Inputs {
				if in.Filed.After(asOf) {
					t.Errorf("%s %s uses %s filed %s, after %s", q.Label, m, in.Accession, in.Filed, asOf)
				}
			}
		}
	}
}

func TestDeriveIgnoresFormsOtherThanPeriodicReports(t *testing.T) {
	var facts []domain.Fact
	y := newCalYear(t, &facts, 2025)
	y.q1.add(sec.EarningsPerShareDiluted, "2025-01-01", "2025-03-31", 1.00)
	newFiling(t, &facts, "8k-2025", "8-K", "2025-06-01", 0, "").add(sec.EarningsPerShareDiluted, "2025-01-01", "2025-03-31", 9.99)
	newFiling(t, &facts, "proxy-2025", "DEF 14A", "2025-06-02", 0, "").add(sec.EarningsPerShareDiluted, "2025-01-01", "2025-03-31", 8.88)

	if v := value(t, derive(t, facts, fundamentals.Options{}), "Q1 FY25", fundamentals.EPSDiluted); v.V != 1.00 {
		t.Errorf("Q1 FY25 EPS = %v from %v, want 1.00 from the 10-Q", v.V, accessions(v))
	}
}

func TestDeriveTakesRevenueFromTheFirstConceptInPriorityOrder(t *testing.T) {
	var facts []domain.Fact
	y := newCalYear(t, &facts, 2025)
	// Q1: both concepts; Revenues is the total, the contract revenue a part.
	y.q1.add(sec.Revenues, "2025-01-01", "2025-03-31", 500)
	y.q1.add(sec.RevenueFromContractWithCustomerExcludingAssessedTax, "2025-01-01", "2025-03-31", 150)
	// Q2: only the second concept.
	y.q2.add(sec.RevenueFromContractWithCustomerExcludingAssessedTax, "2025-04-01", "2025-06-30", 160)
	// Q4: Revenues lacks the 9M value, so the year difference must come
	// from the second concept alone, never from a mix of concepts.
	y.k.add(sec.Revenues, "2025-01-01", "2025-12-31", 2000)
	y.q3.add(sec.RevenueFromContractWithCustomerExcludingAssessedTax, "2025-01-01", "2025-09-30", 520)
	y.k.add(sec.RevenueFromContractWithCustomerExcludingAssessedTax, "2025-01-01", "2025-12-31", 700)

	s := derive(t, facts, fundamentals.Options{})
	tests := []struct {
		label   string
		want    float64
		concept sec.Concept
	}{
		{"Q1 FY25", 500, sec.Revenues},
		{"Q2 FY25", 160, sec.RevenueFromContractWithCustomerExcludingAssessedTax},
		{"Q4 FY25", 180, sec.RevenueFromContractWithCustomerExcludingAssessedTax},
	}
	for _, tt := range tests {
		v := value(t, s, tt.label, fundamentals.Revenue)
		if v.V != tt.want {
			t.Errorf("%s revenue = %v, want %v", tt.label, v.V, tt.want)
		}
		for _, in := range v.Inputs {
			if in.Concept != string(tt.concept) {
				t.Errorf("%s revenue input concept = %s, want %s", tt.label, in.Concept, tt.concept)
			}
		}
	}
}

func TestDeriveComputesFreeCashFlowAndOperatingCosts(t *testing.T) {
	var facts []domain.Fact
	y := newCalYear(t, &facts, 2025)
	y.q1.add(sec.NetCashProvidedByUsedInOperatingActivities, "2025-01-01", "2025-03-31", 100)
	y.q1.add(sec.PaymentsToAcquirePropertyPlantAndEquipment, "2025-01-01", "2025-03-31", 30)
	y.q1.add(sec.Revenues, "2025-01-01", "2025-03-31", 500)
	y.q1.add(sec.OperatingIncomeLoss, "2025-01-01", "2025-03-31", 120)

	s := derive(t, facts, fundamentals.Options{})
	fcf := value(t, s, "Q1 FY25", fundamentals.FreeCashFlow)
	if fcf.V != 70 || fcf.Method != fundamentals.MethodDerived || len(fcf.Inputs) != 2 {
		t.Errorf("free cash flow = %v %s with %d inputs, want 70 derived with 2", fcf.V, fcf.Method, len(fcf.Inputs))
	}
	costs := value(t, s, "Q1 FY25", fundamentals.OperatingCosts)
	if costs.V != 380 || costs.Method != fundamentals.MethodDerived || len(costs.Inputs) != 2 {
		t.Errorf("operating costs = %v %s with %d inputs, want 380 derived with 2", costs.V, costs.Method, len(costs.Inputs))
	}
}

// fiftyThreeWeekShares adds diluted share counts to a 53-week year with
// 91-day Q1 to Q3 and a 98-day Q4.
func fiftyThreeWeekShares(t *testing.T, facts *[]domain.Fact, q2 bool, fy float64) {
	t.Helper()
	y := newYear(t, facts, 2025, "2024-12-29", "2025-03-29", "2025-06-28", "2025-09-27", "2026-01-03")
	y.q1.add(sec.WeightedAverageNumberOfDilutedSharesOutstanding, "2024-12-29", "2025-03-29", 1000)
	if q2 {
		y.q2.add(sec.WeightedAverageNumberOfDilutedSharesOutstanding, "2025-03-30", "2025-06-28", 1010)
	}
	y.q3.add(sec.WeightedAverageNumberOfDilutedSharesOutstanding, "2025-06-29", "2025-09-27", 1020)
	y.k.add(sec.WeightedAverageNumberOfDilutedSharesOutstanding, "2024-12-29", "2026-01-03", fy)
}

func TestDeriveEstimatesQ4SharesWithDurationWeights(t *testing.T) {
	var facts []domain.Fact
	fiftyThreeWeekShares(t, &facts, true, 1015)

	// (1015 × 371 − (1000 + 1010 + 1020) × 91) / 98 = 1028.93, rounded to
	// whole shares.
	v := value(t, derive(t, facts, fundamentals.Options{}), "Q4 FY25", fundamentals.DilutedShares)
	if v.V != 1029 || v.Method != fundamentals.MethodWeightedAverageQ4 || !v.Estimate {
		t.Errorf("Q4 FY25 shares = %v %s estimate=%v, want 1029 weighted_average_q4 estimate", v.V, v.Method, v.Estimate)
	}
	if want := []string{"k-2025", "q1-2025", "q2-2025", "q3-2025"}; !cmp.Equal(accessions(v), want) {
		t.Errorf("inputs = %v, want %v", accessions(v), want)
	}
}

func TestDeriveLeavesQ4SharesUnavailableWhenTheRuleCannotApply(t *testing.T) {
	tests := []struct {
		name string
		q2   bool
		fy   float64
	}{
		{"a quarter is missing", false, 1015},
		{"the result is not positive", true, 700},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var facts []domain.Fact
			fiftyThreeWeekShares(t, &facts, tt.q2, tt.fy)
			if v, ok := quarter(t, derive(t, facts, fundamentals.Options{}), "Q4 FY25").Values[fundamentals.DilutedShares]; ok {
				t.Errorf("Q4 FY25 shares = %v, want none", v.V)
			}
		})
	}
}

func TestDeriveRejectsFactsOfAnotherCompany(t *testing.T) {
	var facts []domain.Fact
	newCalYear(t, &facts, 2025)
	facts[0].CIK = 1
	if _, err := fundamentals.Derive(testCIK, facts, fundamentals.Options{}); err == nil {
		t.Error("Derive() error = nil, want an error for a fact of another CIK")
	}
}

func TestDeriveRejectsASplitWithoutAPositiveRatio(t *testing.T) {
	var facts []domain.Fact
	newCalYear(t, &facts, 2025)
	opts := fundamentals.Options{Splits: []domain.Split{{Date: day(t, "2025-06-10"), From: 0, To: 2}}}
	if _, err := fundamentals.Derive(testCIK, facts, opts); err == nil {
		t.Error("Derive() error = nil, want an error for a split with From 0")
	}
}

func TestDeriveGivesAnEmptySeriesWithoutAnAnnualReport(t *testing.T) {
	var facts []domain.Fact
	newFiling(t, &facts, "q1-2026", "10-Q", "2026-04-25", 2026, "Q1").add(sec.EarningsPerShareDiluted, "2026-01-01", "2026-03-31", 1)
	if s := derive(t, facts, fundamentals.Options{}); len(s.Quarters) != 0 {
		t.Errorf("quarters = %v, want none", periods(s))
	}
}
