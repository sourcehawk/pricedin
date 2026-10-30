package sec_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/sec"
)

func submissionsFixture(t *testing.T, cik domain.CIK) sec.Submissions {
	t.Helper()
	sub, err := sec.ParseSubmissions(readFixture(t, "data.sec.gov/submissions/CIK"+cik.String()+".json"))
	if err != nil {
		t.Fatalf("ParseSubmissions() error = %v", err)
	}
	return sub
}

func findFiling(filings []domain.Filing, accession string) (domain.Filing, bool) {
	for _, f := range filings {
		if f.Accession == accession {
			return f, true
		}
	}
	return domain.Filing{}, false
}

func TestParseSubmissionsReadsTheCompany(t *testing.T) {
	tests := []domain.Company{
		{CIK: cikNVDA, Name: "NVIDIA CORP", SIC: "3674", SICDescription: "Semiconductors & Related Devices", FiscalYearEnd: "0131", StateOfIncorporation: "DE"},
		{CIK: cikAAPL, Name: "Apple Inc.", SIC: "3571", SICDescription: "Electronic Computers", FiscalYearEnd: "0926", StateOfIncorporation: "CA"},
		{CIK: cikKO, Name: "COCA COLA CO", SIC: "2080", SICDescription: "Beverages", FiscalYearEnd: "1231", StateOfIncorporation: "DE"},
		{CIK: cikGRMN, Name: "GARMIN LTD", SIC: "3812", SICDescription: "Search, Detection, Navigation, Guidance, Aeronautical Sys", FiscalYearEnd: "1226", StateOfIncorporation: "V8"},
		{CIK: cikADM, Name: "Archer-Daniels-Midland Co", SIC: "2070", SICDescription: "Fats & Oils", FiscalYearEnd: "1231", StateOfIncorporation: "DE"},
	}
	for _, want := range tests {
		t.Run(want.Name, func(t *testing.T) {
			if diff := cmp.Diff(want, submissionsFixture(t, want.CIK).Company); diff != "" {
				t.Errorf("company mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseSubmissionsReadsRecentFilingsAndExtraFileNames(t *testing.T) {
	// The counts are the rows of filings.recent in each trimmed fixture.
	tests := []struct {
		cik       domain.CIK
		wantCount int
		wantFiles []string
	}{
		{cikNVDA, 24, []string{"CIK0001045810-submissions-001.json"}},
		{cikAAPL, 23, []string{"CIK0000320193-submissions-001.json"}},
		{cikKO, 25, []string{"CIK0000021344-submissions-001.json", "CIK0000021344-submissions-002.json"}},
		{cikGRMN, 26, []string{"CIK0001121788-submissions-001.json"}},
		{cikADM, 27, []string{"CIK0000007084-submissions-001.json", "CIK0000007084-submissions-002.json"}},
	}
	for _, tt := range tests {
		t.Run(tt.cik.String(), func(t *testing.T) {
			sub := submissionsFixture(t, tt.cik)
			if len(sub.Filings) != tt.wantCount {
				t.Errorf("len(Filings) = %d, want %d", len(sub.Filings), tt.wantCount)
			}
			if diff := cmp.Diff(tt.wantFiles, sub.Files); diff != "" {
				t.Errorf("Files mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseSubmissionsReadsEachFiling(t *testing.T) {
	tests := []domain.Filing{
		{Accession: "0001045810-26-000075", CIK: cikNVDA, Form: "10-Q", Filed: date(t, "2026-08-26"), ReportDate: datePtr(t, "2026-07-26"), PrimaryDocument: "nvda-20260726.htm"},
		{Accession: "0000007084-24-000051", CIK: cikADM, Form: "10-K/A", Filed: date(t, "2024-11-18"), ReportDate: datePtr(t, "2023-12-31"), PrimaryDocument: "adm-20231231.htm"},
	}
	for _, want := range tests {
		t.Run(want.Accession, func(t *testing.T) {
			got, ok := findFiling(submissionsFixture(t, want.CIK).Filings, want.Accession)
			if !ok {
				t.Fatal("filing not found")
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("filing mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseSubmissionsFileReadsOlderFilings(t *testing.T) {
	got, err := sec.ParseSubmissionsFile(cikNVDA, readFixture(t, "data.sec.gov/submissions/CIK0001045810-submissions-001.json"))
	if err != nil {
		t.Fatalf("ParseSubmissionsFile() error = %v", err)
	}
	if len(got) != 88 {
		t.Errorf("len = %d, want 88", len(got))
	}
	want := []domain.Filing{
		{Accession: "0001045810-20-000147", CIK: cikNVDA, Form: "10-Q", Filed: date(t, "2020-08-19"), ReportDate: datePtr(t, "2020-07-26"), PrimaryDocument: "nvda-20200726.htm"},
		// Filings from before 2001 have no primary document.
		{Accession: "0001012870-99-001954", CIK: cikNVDA, Form: "10-Q", Filed: date(t, "1999-06-15"), ReportDate: datePtr(t, "1999-05-02")},
	}
	for _, w := range want {
		f, ok := findFiling(got, w.Accession)
		if !ok {
			t.Errorf("%s not found", w.Accession)
			continue
		}
		if diff := cmp.Diff(w, f); diff != "" {
			t.Errorf("%s mismatch (-want +got):\n%s", w.Accession, diff)
		}
	}
}

func TestParseSubmissionsLeavesAnEmptyReportDateNil(t *testing.T) {
	body := `{"cik":"0000000001","name":"A","filings":{"recent":{
	  "accessionNumber":["0000000001-26-000001"],"form":["8-K"],"filingDate":["2026-01-02"],
	  "reportDate":[""],"primaryDocument":["a.htm"]},"files":[]}}`
	sub, err := sec.ParseSubmissions([]byte(body))
	if err != nil {
		t.Fatalf("ParseSubmissions() error = %v", err)
	}
	want := []domain.Filing{{Accession: "0000000001-26-000001", CIK: 1, Form: "8-K", Filed: date(t, "2026-01-02"), PrimaryDocument: "a.htm"}}
	if diff := cmp.Diff(want, sub.Filings); diff != "" {
		t.Errorf("filings mismatch (-want +got):\n%s", diff)
	}
}

func TestParseSubmissionsRejectsInvalidInput(t *testing.T) {
	tests := map[string]string{
		"not json":         `{"cik":`,
		"no cik":           `{"name":"A","filings":{"recent":{}}}`,
		"columns differ":   `{"cik":"1","filings":{"recent":{"accessionNumber":["a","b"],"form":["8-K"],"filingDate":["2026-01-02"],"reportDate":[""],"primaryDocument":[""]}}}`,
		"bad filing date":  `{"cik":"1","filings":{"recent":{"accessionNumber":["a"],"form":["8-K"],"filingDate":["01/02/2026"],"reportDate":[""],"primaryDocument":[""]}}}`,
		"bad report date":  `{"cik":"1","filings":{"recent":{"accessionNumber":["a"],"form":["8-K"],"filingDate":["2026-01-02"],"reportDate":["x"],"primaryDocument":[""]}}}`,
		"empty accession":  `{"cik":"1","filings":{"recent":{"accessionNumber":[""],"form":["8-K"],"filingDate":["2026-01-02"],"reportDate":[""],"primaryDocument":[""]}}}`,
		"no filing dates":  `{"cik":"1","filings":{"recent":{"accessionNumber":["a"],"form":["8-K"],"reportDate":[""],"primaryDocument":[""]}}}`,
		"empty extra name": `{"cik":"1","filings":{"recent":{},"files":[{"name":""}]}}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := sec.ParseSubmissions([]byte(body)); err == nil {
				t.Error("ParseSubmissions() error = nil, want an error")
			}
		})
	}
}

func TestParseSubmissionsAcceptsNoFilings(t *testing.T) {
	sub, err := sec.ParseSubmissions([]byte(`{"cik":"0000000001","name":"A","filings":{"recent":{"accessionNumber":[],"form":[],"filingDate":[],"reportDate":[],"primaryDocument":[]},"files":[]}}`))
	if err != nil {
		t.Fatalf("ParseSubmissions() error = %v", err)
	}
	if len(sub.Filings) != 0 || len(sub.Files) != 0 || sub.Company.CIK != 1 {
		t.Errorf("got %+v, want company 1 with no filings", sub)
	}
}

func TestParseSubmissionsFileRejectsInvalidInput(t *testing.T) {
	if _, err := sec.ParseSubmissionsFile(cikNVDA, []byte(`{"accessionNumber":["a"],"form":[]}`)); err == nil {
		t.Error("ParseSubmissionsFile() error = nil, want an error")
	}
	if _, err := sec.ParseSubmissionsFile(0, []byte(`{}`)); err == nil {
		t.Error("ParseSubmissionsFile(0) error = nil, want an error")
	}
}
