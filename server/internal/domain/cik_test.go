package domain_test

import (
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

func TestCIKStringPadsToTenDigits(t *testing.T) {
	tests := []struct {
		cik  domain.CIK
		want string
	}{
		{320193, "0000320193"},
		{1, "0000000001"},
		{1045810, "0001045810"},
		{9999999999, "9999999999"},
	}
	for _, tt := range tests {
		if got := tt.cik.String(); got != tt.want {
			t.Errorf("CIK(%d).String() = %q, want %q", int64(tt.cik), got, tt.want)
		}
	}
}

func TestParseCIK(t *testing.T) {
	tests := []struct {
		in   string
		want domain.CIK
	}{
		{"0000320193", 320193},
		{"320193", 320193},
		{"1", 1},
		{"9999999999", 9999999999},
	}
	for _, tt := range tests {
		got, err := domain.ParseCIK(tt.in)
		if err != nil {
			t.Errorf("ParseCIK(%q) error = %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseCIK(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseCIKRejectsInvalidInput(t *testing.T) {
	for _, in := range []string{"", "0", "0000000000", "-5", "+5", "12a", " 320193", "00000320193", "12345678901", "CIK0000320193"} {
		if got, err := domain.ParseCIK(in); err == nil {
			t.Errorf("ParseCIK(%q) = %d, want an error", in, got)
		}
	}
}
