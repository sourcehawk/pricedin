package fundamentals_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/fundamentals"
	"github.com/sourcehawk/pricedin/server/internal/sec"
)

// splitYears gives two calendar years around a 2-for-1 split on
// 2025-06-10. The 2024 filings and the 2025 Q1 10-Q came before the
// split; the 2025 Q2 10-Q came after it and restated its 2024 comparative
// with restatedQ2 (0 for none). It returns the 2025 filings.
func splitYears(t *testing.T, facts *[]domain.Fact, restatedQ2 float64) fiscalYear {
	t.Helper()
	y24 := newCalYear(t, facts, 2024)
	y24.q1.add(sec.EarningsPerShareDiluted, "2024-01-01", "2024-03-31", 2.00)
	y24.q1.add(sec.WeightedAverageNumberOfDilutedSharesOutstanding, "2024-01-01", "2024-03-31", 100)
	y24.q1.add(sec.CommonStockDividendsPerShareDeclared, "2024-01-01", "2024-03-31", 0.50)
	y24.q2.add(sec.EarningsPerShareDiluted, "2024-04-01", "2024-06-30", 2.10)
	y25 := newCalYear(t, facts, 2025)
	y25.q1.add(sec.EarningsPerShareDiluted, "2025-01-01", "2025-03-31", 2.20)
	y25.q2.add(sec.EarningsPerShareDiluted, "2025-04-01", "2025-06-30", 1.15)
	if restatedQ2 != 0 {
		y25.q2.add(sec.EarningsPerShareDiluted, "2024-04-01", "2024-06-30", restatedQ2)
	}
	return y25
}

// providerSplit is a ratio-for-1 split as the price provider gives it.
func providerSplit(t *testing.T, date string, ratio float64) domain.Split {
	return domain.Split{Symbol: "TEST", Date: day(t, date), From: 1, To: ratio, Source: "massive", Evidence: "massive-" + date}
}

type adjusted struct {
	Label      string
	Metric     fundamentals.Metric
	V          float64
	Accession  string
	SplitRatio float64
}

func adjustedValues(t *testing.T, s fundamentals.Series, want []adjusted) []adjusted {
	t.Helper()
	got := make([]adjusted, 0, len(want))
	for _, w := range want {
		v := value(t, s, w.Label, w.Metric)
		got = append(got, adjusted{w.Label, w.Metric, v.V, v.Inputs[0].Accession, v.Inputs[0].SplitRatio})
	}
	return got
}

