package sec_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/sec"
)

func TestParseDailyIndexReadsEveryFiling(t *testing.T) {
	got, err := sec.ParseDailyIndex(readFixture(t, "www.sec.gov/Archives/edgar/daily-index/2026/QTR3/master.20260826.idx"))
	if err != nil {
		t.Fatalf("ParseDailyIndex() error = %v", err)
	}
	if len(got) != 30 {
		t.Errorf("len = %d, want 30", len(got))
	}
	nvda, ok := findFiling(got, "0001045810-26-000075")
	if !ok {
		t.Fatal("NVIDIA 10-Q not found")
	}
	want := domain.Filing{Accession: "0001045810-26-000075", CIK: cikNVDA, Form: "10-Q", Filed: date(t, "2026-08-26")}
	if diff := cmp.Diff(want, nvda); diff != "" {
		t.Errorf("filing mismatch (-want +got):\n%s", diff)
	}
}

func TestParseDailyIndexAcceptsAnIndexWithoutRows(t *testing.T) {
	body := "Description: x\n\nCIK|Company Name|Form Type|Date Filed|File Name\n-----\n"
	got, err := sec.ParseDailyIndex([]byte(body))
	if err != nil || len(got) != 0 {
		t.Errorf("ParseDailyIndex() = %v, %v, want no filings and no error", got, err)
	}
}

func TestParseDailyIndexAcceptsCRLF(t *testing.T) {
	body := "CIK|Company Name|Form Type|Date Filed|File Name\r\n-----\r\n7084|ADM|10-K/A|20241118|edgar/data/7084/0000007084-24-000051.txt\r\n"
	got, err := sec.ParseDailyIndex([]byte(body))
	if err != nil {
		t.Fatalf("ParseDailyIndex() error = %v", err)
	}
	want := []domain.Filing{{Accession: "0000007084-24-000051", CIK: cikADM, Form: "10-K/A", Filed: date(t, "2024-11-18")}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}

func TestParseDailyIndexRejectsInvalidInput(t *testing.T) {
	header := "CIK|Company Name|Form Type|Date Filed|File Name\n-----\n"
	tests := map[string]string{
		"no header":     "7084|ADM|10-K|20241118|edgar/data/7084/0000007084-24-000051.txt\n",
		"four fields":   header + "7084|ADM|10-K|20241118\n",
		"bad cik":       header + "x|ADM|10-K|20241118|edgar/data/7084/0000007084-24-000051.txt\n",
		"bad date":      header + "7084|ADM|10-K|2024-11-18|edgar/data/7084/0000007084-24-000051.txt\n",
		"bad file name": header + "7084|ADM|10-K|20241118|edgar/data/7084/\n",
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := sec.ParseDailyIndex([]byte(body)); err == nil {
				t.Error("ParseDailyIndex() error = nil, want an error")
			}
		})
	}
}
