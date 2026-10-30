package ingest_test

import (
	"testing"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/ingest"
)

func TestParseReprocessArgs(t *testing.T) {
	tests := []struct {
		args []string
		want ingest.ReprocessOptions
	}{
		{nil, ingest.ReprocessOptions{}},
		{[]string{"--prefix", "sec"}, ingest.ReprocessOptions{Prefix: ingest.ReprocessSEC}},
		{[]string{"--prefix=prices", "--since", "2026-10-01", "--cik", "1045810"}, ingest.ReprocessOptions{
			Prefix: ingest.ReprocessPrices, Since: domain.NewDate(2026, time.October, 1), CIK: 1045810,
		}},
		{[]string{"--cik=0001045810"}, ingest.ReprocessOptions{CIK: 1045810}},
	}
	for _, tt := range tests {
		got, err := ingest.ParseReprocessArgs(tt.args)
		if err != nil || got != tt.want {
			t.Errorf("ParseReprocessArgs(%q) = %+v, %v; want %+v", tt.args, got, err, tt.want)
		}
	}
}

func TestParseReprocessArgsRejectsWhatItDoesNotKnow(t *testing.T) {
	for _, args := range [][]string{
		{"--prefix", "stooq"},
		{"--since", "2026-13-01"},
		{"--cik", "0"},
		{"--cik", "nvda"},
		{"--wait"},
		{"sec"},
		{"--prefix"},
	} {
		if _, err := ingest.ParseReprocessArgs(args); err == nil {
			t.Errorf("ParseReprocessArgs(%q) error = nil, want an error", args)
		}
	}
}

// A reprocess reads only the blob store, so `jobs run` gives it no client.
func TestTheReprocessJobNeedsNoClient(t *testing.T) {
	job, ok := ingest.LookupJob(ingest.JobReprocess)
	if !ok {
		t.Fatal("no reprocess job")
	}
	if job.SEC || job.Prices {
		t.Errorf("reprocess job SEC = %v, Prices = %v; want neither", job.SEC, job.Prices)
	}
}

func TestTheReprocessJobChecksItsArgumentsWithoutADatabase(t *testing.T) {
	job, _ := ingest.LookupJob(ingest.JobReprocess)
	if err := job.CheckArgs([]string{"--since", "2026-10-01"}); err != nil {
		t.Errorf("CheckArgs(--since 2026-10-01) = %v, want nil", err)
	}
	if err := job.CheckArgs([]string{"--prefix", "stooq"}); err == nil {
		t.Error("CheckArgs(--prefix stooq) = nil, want an error")
	}
}