func TestDeriveAdjustsValuesFromBeforeAConfirmedSplit(t *testing.T) {
	var facts []domain.Fact
	splitYears(t, &facts, 1.05)
	split := providerSplit(t, "2025-06-10", 2)

	s := derive(t, facts, fundamentals.Options{Splits: []domain.Split{split}})
	want := []adjusted{
		// Filed before the split and never restated: divided by 2.
		{"Q1 FY24", fundamentals.EPSDiluted, 1.00, "q1-2024", 2},
		{"Q1 FY24", fundamentals.DividendsPerShare, 0.25, "q1-2024", 2},
		// Share counts are multiplied by 2.
		{"Q1 FY24", fundamentals.DilutedShares, 200, "q1-2024", 2},
		// Restated by the 10-Q filed after the split: used as filed.
		{"Q2 FY24", fundamentals.EPSDiluted, 1.05, "q2-2025", 1},
		{"Q1 FY25", fundamentals.EPSDiluted, 1.10, "q1-2025", 2},
		// After the split: as filed.
		{"Q2 FY25", fundamentals.EPSDiluted, 1.15, "q2-2025", 1},
	}
	if diff := cmp.Diff(want, adjustedValues(t, s, want)); diff != "" {
		t.Errorf("values (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]domain.Split{split}, s.Splits); diff != "" {
		t.Errorf("Splits (-want +got):\n%s", diff)
	}
	if len(s.Conflicts) != 0 {
		t.Errorf("Conflicts = %+v, want none", s.Conflicts)
	}
}

func TestDeriveIgnoresASplitAfterAsOf(t *testing.T) {
	var facts []domain.Fact
	splitYears(t, &facts, 0)
	asOf := day(t, "2025-06-01")

	s := derive(t, facts, fundamentals.Options{AsOf: &asOf, Splits: []domain.Split{providerSplit(t, "2025-06-10", 2)}})
	if v := value(t, s, "Q1 FY24", fundamentals.EPSDiluted); v.V != 2.00 {
		t.Errorf("Q1 FY24 EPS = %v, want 2.00 as filed", v.V)
	}
	if len(s.Splits) != 0 {
		t.Errorf("Splits = %+v, want none", s.Splits)
	}
}

func TestDeriveConfirmsASplitFromTheSECRatioConcept(t *testing.T) {
	var facts []domain.Fact
	y25 := splitYears(t, &facts, 1.05)
	y25.q2.add(sec.StockholdersEquityNoteStockSplitConversionRatio1, "", "2025-06-10", 2)

	s := derive(t, facts, fundamentals.Options{})
	want := []domain.Split{{CIK: testCIK, Date: day(t, "2025-06-10"), From: 1, To: 2, Source: "sec", Evidence: "q2-2025"}}
	if diff := cmp.Diff(want, s.Splits); diff != "" {
		t.Errorf("Splits (-want +got):\n%s", diff)
	}
	if v := value(t, s, "Q1 FY24", fundamentals.EPSDiluted); v.V != 1.00 {
		t.Errorf("Q1 FY24 EPS = %v, want 1.00", v.V)
	}
}

func TestDeriveMergesSECEvidenceWithTheMatchingProviderEvent(t *testing.T) {
	// The filing dates the split three days before the first trading day
	// that the provider gives.
	var facts []domain.Fact
	y25 := splitYears(t, &facts, 1.05)
	y25.q2.add(sec.StockholdersEquityNoteStockSplitConversionRatio1, "", "2025-06-07", 2)
	split := providerSplit(t, "2025-06-10", 2)

	s := derive(t, facts, fundamentals.Options{Splits: []domain.Split{split}})
	if diff := cmp.Diff([]domain.Split{split}, s.Splits); diff != "" {
		t.Errorf("Splits (-want +got):\n%s", diff)
	}
	if len(s.Conflicts) != 0 {
		t.Errorf("Conflicts = %+v, want none", s.Conflicts)
	}
}

func TestDeriveFlagsSECSplitEvidenceThatNoProviderEventMatches(t *testing.T) {
	var facts []domain.Fact
	y25 := splitYears(t, &facts, 1.05)
	y25.q2.add(sec.StockholdersEquityNoteStockSplitConversionRatio1, "", "2025-06-10", 2)
	other := providerSplit(t, "2020-03-02", 3)

	s := derive(t, facts, fundamentals.Options{Splits: []domain.Split{other}})
	if len(s.Splits) != 2 {
		t.Fatalf("Splits = %+v, want the provider split and the SEC split", s.Splits)
	}
	var found bool
	for _, c := range s.Conflicts {
		if c.Kind == fundamentals.ConflictSplitEvidence && c.Split != nil && c.Split.Date.String() == "2025-06-10" && c.Restated.Accession == "q2-2025" {
			found = true
		}
	}
	if !found {
		t.Errorf("Conflicts = %+v, want a split_evidence conflict for the 2025-06-10 split", s.Conflicts)
	}
	if v := value(t, s, "Q1 FY24", fundamentals.EPSDiluted); v.V != 1.00 {
		t.Errorf("Q1 FY24 EPS = %v, want 1.00: the SEC split still applies", v.V)
	}
}

func TestDeriveDoesNotInferASplitFromARestatementAlone(t *testing.T) {
	var facts []domain.Fact
	splitYears(t, &facts, 1.05)

	s := derive(t, facts, fundamentals.Options{})
	if v := value(t, s, "Q1 FY24", fundamentals.EPSDiluted); v.V != 2.00 {
		t.Errorf("Q1 FY24 EPS = %v, want 2.00 as filed", v.V)
	}
	if len(s.Splits) != 0 {
		t.Errorf("Splits = %+v, want none", s.Splits)
	}
	want := []fundamentals.Conflict{{
		Kind:        fundamentals.ConflictUnconfirmedSplit,
		Concept:     string(sec.EarningsPerShareDiluted),
		PeriodStart: dayPtr(t, "2024-04-01"),
		PeriodEnd:   day(t, "2024-06-30"),
		Observed:    2,
		Expected:    1,
	}}
	if diff := cmp.Diff(want, s.Conflicts, conflictSummary); diff != "" {
		t.Errorf("Conflicts (-want +got):\n%s", diff)
	}
	if c := s.Conflicts[0]; c.Original.Accession != "q2-2024" || c.Restated.Accession != "q2-2025" {
		t.Errorf("conflict filings = %s and %s, want q2-2024 and q2-2025", c.Original.Accession, c.Restated.Accession)
	}
}

// conflictSummary compares conflicts without their source references.
var conflictSummary = cmp.FilterPath(func(p cmp.Path) bool {
	switch p.Last().String() {
	case ".Original", ".Restated":
		return true
	}
	return false
}, cmp.Ignore())

func TestDeriveFlagsARestatementThatTheConfirmedSplitDoesNotExplain(t *testing.T) {
	var facts []domain.Fact
	splitYears(t, &facts, 2.00)

	s := derive(t, facts, fundamentals.Options{Splits: []domain.Split{providerSplit(t, "2025-06-10", 2)}})
	want := []fundamentals.Conflict{{
		Kind:        fundamentals.ConflictSplitMismatch,
		Concept:     string(sec.EarningsPerShareDiluted),
		PeriodStart: dayPtr(t, "2024-04-01"),
		PeriodEnd:   day(t, "2024-06-30"),
		Observed:    1.05,
		Expected:    2,
	}}
	if diff := cmp.Diff(want, s.Conflicts, conflictSummary); diff != "" {
		t.Errorf("Conflicts (-want +got):\n%s", diff)
	}
}

func TestDeriveIgnoresAnOrdinaryRestatement(t *testing.T) {
	tests := []struct {
		name     string
		restated float64
	}{
		{"a small correction", 2.12},
		// Apple restated a fiscal 2009 EPS from 1.35 to 2.01 for a change of
		// accounting: a ratio close to 2/3, but no split is 2-for-3 reverse.
		{"a change of accounting", 3.13},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var facts []domain.Fact
			splitYears(t, &facts, tt.restated)
			if s := derive(t, facts, fundamentals.Options{}); len(s.Conflicts) != 0 {
				t.Errorf("Conflicts = %+v, want none", s.Conflicts)
			}
		})
	}
}

