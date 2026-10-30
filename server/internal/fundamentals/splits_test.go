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
