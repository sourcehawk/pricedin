package domain_test

import (
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

func TestFilingURL(t *testing.T) {
	tests := []struct {
		name   string
		filing domain.Filing
		want   string
	}{
		{
			name:   "primary document",
			filing: domain.Filing{Accession: "0001045810-24-000124", CIK: 1045810, PrimaryDocument: "nvda-20240428.htm"},
			want:   "https://www.sec.gov/Archives/edgar/data/1045810/000104581024000124/nvda-20240428.htm",
		},
		{
			name:   "index page without a primary document",
			filing: domain.Filing{Accession: "0000320193-24-000123", CIK: 320193},
			want:   "https://www.sec.gov/Archives/edgar/data/320193/000032019324000123/0000320193-24-000123-index.htm",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.filing.URL(); got != tt.want {
				t.Errorf("URL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSplitRatio(t *testing.T) {
	tests := []struct {
		split domain.Split
		want  float64
	}{
		{domain.Split{From: 1, To: 10}, 10},
		{domain.Split{From: 1, To: 4}, 4},
		{domain.Split{From: 3, To: 2}, 2.0 / 3.0},
		{domain.Split{From: 10, To: 1}, 0.1},
	}
	for _, tt := range tests {
		if got := tt.split.Ratio(); got != tt.want {
			t.Errorf("Split{From: %v, To: %v}.Ratio() = %v, want %v", tt.split.From, tt.split.To, got, tt.want)
		}
	}
}