func TestDeriveUsesTheLargerShareCountWhenFilingsDifferByAThousand(t *testing.T) {
	var facts []domain.Fact
	newCalYear(t, &facts, 2024)
	y := newCalYear(t, &facts, 2025)
	y.q1.add(sec.WeightedAverageNumberOfDilutedSharesOutstanding, "2025-01-01", "2025-03-31", 4_334_000_000)
	newFiling(t, &facts, "q1-2026", "10-Q", "2026-04-25", 2026, "Q1").add(sec.WeightedAverageNumberOfDilutedSharesOutstanding, "2025-01-01", "2025-03-31", 4_334)

	s := derive(t, facts, fundamentals.Options{})
	if v := value(t, s, "Q1 FY25", fundamentals.DilutedShares); v.V != 4_334_000_000 || v.Inputs[0].Accession != "q1-2025" {
		t.Errorf("Q1 FY25 shares = %v from %v, want 4334000000 from q1-2025", v.V, accessions(v))
	}
	if len(s.Conflicts) != 1 || s.Conflicts[0].Kind != fundamentals.ConflictShareScale {
		t.Errorf("Conflicts = %+v, want one share_scale conflict", s.Conflicts)
	}
}

func TestDeriveAcceptsARestatedSumOfSeparatelyRoundedQuarters(t *testing.T) {
	// Apple's 9M dividends of fiscal 2013 were $8.35 before its 7-for-1
	// split. The restatement rounded each quarter first (0.38 + 0.38 +
	// 0.44) and gave 1.20, not 8.35 / 7 = 1.193.
	var facts []domain.Fact
	newCalYear(t, &facts, 2024).q3.add(sec.CommonStockDividendsPerShareDeclared, "2024-01-01", "2024-09-30", 8.35)
	newCalYear(t, &facts, 2025).q3.add(sec.CommonStockDividendsPerShareDeclared, "2024-01-01", "2024-09-30", 1.20)

	s := derive(t, facts, fundamentals.Options{Splits: []domain.Split{providerSplit(t, "2025-06-10", 7)}})
	if len(s.Conflicts) != 0 {
		t.Errorf("Conflicts = %+v, want none", s.Conflicts)
	}
}

