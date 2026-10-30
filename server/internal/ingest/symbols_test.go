package ingest_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/ingest"
)

func TestSymbolMappingBetweenSECAndTheProvider(t *testing.T) {
	tests := []struct{ sec, provider string }{
		{"BRK-B", "BRK.B"},
		{"NVDA", "NVDA"},
		{"BF-A", "BF.A"},
	}
	for _, tt := range tests {
		if got := ingest.ProviderSymbol(tt.sec); got != tt.provider {
			t.Errorf("ProviderSymbol(%q) = %q, want %q", tt.sec, got, tt.provider)
		}
		if got := ingest.SECSymbol(tt.provider); got != tt.sec {
			t.Errorf("SECSymbol(%q) = %q, want %q", tt.provider, got, tt.sec)
		}
	}
}

func TestSelectUniverse(t *testing.T) {
	current := []domain.Ticker{
		{Symbol: "NVDA", CIK: 1045810, Exchange: "Nasdaq"},
		{Symbol: "BRK-B", CIK: 1067983, Exchange: "NYSE"},
		{Symbol: "CYATY", CIK: 2070829, Exchange: "OTC"},
		{Symbol: "ET", CIK: 1276187, Exchange: "TXSE"},
		{Symbol: "INBT", CIK: 2156068, Exchange: ""},
		{Symbol: "GOOGL", CIK: 1652044, Exchange: "Nasdaq"},
		{Symbol: "GOOG", CIK: 1652044, Exchange: "Nasdaq"},
	}
	tests := []struct {
		name   string
		narrow []string
		want   []string
	}{
		{"every NYSE and Nasdaq ticker", nil, []string{"BRK-B", "GOOG", "GOOGL", "NVDA"}},
		{"narrowed", []string{"nvda", "BRK-B"}, []string{"BRK-B", "NVDA"}},
		{"narrowing keeps the exchange rule", []string{"CYATY", "NVDA"}, []string{"NVDA"}},
		{"narrowed to an unknown symbol", []string{"ZZZZ"}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := []string{}
			for _, m := range ingest.SelectUniverse(current, tt.narrow) {
				got = append(got, m.Symbol)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("SelectUniverse() (-want +got):\n%s", diff)
			}
		})
	}
}
