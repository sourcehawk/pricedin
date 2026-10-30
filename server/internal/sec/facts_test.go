package sec_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/sec"
)

func companyFactsFixture(t *testing.T, cik domain.CIK) []domain.Fact {
	t.Helper()
	facts, err := sec.ParseCompanyFacts(readFixture(t, "data.sec.gov/api/xbrl/companyfacts/CIK"+cik.String()+".json"))
	if err != nil {
		t.Fatalf("ParseCompanyFacts() error = %v", err)
	}
	return facts
}

// findFacts returns the facts of one concept that one filing reported.
func findFacts(facts []domain.Fact, concept sec.Concept, accession string) []domain.Fact {
	var found []domain.Fact
	for i := range facts {
		if facts[i].Concept == string(concept) && facts[i].Accession == accession {
			found = append(found, facts[i])
		}
	}
	return found
}

func TestParseCompanyFactsReadsEveryKeptValueOfEachFixture(t *testing.T) {
	// The fixtures hold only kept concepts, so every value in them is a fact.
	// The counts come from the fixture files.
	tests := []struct {
		cik  domain.CIK
		want int
	}{
		{cikNVDA, 1910},
		{cikAAPL, 2194},
		{cikKO, 1821},
		{cikGRMN, 1846},
		{cikADM, 2135},
	}
	for _, tt := range tests {
		t.Run(tt.cik.String(), func(t *testing.T) {
			facts := companyFactsFixture(t, tt.cik)
			if len(facts) != tt.want {
				t.Errorf("len(facts) = %d, want %d", len(facts), tt.want)
			}
			for _, f := range facts {
				if f.CIK != tt.cik || f.Form == "" || f.Filed.IsZero() || f.Accession == "" || f.Unit == "" || f.PeriodEnd.IsZero() {
					t.Fatalf("fact misses a source field: %+v", f)
				}
			}
		})
	}
}

func TestParseCompanyFactsKeepsTheSourceOfAQuarterlyEPS(t *testing.T) {
	// NVIDIA 10-Q for the quarter that ended 2026-07-26 (fiscal 2027 Q2),
	// filed 2026-08-26: diluted EPS of 2.46 for the quarter.
	facts := findFacts(companyFactsFixture(t, cikNVDA), sec.EarningsPerShareDiluted, "0001045810-26-000075")
	want := domain.Fact{
		CIK:          cikNVDA,
		Taxonomy:     "us-gaap",
		Concept:      "EarningsPerShareDiluted",
		Unit:         "USD/shares",
		PeriodStart:  datePtr(t, "2026-04-27"),
		PeriodEnd:    date(t, "2026-07-26"),
		Value:        2.46,
		Form:         "10-Q",
		FiscalYear:   2027,
		FiscalPeriod: "Q2",
		Frame:        "CY2026Q2",
		Filed:        date(t, "2026-08-26"),
		Accession:    "0001045810-26-000075",
	}
	for _, f := range facts {
		if f.PeriodStart != nil && *f.PeriodStart == *want.PeriodStart && f.PeriodEnd == want.PeriodEnd {
			if diff := cmp.Diff(want, f); diff != "" {
				t.Errorf("fact mismatch (-want +got):\n%s", diff)
			}
			return
		}
	}
	t.Fatalf("no EPS fact for the quarter in %d facts of the filing", len(facts))
}

func TestParseCompanyFactsReadsDEIInstantFacts(t *testing.T) {
	// The cover page of the same 10-Q: 24.1 billion shares on 2026-08-21.
	facts := findFacts(companyFactsFixture(t, cikNVDA), sec.EntityCommonStockSharesOutstanding, "0001045810-26-000075")
	want := []domain.Fact{{
		CIK:          cikNVDA,
		Taxonomy:     "dei",
		Concept:      "EntityCommonStockSharesOutstanding",
		Unit:         "shares",
		PeriodEnd:    date(t, "2026-08-21"),
		Value:        24100000000,
		Form:         "10-Q",
		FiscalYear:   2027,
		FiscalPeriod: "Q2",
		Frame:        "CY2026Q3I",
		Filed:        date(t, "2026-08-26"),
		Accession:    "0001045810-26-000075",
	}}
	if diff := cmp.Diff(want, facts); diff != "" {
		t.Errorf("facts mismatch (-want +got):\n%s", diff)
	}
}