func TestDeriveLetsTheProviderEventWinOverSECEvidenceWithAnotherRatio(t *testing.T) {
	var facts []domain.Fact
	y25 := splitYears(t, &facts, 0)
	y25.q2.add(sec.StockholdersEquityNoteStockSplitConversionRatio1, "", "2025-06-10", 2)
	split := providerSplit(t, "2025-06-10", 3)

	s := derive(t, facts, fundamentals.Options{Splits: []domain.Split{split}})
	if diff := cmp.Diff([]domain.Split{split}, s.Splits); diff != "" {
		t.Errorf("Splits (-want +got):\n%s", diff)
	}
	if v := value(t, s, "Q1 FY24", fundamentals.EPSDiluted); !near(v.V, 2.0/3, 1e-12) {
		t.Errorf("Q1 FY24 EPS = %v, want 2/3: the 3-for-1 provider split only", v.V)
	}
	if n := countConflicts(s, fundamentals.ConflictSplitEvidence); n != 1 {
		t.Errorf("%d split_evidence conflicts, want 1: %+v", n, s.Conflicts)
	}
}

func TestDeriveFlagsAValueThatAFilingAfterTheSplitRepeatsUnchanged(t *testing.T) {
	var facts []domain.Fact
	splitYears(t, &facts, 2.10)

	s := derive(t, facts, fundamentals.Options{Splits: []domain.Split{providerSplit(t, "2025-06-10", 2)}})
	want := []fundamentals.Conflict{{
		Kind:        fundamentals.ConflictSplitMismatch,
		Concept:     string(sec.EarningsPerShareDiluted),
		PeriodStart: dayPtr(t, "2024-04-01"),
		PeriodEnd:   day(t, "2024-06-30"),
		Observed:    1,
		Expected:    2,
	}}
	if diff := cmp.Diff(want, s.Conflicts, conflictSummary); diff != "" {
		t.Errorf("Conflicts (-want +got):\n%s", diff)
	}
	// The repeated value contradicts the split, so the first filing,
	// adjusted, stays in use.
	if v := value(t, s, "Q2 FY24", fundamentals.EPSDiluted); v.V != 1.05 || v.Inputs[0].Accession != "q2-2024" || v.Inputs[0].SplitRatio != 2 {
		t.Errorf("Q2 FY24 EPS = %v from %v, want 1.05 from q2-2024 divided by 2", v.V, accessions(v))
	}
}

func TestDeriveMatchesSECEvidenceToTheProviderEventWithItsRatio(t *testing.T) {
	// Two provider splits lie within the same-split window. The SEC
	// evidence is the second one, so it must not conflict with the first.
	var facts []domain.Fact
	y25 := splitYears(t, &facts, 0)
	y25.q2.add(sec.StockholdersEquityNoteStockSplitConversionRatio1, "", "2025-06-10", 2)
	splits := []domain.Split{providerSplit(t, "2025-03-03", 3), providerSplit(t, "2025-06-10", 2)}

	s := derive(t, facts, fundamentals.Options{Splits: splits})
	if diff := cmp.Diff(splits, s.Splits); diff != "" {
		t.Errorf("Splits (-want +got):\n%s", diff)
	}
	if len(s.Conflicts) != 0 {
		t.Errorf("Conflicts = %+v, want none", s.Conflicts)
	}
}

