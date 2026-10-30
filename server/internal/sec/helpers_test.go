package sec_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// fixtureDir is testdata/sec at the repository root. Below it, the files sit
// at their paths on www.sec.gov and data.sec.gov.
const fixtureDir = "../../../testdata/sec"

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.FromSlash(fixtureDir + "/" + path))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return body
}

func date(t *testing.T, s string) domain.Date {
	t.Helper()
	d, err := domain.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func datePtr(t *testing.T, s string) *domain.Date {
	t.Helper()
	d := date(t, s)
	return &d
}

const (
	cikNVDA domain.CIK = 1045810
	cikAAPL domain.CIK = 320193
	cikKO   domain.CIK = 21344
	cikGRMN domain.CIK = 1121788
	cikADM  domain.CIK = 7084
)