func TestParseCompanyFactsKeepsTheOriginalAndTheAmendedValue(t *testing.T) {
	// ADM reported 2021 diluted EPS of 4.79 in its 10-K for 2021 and again in
	// the 10-K/A for 2023, filed 2024-11-18.
	facts := companyFactsFixture(t, cikADM)
	var got []domain.Fact
	for _, f := range facts {
		if f.Concept == string(sec.EarningsPerShareDiluted) && f.PeriodEnd == date(t, "2021-12-31") &&
			f.PeriodStart != nil && *f.PeriodStart == date(t, "2021-01-01") {
			got = append(got, f)
		}
	}
	type source struct {
		Form, Accession string
		Filed           domain.Date
		FiscalYear      int
		FiscalPeriod    string
	}
	gotSources := make([]source, 0, len(got))
	for _, f := range got {
		gotSources = append(gotSources, source{f.Form, f.Accession, f.Filed, f.FiscalYear, f.FiscalPeriod})
		if f.Value != 4.79 {
			t.Errorf("value in %s = %v, want 4.79", f.Accession, f.Value)
		}
	}
	want := []source{
		{"10-K", "0000007084-22-000008", date(t, "2022-02-17"), 2021, "FY"},
		{"10-K", "0000007084-23-000010", date(t, "2023-02-14"), 2022, "FY"},
		{"10-K", "0000007084-24-000009", date(t, "2024-03-12"), 2023, "FY"},
		{"10-K/A", "0000007084-24-000051", date(t, "2024-11-18"), 2023, "FY"},
	}
	if diff := cmp.Diff(want, gotSources); diff != "" {
		t.Errorf("sources mismatch (-want +got):\n%s", diff)
	}
}

func TestParseCompanyFactsLeavesMissingFiscalFieldsEmpty(t *testing.T) {
	// Apple's 8-K of 2015-01-28 has no fiscal year or period.
	facts := findFacts(companyFactsFixture(t, cikAAPL), sec.EarningsPerShareDiluted, "0001193125-15-023732")
	if len(facts) == 0 {
		t.Fatal("no facts from the 8-K")
	}
	for _, f := range facts {
		if f.FiscalYear != 0 || f.FiscalPeriod != "" || f.Form != "8-K" {
			t.Errorf("fact = %+v, want fiscal year 0, empty fiscal period and form 8-K", f)
		}
	}
}

func TestParseCompanyFactsDropsOtherConcepts(t *testing.T) {
	body := `{"cik":1,"entityName":"X","facts":{
	  "us-gaap":{
	    "Assets":{"units":{"USD":[{"end":"2024-12-31","val":5,"accn":"a","fy":2024,"fp":"FY","form":"10-K","filed":"2025-02-01"}]}},
	    "NetIncomeLoss":{"units":{"USD":[{"start":"2024-01-01","end":"2024-12-31","val":7,"accn":"a","fy":2024,"fp":"FY","form":"10-K","filed":"2025-02-01"}]}}},
	  "dei":{
	    "EntityPublicFloat":{"units":{"USD":[{"end":"2024-06-30","val":9,"accn":"a","fy":2024,"fp":"FY","form":"10-K","filed":"2025-02-01"}]}}},
	  "srt":{
	    "NetIncomeLoss":{"units":{"USD":[{"start":"2024-01-01","end":"2024-12-31","val":8,"accn":"a","fy":2024,"fp":"FY","form":"10-K","filed":"2025-02-01"}]}}}}}`
	facts, err := sec.ParseCompanyFacts([]byte(body))
	if err != nil {
		t.Fatalf("ParseCompanyFacts() error = %v", err)
	}
	if len(facts) != 1 || facts[0].Taxonomy != "us-gaap" || facts[0].Concept != "NetIncomeLoss" || facts[0].Value != 7 {
		t.Errorf("facts = %+v, want only us-gaap NetIncomeLoss", facts)
	}
}

func TestParseCompanyFactsIsStable(t *testing.T) {
	body := readFixture(t, "data.sec.gov/api/xbrl/companyfacts/CIK0000021344.json")
	first, err := sec.ParseCompanyFacts(body)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		again, err := sec.ParseCompanyFacts(body)
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(first, again); diff != "" {
			t.Fatalf("order differs between runs (-first +again):\n%s", diff)
		}
	}
}

func TestParseCompanyFactsRejectsInvalidInput(t *testing.T) {
	tests := map[string]string{
		"not json":      `{"cik":`,
		"no cik":        `{"facts":{}}`,
		"bad end date":  `{"cik":1,"facts":{"us-gaap":{"NetIncomeLoss":{"units":{"USD":[{"end":"2024-13-01","val":1,"accn":"a","form":"10-K","filed":"2025-01-01"}]}}}}}`,
		"bad filed":     `{"cik":1,"facts":{"us-gaap":{"NetIncomeLoss":{"units":{"USD":[{"end":"2024-12-31","val":1,"accn":"a","form":"10-K","filed":""}]}}}}}`,
		"string value":  `{"cik":1,"facts":{"us-gaap":{"NetIncomeLoss":{"units":{"USD":[{"end":"2024-12-31","val":"1","accn":"a","form":"10-K","filed":"2025-01-01"}]}}}}}`,
		"no accession":  `{"cik":1,"facts":{"us-gaap":{"NetIncomeLoss":{"units":{"USD":[{"end":"2024-12-31","val":1,"form":"10-K","filed":"2025-01-01"}]}}}}}`,
		"bad start":     `{"cik":1,"facts":{"us-gaap":{"NetIncomeLoss":{"units":{"USD":[{"start":"x","end":"2024-12-31","val":1,"accn":"a","form":"10-K","filed":"2025-01-01"}]}}}}}`,
		"no form":       `{"cik":1,"facts":{"us-gaap":{"NetIncomeLoss":{"units":{"USD":[{"end":"2024-12-31","val":1,"accn":"a","filed":"2025-01-01"}]}}}}}`,
		"negative cik":  `{"cik":-1,"facts":{}}`,
		"null document": `null`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := sec.ParseCompanyFacts([]byte(body)); err == nil {
				t.Error("ParseCompanyFacts() error = nil, want an error")
			}
		})
	}
}