func TestDeriveIgnoresAnAnnouncedSECSplitDatedAfterAsOf(t *testing.T) {
	// A 10-Q filed on 2025-05-20 reports a split that takes effect on
	// 2025-06-10. As of 2025-06-01 the split has not happened yet.
	var facts []domain.Fact
	splitYears(t, &facts, 0)
	newFiling(t, &facts, "q1b-2025", "10-Q", "2025-05-20", 2025, "Q1").add(sec.StockholdersEquityNoteStockSplitConversionRatio1, "", "2025-06-10", 2)
	asOf := day(t, "2025-06-01")

	s := derive(t, facts, fundamentals.Options{AsOf: &asOf})
	if len(s.Splits) != 0 {
		t.Errorf("Splits = %+v, want none", s.Splits)
	}
	if v := value(t, s, "Q1 FY24", fundamentals.EPSDiluted); v.V != 2.00 {
		t.Errorf("Q1 FY24 EPS = %v, want 2.00 as filed", v.V)
	}
}

func TestDeriveLetsAnAmendedSplitRatioReplaceTheOriginal(t *testing.T) {
	// The 10-Q gives the split as 2-for-1. Its amendment corrects the same
	// period to 3-for-1: one split of 3, not two splits.
	var facts []domain.Fact
	y25 := splitYears(t, &facts, 0)
	y25.q2.add(sec.StockholdersEquityNoteStockSplitConversionRatio1, "", "2025-06-10", 2)
	newFiling(t, &facts, "q2a-2025", "10-Q/A", "2025-08-15", 2025, "Q2").add(sec.StockholdersEquityNoteStockSplitConversionRatio1, "", "2025-06-10", 3)

	s := derive(t, facts, fundamentals.Options{})
	want := []domain.Split{{CIK: testCIK, Date: day(t, "2025-06-10"), From: 1, To: 3, Source: "sec", Evidence: "q2a-2025"}}
	if diff := cmp.Diff(want, s.Splits); diff != "" {
		t.Errorf("Splits (-want +got):\n%s", diff)
	}
	if v := value(t, s, "Q1 FY24", fundamentals.EPSDiluted); !near(v.V, 2.0/3, 1e-12) {
		t.Errorf("Q1 FY24 EPS = %v, want 2/3", v.V)
	}
}

func TestDeriveFlagsSECSplitEvidenceWhenTheProviderHasNoSplits(t *testing.T) {
	var facts []domain.Fact
	y25 := splitYears(t, &facts, 1.05)
	y25.q2.add(sec.StockholdersEquityNoteStockSplitConversionRatio1, "", "2025-06-10", 2)

	s := derive(t, facts, fundamentals.Options{ProviderChecked: true})
	if len(s.Splits) != 1 || countConflicts(s, fundamentals.ConflictSplitEvidence) != 1 {
		t.Errorf("Splits = %+v, Conflicts = %+v; want the SEC split and one split_evidence conflict", s.Splits, s.Conflicts)
	}
}

func TestDeriveCitesTheFirstFilingOfTheSplitRatio(t *testing.T) {
	var facts []domain.Fact
	y25 := splitYears(t, &facts, 1.05)
	y25.q2.add(sec.StockholdersEquityNoteStockSplitConversionRatio1, "", "2025-06-10", 2)
	y25.q3.add(sec.StockholdersEquityNoteStockSplitConversionRatio1, "", "2025-06-10", 2)

	if s := derive(t, facts, fundamentals.Options{}); len(s.Splits) != 1 || s.Splits[0].Evidence != "q2-2025" {
		t.Errorf("Splits = %+v, want one with evidence q2-2025", s.Splits)
	}
}

