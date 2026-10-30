package fundamentals_test

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/fundamentals"
	"github.com/sourcehawk/pricedin/server/internal/sec"
)

const (
	cikNVDA domain.CIK = 1045810
	cikAAPL domain.CIK = 320193
	cikKO   domain.CIK = 21344
	cikGRMN domain.CIK = 1121788
	cikADM  domain.CIK = 7084
)

func fixtureFacts(t *testing.T, cik domain.CIK) []domain.Fact {
	t.Helper()
	path := filepath.FromSlash("../../../testdata/sec/data.sec.gov/api/xbrl/companyfacts/CIK" + cik.String() + ".json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	facts, err := sec.ParseCompanyFacts(body)
	if err != nil {
		t.Fatalf("ParseCompanyFacts() error = %v", err)
	}
	return facts
}

func deriveFixture(t *testing.T, cik domain.CIK, opts fundamentals.Options) fundamentals.Series {
	t.Helper()
	s, err := fundamentals.Derive(cik, fixtureFacts(t, cik), opts)
	if err != nil {
		t.Fatalf("Derive() error = %v", err)
	}
	return s
}

// The splits as a price provider dates them: the first trading day on the
// new basis.
func nvdaSplits(t *testing.T) []domain.Split {
	return []domain.Split{
		{Symbol: "NVDA", Date: day(t, "2021-07-20"), From: 1, To: 4, Source: "massive"},
		{Symbol: "NVDA", Date: day(t, "2024-06-10"), From: 1, To: 10, Source: "massive"},
	}
}

func aaplSplits(t *testing.T) []domain.Split {
	return []domain.Split{
		{Symbol: "AAPL", Date: day(t, "2014-06-09"), From: 1, To: 7, Source: "massive"},
		{Symbol: "AAPL", Date: day(t, "2020-08-31"), From: 1, To: 4, Source: "massive"},
	}
}

func koSplits(t *testing.T) []domain.Split {
	return []domain.Split{{Symbol: "KO", Date: day(t, "2012-08-13"), From: 1, To: 2, Source: "massive"}}
}

// filingRow is one quarter as the company's own filings give it. A source
// names the filing of each input as "<form> <accession>". A reported value
// has one source. A Q4 value is the 10-K's full year minus the 10-Q's nine
// months ("10-K … − 10-Q …"), and the comment after the row gives the two
// numbers. For each period the latest filing counts, which is often the
// comparative column of the next year's filing.
type filingRow struct {
	label      string
	end        string
	eps        float64
	epsFrom    string
	revenue    float64
	revenueFrm string
}

func TestDeriveMatchesTheFilingsOfEachFixtureCompany(t *testing.T) {
	tests := []struct {
		name           string
		cik            domain.CIK
		splits         []domain.Split
		revenueConcept sec.Concept
		rows           []filingRow
	}{
		{"NVDA", cikNVDA, nvdaSplits(t), sec.Revenues, []filingRow{
			{"Q1 FY25", "2024-04-28", 0.60, "10-Q 0001045810-25-000116", 26044000000, "10-Q 0001045810-25-000116"},
			{"Q2 FY25", "2024-07-28", 0.67, "10-Q 0001045810-25-000209", 30040000000, "10-Q 0001045810-25-000209"},
			{"Q3 FY25", "2024-10-27", 0.78, "10-Q 0001045810-25-000230", 35082000000, "10-Q 0001045810-25-000230"},
			// EPS 2.94 − 2.04; revenue 130,497 − 91,166 million.
			{"Q4 FY25", "2025-01-26", 0.90, "10-K 0001045810-26-000021 − 10-Q 0001045810-25-000230", 39331000000, "10-K 0001045810-26-000021 − 10-Q 0001045810-25-000230"},
			{"Q1 FY26", "2025-04-27", 0.76, "10-Q 0001045810-26-000052", 44062000000, "10-Q 0001045810-26-000052"},
			{"Q2 FY26", "2025-07-27", 1.08, "10-Q 0001045810-26-000075", 46743000000, "10-Q 0001045810-26-000075"},
			{"Q3 FY26", "2025-10-26", 1.30, "10-Q 0001045810-25-000230", 57006000000, "10-Q 0001045810-25-000230"},
			// EPS 4.90 − 3.14; revenue 215,938 − 147,811 million.
			{"Q4 FY26", "2026-01-25", 1.76, "10-K 0001045810-26-000021 − 10-Q 0001045810-25-000230", 68127000000, "10-K 0001045810-26-000021 − 10-Q 0001045810-25-000230"},
			{"Q1 FY27", "2026-04-26", 2.39, "10-Q 0001045810-26-000052", 81615000000, "10-Q 0001045810-26-000052"},
			{"Q2 FY27", "2026-07-26", 2.46, "10-Q 0001045810-26-000075", 96221000000, "10-Q 0001045810-26-000075"},
		}},
		{"AAPL", cikAAPL, aaplSplits(t), sec.RevenueFromContractWithCustomerExcludingAssessedTax, []filingRow{
			{"Q2 FY24", "2024-03-30", 1.53, "10-Q 0000320193-25-000057", 90753000000, "10-Q 0000320193-25-000057"},
			{"Q3 FY24", "2024-06-29", 1.40, "10-Q 0000320193-25-000073", 85777000000, "10-Q 0000320193-25-000073"},
			// EPS 6.08 − 5.11; revenue 391,035 − 296,105 million.
			{"Q4 FY24", "2024-09-28", 0.97, "10-K 0000320193-25-000079 − 10-Q 0000320193-25-000073", 94930000000, "10-K 0000320193-25-000079 − 10-Q 0000320193-25-000073"},
			{"Q1 FY25", "2024-12-28", 2.40, "10-Q 0000320193-26-000006", 124300000000, "10-Q 0000320193-26-000006"},
			{"Q2 FY25", "2025-03-29", 1.65, "10-Q 0000320193-26-000013", 95359000000, "10-Q 0000320193-26-000013"},
			{"Q3 FY25", "2025-06-28", 1.57, "10-Q 0000320193-26-000020", 94036000000, "10-Q 0000320193-26-000020"},
			// EPS 7.46 − 5.62; revenue 416,161 − 313,695 million.
			{"Q4 FY25", "2025-09-27", 1.84, "10-K 0000320193-25-000079 − 10-Q 0000320193-26-000020", 102466000000, "10-K 0000320193-25-000079 − 10-Q 0000320193-26-000020"},
			{"Q1 FY26", "2025-12-27", 2.84, "10-Q 0000320193-26-000006", 143756000000, "10-Q 0000320193-26-000006"},
			{"Q2 FY26", "2026-03-28", 2.01, "10-Q 0000320193-26-000013", 111184000000, "10-Q 0000320193-26-000013"},
			{"Q3 FY26", "2026-06-27", 2.02, "10-Q 0000320193-26-000020", 109417000000, "10-Q 0000320193-26-000020"},
		}},
		{"KO", cikKO, koSplits(t), sec.Revenues, []filingRow{
			{"Q2 FY24", "2024-06-28", 0.56, "10-Q 0000021344-25-000061", 12363000000, "10-Q 0000021344-25-000061"},
			{"Q3 FY24", "2024-09-27", 0.66, "10-Q 0001628280-25-046054", 11854000000, "10-Q 0001628280-25-046054"},
			// EPS 2.46 − 1.95; revenue 47,061 − 35,517 million.
			{"Q4 FY24", "2024-12-31", 0.51, "10-K 0001628280-26-010047 − 10-Q 0001628280-25-046054", 11544000000, "10-K 0001628280-26-010047 − 10-Q 0001628280-25-046054"},
			{"Q1 FY25", "2025-03-28", 0.77, "10-Q 0001628280-26-028802", 11129000000, "10-Q 0001628280-26-028802"},
			{"Q2 FY25", "2025-06-27", 0.88, "10-Q 0001628280-26-050503", 12535000000, "10-Q 0001628280-26-050503"},
			{"Q3 FY25", "2025-09-26", 0.86, "10-Q 0001628280-26-067844", 12455000000, "10-Q 0001628280-26-067844"},
			// EPS 3.04 − 2.51; revenue 47,941 − 36,119 million.
			{"Q4 FY25", "2025-12-31", 0.53, "10-K 0001628280-26-010047 − 10-Q 0001628280-26-067844", 11822000000, "10-K 0001628280-26-010047 − 10-Q 0001628280-26-067844"},
			{"Q1 FY26", "2026-04-03", 0.91, "10-Q 0001628280-26-028802", 12472000000, "10-Q 0001628280-26-028802"},
			{"Q2 FY26", "2026-07-03", 1.03, "10-Q 0001628280-26-050503", 13380000000, "10-Q 0001628280-26-050503"},
			{"Q3 FY26", "2026-10-02", 0.93, "10-Q 0001628280-26-067844", 13380000000, "10-Q 0001628280-26-067844"},
		}},
		{"GRMN", cikGRMN, nil, sec.RevenueFromContractWithCustomerExcludingAssessedTax, []filingRow{
			{"Q2 FY24", "2024-06-29", 1.56, "10-Q 0000950170-25-099877", 1506671000, "10-Q 0000950170-25-099877"},
			{"Q3 FY24", "2024-09-28", 2.07, "10-Q 0001193125-25-254605", 1586022000, "10-Q 0001193125-25-254605"},
			// EPS 7.30 − 5.06; revenue 6,296,903 − 4,474,342 thousand.
			{"Q4 FY24", "2024-12-28", 2.24, "10-K 0001193125-26-056028 − 10-Q 0001193125-25-254605", 1822561000, "10-K 0001193125-26-056028 − 10-Q 0001193125-25-254605"},
			{"Q1 FY25", "2025-03-29", 1.72, "10-Q 0001193125-26-188908", 1535099000, "10-Q 0001193125-26-188908"},
			{"Q2 FY25", "2025-06-28", 2.07, "10-Q 0001193125-26-322114", 1814564000, "10-Q 0001193125-26-322114"},
			{"Q3 FY25", "2025-09-27", 2.08, "10-Q 0001193125-26-435070", 1770901000, "10-Q 0001193125-26-435070"},
			// EPS 8.59 − 5.87; revenue 7,245,519 − 5,120,564 thousand.
			{"Q4 FY25", "2025-12-27", 2.72, "10-K 0001193125-26-056028 − 10-Q 0001193125-26-435070", 2124955000, "10-K 0001193125-26-056028 − 10-Q 0001193125-26-435070"},
			{"Q1 FY26", "2026-03-28", 2.09, "10-Q 0001193125-26-188908", 1753489000, "10-Q 0001193125-26-188908"},
			{"Q2 FY26", "2026-06-27", 2.80, "10-Q 0001193125-26-322114", 2022092000, "10-Q 0001193125-26-322114"},
			{"Q3 FY26", "2026-09-26", 2.80, "10-Q 0001193125-26-435070", 1986076000, "10-Q 0001193125-26-435070"},
		}},
		// ADM tags only a part of its revenue as contract revenue, so the
		// total comes from Revenues (see the concept priority).
		{"ADM", cikADM, nil, sec.Revenues, []filingRow{
			{"Q1 FY24", "2024-03-31", 1.42, "10-Q 0000007084-25-000022", 21847000000, "10-Q 0000007084-25-000022"},
			{"Q2 FY24", "2024-06-30", 0.98, "10-Q 0000007084-25-000042", 22248000000, "10-Q 0000007084-25-000042"},
			{"Q3 FY24", "2024-09-30", 0.04, "10-Q 0000007084-25-000052", 19937000000, "10-Q 0000007084-25-000052"},
			// EPS 3.65 − 2.48; revenue 85,530 − 64,032 million.
			{"Q4 FY24", "2024-12-31", 1.17, "10-K 0000007084-26-000011 − 10-Q 0000007084-25-000052", 21498000000, "10-K 0000007084-26-000011 − 10-Q 0000007084-25-000052"},
			{"Q1 FY25", "2025-03-31", 0.61, "10-Q 0000007084-26-000023", 20175000000, "10-Q 0000007084-26-000023"},
			{"Q2 FY25", "2025-06-30", 0.45, "10-Q 0000007084-26-000042", 21166000000, "10-Q 0000007084-26-000042"},
			{"Q3 FY25", "2025-09-30", 0.22, "10-Q 0000007084-25-000052", 20372000000, "10-Q 0000007084-25-000052"},
			// EPS 2.23 − 1.29; revenue 80,269 − 61,713 million.
			{"Q4 FY25", "2025-12-31", 0.94, "10-K 0000007084-26-000011 − 10-Q 0000007084-25-000052", 18556000000, "10-K 0000007084-26-000011 − 10-Q 0000007084-25-000052"},
			{"Q1 FY26", "2026-03-31", 0.62, "10-Q 0000007084-26-000023", 20490000000, "10-Q 0000007084-26-000023"},
			{"Q2 FY26", "2026-06-30", 1.87, "10-Q 0000007084-26-000042", 22681000000, "10-Q 0000007084-26-000042"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := deriveFixture(t, tt.cik, fundamentals.Options{Splits: tt.splits})
			n := len(s.Quarters)
			if n < len(tt.rows) {
				t.Fatalf("have %d quarters, want at least %d", n, len(tt.rows))
			}
			for i, row := range tt.rows {
				q := s.Quarters[n-len(tt.rows)+i]
				if q.Label != row.label || q.PeriodEnd.String() != row.end {
					t.Errorf("quarter %d = %s ending %s, want %s ending %s", i, q.Label, q.PeriodEnd, row.label, row.end)
					continue
				}
				checkFiled(t, q, fundamentals.EPSDiluted, row.eps, row.epsFrom, sec.EarningsPerShareDiluted)
				checkFiled(t, q, fundamentals.Revenue, row.revenue, row.revenueFrm, tt.revenueConcept)
			}
		})
	}
}

// checkFiled checks one value of q against its filing row.
func checkFiled(t *testing.T, q fundamentals.Quarter, m fundamentals.Metric, want float64, from string, concept sec.Concept) {
	t.Helper()
	v, ok := q.Values[m]
	if !ok {
		t.Errorf("%s has no %s", q.Label, m)
		return
	}
	if v.V != want {
		t.Errorf("%s %s = %v, want %v", q.Label, m, v.V, want)
	}
	sources := strings.Split(from, " − ")
	method := fundamentals.MethodReported
	if len(sources) == 2 {
		method = fundamentals.MethodFYDifference
	}
	if v.Method != method {
		t.Errorf("%s %s method = %s, want %s", q.Label, m, v.Method, method)
	}
	got := make([]string, 0, len(v.Inputs))
	for _, in := range v.Inputs {
		got = append(got, in.Form+" "+in.Accession)
		if in.Concept != string(concept) {
			t.Errorf("%s %s input concept = %s, want %s", q.Label, m, in.Concept, concept)
		}
	}
	if strings.Join(got, " − ") != from {
		t.Errorf("%s %s sources = %q, want %q", q.Label, m, strings.Join(got, " − "), from)
	}
}

// nvdaPreSplit lists the quarters that ended before NVIDIA's 10-for-1 split
// on 2024-06-10. Each was first filed before the split; the filings after
// it restated all of them except Q1 FY24.
var nvdaPreSplit = []struct {
	label string
	// Before the restatements (as of 2024-07-01): the first filing, divided
	// by 10.
	original     float64
	originalFrom string
	// After them: the restated value as filed, or the first filing divided
	// by 10 where no filing restated the quarter.
	restated     float64
	restatedFrom string
}{
	// 0.82 / 10; no later filing gives this quarter.
	{"Q1 FY24", 0.082, "10-Q 0001045810-24-000124", 0.082, "10-Q 0001045810-24-000124"},
	// 2.48 / 10 and the restated 0.25.
	{"Q2 FY24", 0.248, "10-Q 0001045810-23-000175", 0.25, "10-Q 0001045810-24-000264"},
	// 3.71 / 10 and the restated 0.37.
	{"Q3 FY24", 0.371, "10-Q 0001045810-23-000227", 0.37, "10-Q 0001045810-24-000316"},
	// (11.93 − 7.01) / 10 and the restated 1.19 − 0.70.
	{"Q4 FY24", 0.492, "10-K 0001045810-24-000029 − 10-Q 0001045810-23-000227", 0.49, "10-K 0001045810-26-000021 − 10-Q 0001045810-24-000316"},
	// 5.98 / 10 and the restated 0.60.
	{"Q1 FY25", 0.598, "10-Q 0001045810-24-000124", 0.60, "10-Q 0001045810-25-000116"},
}

func TestDeriveDividesNVIDIAEPSBeforeTheSplitByTen(t *testing.T) {
	asOf := day(t, "2024-07-01")
	before := deriveFixture(t, cikNVDA, fundamentals.Options{AsOf: &asOf, Splits: nvdaSplits(t)})
	after := deriveFixture(t, cikNVDA, fundamentals.Options{Splits: nvdaSplits(t)})

	for _, row := range nvdaPreSplit {
		checkFiled(t, quarter(t, before, row.label), fundamentals.EPSDiluted, row.original, row.originalFrom, sec.EarningsPerShareDiluted)
		checkFiled(t, quarter(t, after, row.label), fundamentals.EPSDiluted, row.restated, row.restatedFrom, sec.EarningsPerShareDiluted)
		// The adjusted first filing matches the restatement to the cent.
		if !near(row.original, row.restated, 0.005) {
			t.Errorf("%s: adjusted %v and restated %v differ by more than a half cent", row.label, row.original, row.restated)
		}
	}
	for _, in := range value(t, before, "Q2 FY24", fundamentals.EPSDiluted).Inputs {
		if in.Value != 2.48 || in.SplitRatio != 10 {
			t.Errorf("Q2 FY24 input = %v with split ratio %v, want 2.48 with 10", in.Value, in.SplitRatio)
		}
	}
}

func TestDeriveConfirmsNVIDIASplitsFromItsFilingsAlone(t *testing.T) {
	withProvider := deriveFixture(t, cikNVDA, fundamentals.Options{Splits: nvdaSplits(t)})
	secOnly := deriveFixture(t, cikNVDA, fundamentals.Options{})

	dates := make([]string, 0, len(secOnly.Splits))
	for _, s := range secOnly.Splits {
		if s.Source != "sec" {
			t.Errorf("split %+v, want source sec", s)
		}
		dates = append(dates, s.Date.String()+" ×"+strconv.FormatFloat(s.Ratio(), 'f', -1, 64))
	}
	if want := "2021-07-19 ×4, 2024-06-30 ×10"; strings.Join(dates, ", ") != want {
		t.Errorf("SEC splits = %s, want %s", strings.Join(dates, ", "), want)
	}
	if len(withProvider.Quarters) != len(secOnly.Quarters) {
		t.Fatalf("quarters: %d with provider splits, %d without", len(withProvider.Quarters), len(secOnly.Quarters))
	}
	for i, q := range withProvider.Quarters {
		for _, m := range []fundamentals.Metric{fundamentals.EPSDiluted, fundamentals.DividendsPerShare, fundamentals.DilutedShares} {
			if a, b := q.Values[m], secOnly.Quarters[i].Values[m]; a.V != b.V {
				t.Errorf("%s %s = %v with provider splits, %v without", q.Label, m, a.V, b.V)
			}
		}
	}
}

func TestDeriveFollowsApplesFiscalCalendar(t *testing.T) {
	// Apple's fiscal year ends on the last Saturday of September. Fiscal
	// 2023 had 53 weeks, with a 14-week first quarter.
	s := deriveFixture(t, cikAAPL, fundamentals.Options{Splits: aaplSplits(t)})
	want := []period{
		{"Q1 FY23", "2022-09-25", "2022-12-31"},
		{"Q2 FY23", "2023-01-01", "2023-04-01"},
		{"Q3 FY23", "2023-04-02", "2023-07-01"},
		{"Q4 FY23", "2023-07-02", "2023-09-30"},
		{"Q1 FY24", "2023-10-01", "2023-12-30"},
	}
	for _, w := range want {
		q := quarter(t, s, w.Label)
		if got := (period{q.Label, q.PeriodStart.String(), q.PeriodEnd.String()}); got != w {
			t.Errorf("quarter = %+v, want %+v", got, w)
		}
	}
	if q := s.Quarters[len(s.Quarters)-1]; q.Label != "Q3 FY26" || q.FiscalYear != 2026 || q.FiscalQuarter != 3 {
		t.Errorf("last quarter = %s (FY %d Q%d), want Q3 FY26", q.Label, q.FiscalYear, q.FiscalQuarter)
	}
}

func TestDeriveGivesCocaColaDividendsPerShare(t *testing.T) {
	s := deriveFixture(t, cikKO, fundamentals.Options{Splits: koSplits(t)})
	tests := []struct {
		label  string
		want   float64
		method fundamentals.Method
		from   string
	}{
		{"Q3 FY25", 0.51, fundamentals.MethodReported, "0001628280-26-067844"},
		// 2.04 for the year − 1.53 for nine months.
		{"Q4 FY25", 0.51, fundamentals.MethodFYDifference, "0001628280-26-010047 0001628280-26-067844"},
		{"Q1 FY26", 0.53, fundamentals.MethodReported, "0001628280-26-028802"},
		// $0.47 before the 2-for-1 split of August 2012, filed before it and
		// never restated: divided by 2.
		{"Q1 FY11", 0.235, fundamentals.MethodReported, "0000021344-12-000023"},
		// $0.51 before the split, restated as $0.255 by the 10-Q of 2013.
		{"Q2 FY12", 0.255, fundamentals.MethodReported, "0000021344-13-000039"},
	}
	for _, tt := range tests {
		v := value(t, s, tt.label, fundamentals.DividendsPerShare)
		if v.V != tt.want || v.Method != tt.method || strings.Join(accessions(v), " ") != tt.from {
			t.Errorf("%s dividends = %v %s from %v, want %v %s from %s", tt.label, v.V, v.Method, accessions(v), tt.want, tt.method, tt.from)
		}
	}
	at := len(s.Quarters) - 1
	for s.Quarters[at].Label != "Q4 FY25" {
		at--
	}
	if v, ok := fundamentals.TTM(s, fundamentals.DividendsPerShare, at); !ok || v.V != 2.04 {
		t.Errorf("TTM dividends at Q4 FY25 = %v, %v; want 2.04", v.V, ok)
	}
}

func TestDeriveFlagsCocaColaSplitWithoutProviderEvents(t *testing.T) {
	// Coca-Cola's filings never tag the split ratio, so without the provider
	// event the 2012 split is not confirmed: the 2-for-1 restatements are
	// conflicts, and the 2011 dividend stays as filed.
	without := deriveFixture(t, cikKO, fundamentals.Options{})
	with := deriveFixture(t, cikKO, fundamentals.Options{Splits: koSplits(t)})

	if v := value(t, without, "Q1 FY11", fundamentals.DividendsPerShare); v.V != 0.47 {
		t.Errorf("Q1 FY11 dividends without splits = %v, want 0.47", v.V)
	}
	if n := countConflicts(without, fundamentals.ConflictUnconfirmedSplit); n == 0 {
		t.Error("no unconfirmed_split conflicts without the provider split")
	}
	for _, c := range without.Conflicts {
		// The ratio of two rounded values is close to 2, not exactly 2.
		if c.Kind == fundamentals.ConflictUnconfirmedSplit && !near(c.Observed, 2, 0.03) {
			t.Errorf("unconfirmed split %s %s observed %v, want about 2", c.Concept, c.PeriodEnd, c.Observed)
		}
	}
	if n := countConflicts(with, fundamentals.ConflictUnconfirmedSplit) + countConflicts(with, fundamentals.ConflictSplitMismatch); n != 0 {
		t.Errorf("%d split conflicts with the provider split, want none", n)
	}
}

func countConflicts(s fundamentals.Series, kind fundamentals.ConflictKind) int {
	n := 0
	for i := range s.Conflicts {
		if s.Conflicts[i].Kind == kind {
			n++
		}
	}
	return n
}

func TestDeriveUsesADMAmendmentsFromTheirFilingDate(t *testing.T) {
	// ADM amended its 2023 10-K and its 2024 10-Qs on 2024-11-18. The
	// amendments repeat EPS and revenue unchanged, so the value stays and
	// the source moves to the amendment; the next year's 10-Q then repeats
	// the quarter again.
	tests := []struct {
		label, asOf string
		from        string
	}{
		{"Q1 FY24", "2024-11-17", "10-Q 0000007084-24-000017"},
		{"Q1 FY24", "2024-11-18", "10-Q/A 0000007084-24-000052"},
		{"Q1 FY24", "2025-05-06", "10-Q 0000007084-25-000022"},
		// Q4 2023 is reported in the 10-K itself.
		{"Q4 FY23", "2024-11-17", "10-K 0000007084-24-000009"},
		{"Q4 FY23", "2024-11-18", "10-K/A 0000007084-24-000051"},
	}
	for _, tt := range tests {
		t.Run(tt.label+" "+tt.asOf, func(t *testing.T) {
			asOf := day(t, tt.asOf)
			s := deriveFixture(t, cikADM, fundamentals.Options{AsOf: &asOf})
			want := map[string]float64{"Q1 FY24": 1.42, "Q4 FY23": 1.06}[tt.label]
			checkFiled(t, quarter(t, s, tt.label), fundamentals.EPSDiluted, want, tt.from, sec.EarningsPerShareDiluted)
		})
	}
}

func TestDeriveStartsADMAfterItsChangeOfFiscalYear(t *testing.T) {
	// ADM's fiscal year ended in June until 2012, then in December. Its
	// 10-K for 2013 gives calendar 2012 as a comparative year.
	s := deriveFixture(t, cikADM, fundamentals.Options{})
	if q := s.Quarters[0]; q.Label != "Q1 FY12" || q.PeriodStart.String() != "2012-01-01" || q.PeriodEnd.String() != "2012-03-31" {
		t.Errorf("first quarter = %s %s..%s, want Q1 FY12 2012-01-01..2012-03-31", q.Label, q.PeriodStart, q.PeriodEnd)
	}
}

func TestDeriveEstimatesApplesQ4SharesFromTheFullYear(t *testing.T) {
	// Fiscal 2025 has four 91-day quarters and 364 days:
	// 15,004,697,000 × 364 / 91 − (15,150,865,000 + 15,056,133,000 +
	// 14,948,179,000) = 14,863,611,000.
	s := deriveFixture(t, cikAAPL, fundamentals.Options{Splits: aaplSplits(t)})
	v := value(t, s, "Q4 FY25", fundamentals.DilutedShares)
	want := "0000320193-25-000079 0000320193-26-000006 0000320193-26-000013 0000320193-26-000020"
	if v.V != 14863611000 || v.Method != fundamentals.MethodWeightedAverageQ4 || !v.Estimate || strings.Join(accessions(v), " ") != want {
		t.Errorf("Q4 FY25 shares = %v %s estimate=%v from %v, want 14863611000 weighted_average_q4 estimate from %s", v.V, v.Method, v.Estimate, accessions(v), want)
	}
}

func fixtureCompanies(t *testing.T) []struct {
	name   string
	cik    domain.CIK
	splits []domain.Split
} {
	return []struct {
		name   string
		cik    domain.CIK
		splits []domain.Split
	}{
		{"NVDA", cikNVDA, nvdaSplits(t)},
		{"AAPL", cikAAPL, aaplSplits(t)},
		{"KO", cikKO, koSplits(t)},
		{"GRMN", cikGRMN, nil},
		{"ADM", cikADM, nil},
	}
}

func TestDeriveAsOfShowsNoValueFromALaterFiling(t *testing.T) {
	asOf := day(t, "2025-06-30")
	for _, c := range fixtureCompanies(t) {
		t.Run(c.name, func(t *testing.T) {
			s := deriveFixture(t, c.cik, fundamentals.Options{AsOf: &asOf, Splits: c.splits})
			for _, q := range s.Quarters {
				for m, v := range q.Values {
					for _, in := range v.Inputs {
						if in.Filed.After(asOf) {
							t.Errorf("%s %s uses %s filed %s", q.Label, m, in.Accession, in.Filed)
						}
					}
				}
			}
			for _, x := range s.Conflicts {
				if x.Restated.Filed.After(asOf) {
					t.Errorf("conflict %s uses %s filed %s", x.Kind, x.Restated.Accession, x.Restated.Filed)
				}
			}
			for _, sp := range s.Splits {
				if sp.Date.After(asOf) {
					t.Errorf("split on %s, after %s", sp.Date, asOf)
				}
			}
			if last := s.Quarters[len(s.Quarters)-1]; !last.PeriodEnd.Before(asOf) {
				t.Errorf("last quarter %s ends %s", last.Label, last.PeriodEnd)
			}
		})
	}
}

func TestDeriveGivesEveryValueItsMethodAndSources(t *testing.T) {
	for _, c := range fixtureCompanies(t) {
		t.Run(c.name, func(t *testing.T) {
			s := deriveFixture(t, c.cik, fundamentals.Options{Splits: c.splits})
			for _, q := range s.Quarters {
				for m, v := range q.Values {
					if v.Method == "" || len(v.Inputs) == 0 {
						t.Fatalf("%s %s: method %q with %d inputs", q.Label, m, v.Method, len(v.Inputs))
					}
					var latest domain.Date
					for _, in := range v.Inputs {
						if in.Accession == "" || in.Concept == "" || in.Filed.IsZero() || in.SplitRatio == 0 {
							t.Fatalf("%s %s: incomplete input %+v", q.Label, m, in)
						}
						if !strings.HasPrefix(in.Form, "10-") || strings.HasPrefix(in.Form, "10-12") {
							t.Fatalf("%s %s: input from form %s", q.Label, m, in.Form)
						}
						if in.Filed.After(latest) {
							latest = in.Filed
						}
					}
					if !v.Filed.Equal(latest) || v.FirstFiled.After(v.Filed) {
						t.Fatalf("%s %s: Filed %s, FirstFiled %s, latest input %s", q.Label, m, v.Filed, v.FirstFiled, latest)
					}
				}
			}
		})
	}
}

func TestDeriveExplainsEveryFixtureRestatementWithTheProviderSplits(t *testing.T) {
	// With the provider's split events, the only conflicts left are share
	// counts that a filing tagged in thousands, as NVIDIA (2009 to 2011),
	// Coca-Cola (2017 to 2019) and Garmin (2011 to 2023) did.
	for _, c := range fixtureCompanies(t) {
		t.Run(c.name, func(t *testing.T) {
			s := deriveFixture(t, c.cik, fundamentals.Options{Splits: c.splits})
			for _, x := range s.Conflicts {
				if x.Kind != fundamentals.ConflictShareScale {
					t.Errorf("conflict %s %s %s: %v from %s to %v from %s", x.Kind, x.Concept, x.PeriodEnd, x.Original.Value, x.Original.Accession, x.Restated.Value, x.Restated.Accession)
				}
			}
		})
	}
}

func TestDeriveDatesADMQ4From10KOfThatYear(t *testing.T) {
	// The 10-K for 2022 (filed 2023-02-14) gives the year and so Q4 2022.
	// The quarter itself is reported only in the 10-K for 2023, filed
	// 2024-03-12. That value is used, but the quarter was known in 2023.
	s := deriveFixture(t, cikADM, fundamentals.Options{})
	v := value(t, s, "Q4 FY22", fundamentals.EPSDiluted)
	if v.V != 1.84 || v.Method != fundamentals.MethodReported || v.FirstFiled.String() != "2023-02-14" {
		t.Errorf("Q4 FY22 EPS = %v %s first filed %s, want 1.84 reported first filed 2023-02-14", v.V, v.Method, v.FirstFiled)
	}
}

func TestDeriveFindsCocaColasRestatementOfAShiftedPeriod(t *testing.T) {
	// The 10-Q filed 2011-08-01 gives Q2 2011 operating income of 3,174
	// million for 2011-04-02 to 2011-07-01. The 10-Q filed 2012-07-26
	// restates it as 3,179 million for 2011-04-03 to 2011-07-01.
	s := deriveFixture(t, cikKO, fundamentals.Options{Splits: koSplits(t)})
	v := value(t, s, "Q2 FY11", fundamentals.OperatingIncome)
	if v.V != 3179000000 || len(v.Inputs) != 1 || v.Inputs[0].Accession != "0000021344-12-000037" {
		t.Fatalf("Q2 FY11 operating income = %v from %v, want 3179000000 from 0000021344-12-000037", v.V, accessions(v))
	}
	if in := v.Inputs[0]; in.PeriodStart.String() != "2011-04-03" || v.FirstFiled.String() != "2011-08-01" {
		t.Errorf("input period starts %s, FirstFiled %s; want the filed start 2011-04-03 and 2011-08-01", in.PeriodStart, v.FirstFiled)
	}
}

func TestDeriveCitesTheFirstFilingOfEachSECSplit(t *testing.T) {
	tests := []struct {
		name string
		cik  domain.CIK
		want []string
	}{
		{"NVDA", cikNVDA, []string{"0001045810-21-000131", "0001045810-24-000264"}},
		{"AAPL", cikAAPL, []string{"0001193125-14-277160", "0000320193-20-000096"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, sp := range deriveFixture(t, tt.cik, fundamentals.Options{}).Splits {
				got = append(got, sp.Evidence)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("evidence = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeriveTakesNVIDIAQ4FY23OnThePreSplitBasis(t *testing.T) {
	// The 10-K of 2025 restates FY23 EPS as 0.17, rounded after the split.
	// The 9M value of 1.17 was never restated. NVIDIA reported $0.57 for
	// Q4 FY23 before the split: (1.74 − 1.17) / 10.
	s := deriveFixture(t, cikNVDA, fundamentals.Options{Splits: nvdaSplits(t)})
	checkFiled(t, quarter(t, s, "Q4 FY23"), fundamentals.EPSDiluted, 0.057,
		"10-K 0001045810-24-000029 − 10-Q 0001045810-23-000227", sec.EarningsPerShareDiluted)
}
