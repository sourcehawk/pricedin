package domain_test

import (
	"errors"
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

func TestParseSymbol(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"NVDA", "NVDA"},
		{"F", "F"},
		{"BRK-B", "BRK-B"},     // SEC's form of a share class
		{"BRK.B", "BRK.B"},     // the exchanges' form
		{"KCAC-UN", "KCAC-UN"}, // a unit, the longest SEC symbols
		{"LVWR.WS", "LVWR.WS"}, // a warrant, the longest Massive symbols
		{"ABCDEFGHIJ", "ABCDEFGHIJ"},
		{"1234", "1234"},
		{" brk-b ", "BRK-B"},
		{"googl", "GOOGL"},
	}
	for _, tt := range tests {
		got, err := domain.ParseSymbol(tt.in)
		if err != nil {
			t.Errorf("ParseSymbol(%q) error = %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseSymbol(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseSymbolRejectsNonSymbols(t *testing.T) {
	for _, in := range []string{"", "  ", "ABCDEFGHIJK", "-B", ".B", "BRK B", "../KO", "KO/", "BRK_B", "ÅB", "K\tO"} {
		got, err := domain.ParseSymbol(in)
		if err == nil {
			t.Errorf("ParseSymbol(%q) = %q, want an error", in, got)
			continue
		}
		if !errors.Is(err, domain.ErrInvalidSymbol) {
			t.Errorf("ParseSymbol(%q) error = %v, want ErrInvalidSymbol", in, err)
		}
	}
}