func TestDeriveTakesBothInputsOfADifferenceOnOneSplitBasis(t *testing.T) {
	// FY 2024 EPS was 4.11 before a 2-for-1 split. The 10-K after the split
	// restates it as 2.06, rounded from 2.055. The 9M value of 3.00 was
	// never restated. Q4 is (4.11 − 3.00) / 2 = 0.555, not 2.06 − 1.50.
	var facts []domain.Fact
	y24 := newCalYear(t, &facts, 2024)
	y24.q3.add(sec.EarningsPerShareDiluted, "2024-01-01", "2024-09-30", 3.00)
	y24.k.add(sec.EarningsPerShareDiluted, "2024-01-01", "2024-12-31", 4.11)
	newCalYear(t, &facts, 2025).k.add(sec.EarningsPerShareDiluted, "2024-01-01", "2024-12-31", 2.06)

	v := value(t, derive(t, facts, fundamentals.Options{Splits: []domain.Split{providerSplit(t, "2025-06-10", 2)}}), "Q4 FY24", fundamentals.EPSDiluted)
	if v.V != 0.555 || !cmp.Equal(accessions(v), []string{"k-2024", "q3-2024"}) {
		t.Errorf("Q4 FY24 EPS = %v from %v, want 0.555 from k-2024 and q3-2024", v.V, accessions(v))
	}
}

// windowSplit gives a 2-for-1 split that the SEC ratio fact dates only to
// the quarter from 2025-04-01 to 2025-06-30. It took effect on 2025-04-10,
// so the Q1 10-Q, filed 2025-04-25, already restates Q1 2024 from 2.00 to
// 1.00 and reports Q1 2025 as 1.10 on the new basis.
func windowSplit(t *testing.T, facts *[]domain.Fact) {
	t.Helper()
	newCalYear(t, facts, 2024).q1.add(sec.EarningsPerShareDiluted, "2024-01-01", "2024-03-31", 2.00)
	y25 := newCalYear(t, facts, 2025)
	y25.q1.add(sec.EarningsPerShareDiluted, "2025-01-01", "2025-03-31", 1.10)
	y25.q1.add(sec.EarningsPerShareDiluted, "2024-01-01", "2024-03-31", 1.00)
	y25.q2.add(sec.StockholdersEquityNoteStockSplitConversionRatio1, "2025-04-01", "2025-06-30", 2)
}

func TestDeriveDatesAnSECSplitByTheRestatementsInItsWindow(t *testing.T) {
	var facts []domain.Fact
	windowSplit(t, &facts)

	s := derive(t, facts, fundamentals.Options{})
	if len(s.Splits) != 1 || s.Splits[0].Date.String() != "2025-04-01" {
		t.Errorf("Splits = %+v, want one dated 2025-04-01", s.Splits)
	}
	if v := value(t, s, "Q1 FY25", fundamentals.EPSDiluted); v.V != 1.10 || v.Inputs[0].SplitRatio != 1 {
		t.Errorf("Q1 FY25 EPS = %v with ratio %v, want 1.10 as filed", v.V, v.Inputs[0].SplitRatio)
	}
	if len(s.Conflicts) != 0 {
		t.Errorf("Conflicts = %+v, want none", s.Conflicts)
	}
}

func TestDeriveFlagsAnSECSplitDateThatAFilingLeavesOpen(t *testing.T) {
	// A 10-Q/A filed inside the window reports only a new period, so its
	// basis is unknown.
	var facts []domain.Fact
	windowSplit(t, &facts)
	newFiling(t, &facts, "q1a-2025", "10-Q/A", "2025-05-15", 2025, "Q1").add(sec.EarningsPerShareDiluted, "2025-01-01", "2025-03-31", 1.10)

	s := derive(t, facts, fundamentals.Options{})
	if n := countConflicts(s, fundamentals.ConflictSplitDate); n != 1 {
		t.Errorf("%d split_date conflicts, want 1: %+v", n, s.Conflicts)
	}
}

