package fundamentals_test

import (
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/fundamentals"
)

// ttmSeries gives five quarters with EPS 1 to 5. Quarter i was first filed
// on firstFiled[i] and last refiled a year later.
func ttmSeries(t *testing.T, firstFiled ...string) fundamentals.Series {
	t.Helper()
	var s fundamentals.Series
	for i, ff := range firstFiled {
		first := day(t, ff)
		v := fundamentals.Value{
			V:          float64(i + 1),
			Method:     fundamentals.MethodReported,
			Inputs:     []fundamentals.SourceRef{{Accession: "a" + ff}},
			Filed:      first.AddDays(365),
			FirstFiled: first,
		}
		s.Quarters = append(s.Quarters, fundamentals.Quarter{
			FiscalYear: 2025, FiscalQuarter: i%4 + 1,
			Values: map[fundamentals.Metric]fundamentals.Value{
				fundamentals.EPSDiluted:    v,
				fundamentals.DilutedShares: v,
			},
		})
	}
	return s
}

func TestTTMSumsTheFourQuartersEndingAtTheIndex(t *testing.T) {
	s := ttmSeries(t, "2024-04-25", "2024-07-25", "2024-10-25", "2025-02-20", "2025-04-25")

	v, ok := fundamentals.TTM(s, fundamentals.EPSDiluted, 4)
	if !ok || v.V != 2+3+4+5 || v.Method != fundamentals.MethodTTM || len(v.Inputs) != 4 {
		t.Fatalf("TTM(at 4) = %+v, %v; want 14 ttm with 4 inputs", v, ok)
	}
	if v.FirstFiled.String() != "2025-04-25" || v.Filed.String() != "2026-04-25" {
		t.Errorf("FirstFiled, Filed = %s, %s; want 2025-04-25, 2026-04-25", v.FirstFiled, v.Filed)
	}
}

func TestTTMIsUnavailableWithoutFourQuartersOfAFlow(t *testing.T) {
	s := ttmSeries(t, "2024-04-25", "2024-07-25", "2024-10-25", "2025-02-20", "2025-04-25")
	gap := ttmSeries(t, "2024-04-25", "2024-07-25", "2024-10-25", "2025-02-20")
	delete(gap.Quarters[1].Values, fundamentals.EPSDiluted)

	tests := []struct {
		name string
		s    fundamentals.Series
		m    fundamentals.Metric
		at   int
	}{
		{"fewer than four quarters", s, fundamentals.EPSDiluted, 2},
		{"index past the end", s, fundamentals.EPSDiluted, 5},
		{"negative index", s, fundamentals.EPSDiluted, -1},
		{"a quarter lacks the metric", gap, fundamentals.EPSDiluted, 3},
		{"share counts do not add up", s, fundamentals.DilutedShares, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if v, ok := fundamentals.TTM(tt.s, tt.m, tt.at); ok {
				t.Errorf("TTM() = %v, want unavailable", v.V)
			}
		})
	}
}

func TestTTMKnownOnUsesTheDateEachQuarterFirstBecameKnown(t *testing.T) {
	s := ttmSeries(t, "2024-04-25", "2024-07-25", "2024-10-25", "2025-02-20", "2025-04-25")

	tests := []struct {
		on     string
		wantAt int
		wantV  float64
		ok     bool
	}{
		{"2025-02-19", 0, 0, false},
		{"2025-02-20", 3, 10, true},
		{"2025-04-24", 3, 10, true},
		{"2025-04-25", 4, 14, true},
		{"2030-01-01", 4, 14, true},
	}
	for _, tt := range tests {
		t.Run(tt.on, func(t *testing.T) {
			at, v, ok := fundamentals.TTMKnownOn(s, fundamentals.EPSDiluted, day(t, tt.on))
			if ok != tt.ok || (ok && (at != tt.wantAt || v.V != tt.wantV)) {
				t.Errorf("TTMKnownOn(%s) = %d, %v, %v; want %d, %v, %v", tt.on, at, v.V, ok, tt.wantAt, tt.wantV, tt.ok)
			}
		})
	}
}