func TestConceptsAreTheKeptSet(t *testing.T) {
	concepts := sec.Concepts()
	if len(concepts) != 17 {
		t.Fatalf("len(Concepts()) = %d, want 17", len(concepts))
	}
	for _, c := range concepts {
		want := "us-gaap"
		if c == sec.EntityCommonStockSharesOutstanding {
			want = "dei"
		}
		if c.Taxonomy() != want {
			t.Errorf("%s.Taxonomy() = %q, want %q", c, c.Taxonomy(), want)
		}
	}
	concepts[0] = "Changed"
	if sec.Concepts()[0] == "Changed" {
		t.Error("Concepts() returns a shared slice")
	}
}

func TestFixturesHoldOnlyKeptConcepts(t *testing.T) {
	// tools/sec-fixtures keeps its own copy of the concept list. This test
	// fails when the two lists drift apart.
	kept := map[string]bool{}
	for _, c := range sec.Concepts() {
		kept[c.Taxonomy()+":"+string(c)] = true
	}
	for _, cik := range []domain.CIK{cikNVDA, cikAAPL, cikKO, cikGRMN, cikADM} {
		var doc struct {
			Facts map[string]map[string]json.RawMessage `json:"facts"`
		}
		if err := json.Unmarshal(readFixture(t, "data.sec.gov/api/xbrl/companyfacts/CIK"+cik.String()+".json"), &doc); err != nil {
			t.Fatal(err)
		}
		for taxonomy, concepts := range doc.Facts {
			for name := range concepts {
				if !kept[taxonomy+":"+name] {
					t.Errorf("fixture %s holds %s:%s, which sec.Concepts() does not list", cik, taxonomy, name)
				}
			}
		}
	}
}

func TestParseCompanyFactsErrorNamesTheConcept(t *testing.T) {
	body := `{"cik":1,"facts":{"us-gaap":{"NetIncomeLoss":{"units":{"USD":[{"end":"bad","val":1,"accn":"a","form":"10-K","filed":"2025-01-01"}]}}}}}`
	_, err := sec.ParseCompanyFacts([]byte(body))
	if err == nil || !strings.Contains(err.Error(), "NetIncomeLoss") {
		t.Errorf("error = %v, want one that names NetIncomeLoss", err)
	}
}

func TestParseCompanyFactsReadsKnownValues(t *testing.T) {
	// Each row is a value as the named filing reports it.
	tests := []struct {
		name       string
		cik        domain.CIK
		concept    sec.Concept
		accession  string
		start, end string // start is empty for an instant
		unit       string
		want       float64
	}{
		{"AAPL diluted EPS, 10-Q fiscal 2026 Q3", cikAAPL, sec.EarningsPerShareDiluted, "0000320193-26-000020", "2026-03-29", "2026-06-27", "USD/shares", 2.02},
		{"AAPL 4-for-1 split, 10-Q fiscal 2021 Q3", cikAAPL, sec.StockholdersEquityNoteStockSplitConversionRatio1, "0000320193-21-000065", "", "2020-08-28", "pure", 4},
		{"NVDA 10-for-1 split, 10-Q fiscal 2025 Q2", cikNVDA, sec.StockholdersEquityNoteStockSplitConversionRatio1, "0001045810-24-000264", "2024-05-01", "2024-05-31", "pure", 10},
		{"KO dividend declared, 10-Q 2018 Q3", cikKO, sec.CommonStockDividendsPerShareDeclared, "0000021344-18-000042", "2018-06-30", "2018-09-28", "USD/shares", 0.39},
		{"GRMN revenue, 10-Q 2026 Q3", cikGRMN, sec.RevenueFromContractWithCustomerExcludingAssessedTax, "0001193125-26-435070", "2026-06-28", "2026-09-26", "USD", 1986076000},
		{"ADM diluted EPS, 10-Q/A 2024 Q2", cikADM, sec.EarningsPerShareDiluted, "0000007084-24-000053", "2024-04-01", "2024-06-30", "USD/shares", 0.98},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, f := range findFacts(companyFactsFixture(t, tt.cik), tt.concept, tt.accession) {
				start := ""
				if f.PeriodStart != nil {
					start = f.PeriodStart.String()
				}
				if start == tt.start && f.PeriodEnd.String() == tt.end && f.Unit == tt.unit {
					if f.Value != tt.want {
						t.Errorf("value = %v, want %v", f.Value, tt.want)
					}
					return
				}
			}
			t.Errorf("no %s fact for %s to %s in %s", tt.concept, tt.start, tt.end, tt.accession)
		})
	}
}