// scaleYear gives a calendar year with net income, EPS and diluted share
// counts per quarter. q1Shares is the Q1 count as filed.
func scaleYear(t *testing.T, facts *[]domain.Fact, q1Shares float64, withEPS bool) {
	t.Helper()
	y := newCalYear(t, facts, 2025)
	add := func(f *filing, start, end string, shares, ni, eps float64) {
		f.add(sec.WeightedAverageNumberOfDilutedSharesOutstanding, start, end, shares)
		if withEPS {
			f.add(sec.NetIncomeLoss, start, end, ni)
			f.add(sec.EarningsPerShareDiluted, start, end, eps)
		}
	}
	add(y.q1, "2025-01-01", "2025-03-31", q1Shares, 1_000_000_000, 0.23)
	add(y.q2, "2025-04-01", "2025-06-30", 4_300_000_000, 1_100_000_000, 0.26)
	add(y.q3, "2025-07-01", "2025-09-30", 4_290_000_000, 1_200_000_000, 0.28)
	y.k.add(sec.WeightedAverageNumberOfDilutedSharesOutstanding, "2025-01-01", "2025-12-31", 4_300_000_000)
}

func TestDeriveRescalesAShareCountTaggedInThousands(t *testing.T) {
	tests := []struct {
		name    string
		withEPS bool
	}{
		{"against net income over EPS", true},
		{"against the neighboring quarters", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var facts []domain.Fact
			scaleYear(t, &facts, 4_306_000, tt.withEPS)

			s := derive(t, facts, fundamentals.Options{})
			v := value(t, s, "Q1 FY25", fundamentals.DilutedShares)
			if v.V != 4_306_000_000 || v.Inputs[0].Value != 4_306_000 || v.Inputs[0].Scale != 1000 {
				t.Errorf("Q1 FY25 shares = %v from %+v, want 4306000000 scaled by 1000", v.V, v.Inputs[0])
			}
			want := []fundamentals.Conflict{{
				Kind:        fundamentals.ConflictShareScale,
				Concept:     string(sec.WeightedAverageNumberOfDilutedSharesOutstanding),
				PeriodStart: dayPtr(t, "2025-01-01"),
				PeriodEnd:   day(t, "2025-03-31"),
				Observed:    1000,
				Expected:    1,
			}}
			if diff := cmp.Diff(want, s.Conflicts, conflictSummary); diff != "" {
				t.Errorf("Conflicts (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDeriveKeepsASmallShareCountThatTheOtherValuesConfirm(t *testing.T) {
	// A company with about 1 million shares: net income over EPS agrees.
	var facts []domain.Fact
	y := newCalYear(t, &facts, 2025)
	y.q1.add(sec.WeightedAverageNumberOfDilutedSharesOutstanding, "2025-01-01", "2025-03-31", 1_010_000)
	y.q1.add(sec.NetIncomeLoss, "2025-01-01", "2025-03-31", 50_500_000)
	y.q1.add(sec.EarningsPerShareDiluted, "2025-01-01", "2025-03-31", 50.00)

	s := derive(t, facts, fundamentals.Options{})
	if v := value(t, s, "Q1 FY25", fundamentals.DilutedShares); v.V != 1_010_000 || len(s.Conflicts) != 0 {
		t.Errorf("Q1 FY25 shares = %v, conflicts %+v; want 1010000 and none", v.V, s.Conflicts)
	}
}

func TestDeriveRejectsAQ4ShareEstimateFromInputsOfAnotherScale(t *testing.T) {
	// The 10-K tags the full year in thousands, and no other value of the
	// year shows the scale of that one figure: the estimate would be
	// negative or absurd, so Q4 has no count, and a conflict says why.
	var facts []domain.Fact
	y := newCalYear(t, &facts, 2025)
	y.q1.add(sec.WeightedAverageNumberOfDilutedSharesOutstanding, "2025-01-01", "2025-03-31", 4_306_000_000)
	y.q2.add(sec.WeightedAverageNumberOfDilutedSharesOutstanding, "2025-04-01", "2025-06-30", 4_300_000_000)
	y.q3.add(sec.WeightedAverageNumberOfDilutedSharesOutstanding, "2025-07-01", "2025-09-30", 4_290_000_000)
	y.k.add(sec.WeightedAverageNumberOfDilutedSharesOutstanding, "2025-01-01", "2025-12-31", 4_301_000)

	s := derive(t, facts, fundamentals.Options{})
	if v, ok := quarter(t, s, "Q4 FY25").Values[fundamentals.DilutedShares]; ok && v.Method == fundamentals.MethodWeightedAverageQ4 {
		if v.V < 2e9 || v.V > 8e9 {
			t.Errorf("Q4 FY25 shares = %v, want none or about 4.3 billion", v.V)
		}
	}
	if countConflicts(s, fundamentals.ConflictShareScale) == 0 {
		t.Errorf("Conflicts = %+v, want a share_scale conflict", s.Conflicts)
	}
}

func TestDeriveKeepsACorrectionThatCameWithTheSplitRestatement(t *testing.T) {
	// FY 2024 EPS was 4.00 before a 2-for-1 split. The 10-K after the split
	// gives 2.50, which is more than the split: a correction. It stays,
	// so Q4 is 2.50 − 3.00 / 2 = 1.00.
	var facts []domain.Fact
	y24 := newCalYear(t, &facts, 2024)
	y24.q3.add(sec.EarningsPerShareDiluted, "2024-01-01", "2024-09-30", 3.00)
	y24.k.add(sec.EarningsPerShareDiluted, "2024-01-01", "2024-12-31", 4.00)
	newCalYear(t, &facts, 2025).k.add(sec.EarningsPerShareDiluted, "2024-01-01", "2024-12-31", 2.50)

	v := value(t, derive(t, facts, fundamentals.Options{Splits: []domain.Split{providerSplit(t, "2025-06-10", 2)}}), "Q4 FY24", fundamentals.EPSDiluted)
	if v.V != 1.00 || !cmp.Equal(accessions(v), []string{"k-2025", "q3-2024"}) {
		t.Errorf("Q4 FY24 EPS = %v from %v, want 1.00 from k-2025 and q3-2024", v.V, accessions(v))
	}
}

func TestDeriveLeavesOutSplitsAfterSplitsAsOfButKeepsLaterFacts(t *testing.T) {
	// A 10-Q filed on 2025-05-20 reports a split that takes effect on
	// 2025-06-10; the prices end on 2025-06-01.
	var facts []domain.Fact
	splitYears(t, &facts, 0)
	newFiling(t, &facts, "q1b-2025", "10-Q", "2025-05-20", 2025, "Q1").add(sec.StockholdersEquityNoteStockSplitConversionRatio1, "", "2025-06-10", 2)
	cutoff := day(t, "2025-06-01")

	s := derive(t, facts, fundamentals.Options{SplitsAsOf: &cutoff})
	if len(s.Splits) != 0 || len(s.Conflicts) != 0 {
		t.Errorf("Splits = %+v, Conflicts = %+v; want none", s.Splits, s.Conflicts)
	}
	if v := value(t, s, "Q1 FY24", fundamentals.EPSDiluted); v.V != 2.00 {
		t.Errorf("Q1 FY24 EPS = %v, want 2.00 as filed", v.V)
	}
	// Facts filed after the cutoff still count.
	if v := value(t, s, "Q2 FY25", fundamentals.EPSDiluted); v.V != 1.15 {
		t.Errorf("Q2 FY25 EPS = %v, want 1.15", v.V)
	}
}

func TestDeriveLeavesOutSECEvidenceOfAProviderSplitAfterSplitsAsOf(t *testing.T) {
	// The filing dates the split a week before the provider's first
	// trading day on the new basis; the cutoff lies between them.
	var facts []domain.Fact
	splitYears(t, &facts, 0)
	newFiling(t, &facts, "q1b-2025", "10-Q", "2025-05-20", 2025, "Q1").add(sec.StockholdersEquityNoteStockSplitConversionRatio1, "", "2025-06-03", 2)
	cutoff := day(t, "2025-06-05")

	s := derive(t, facts, fundamentals.Options{Splits: []domain.Split{providerSplit(t, "2025-06-10", 2)}, SplitsAsOf: &cutoff})
	if len(s.Splits) != 0 {
		t.Errorf("Splits = %+v, want none before the provider's date", s.Splits)
	}
	if v := value(t, s, "Q1 FY24", fundamentals.EPSDiluted); v.V != 2.00 {
		t.Errorf("Q1 FY24 EPS = %v, want 2.00 as filed", v.V)
	}
}
