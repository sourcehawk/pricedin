package prices_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/prices"
)

func bar(symbol, day string, closePrice float64) domain.PriceBar {
	return domain.PriceBar{Symbol: symbol, Date: date(day), Close: closePrice}
}

func split(symbol, day string, from, to float64) domain.Split {
	return domain.Split{Symbol: symbol, Date: date(day), From: from, To: to}
}

type adjusted struct {
	Date   string
	Factor float64
	Close  float64
}

func summarize(bars []prices.AdjustedBar) []adjusted {
	out := make([]adjusted, len(bars))
	for i, b := range bars {
		out[i] = adjusted{Date: b.Bar.Date.String(), Factor: b.Factor, Close: b.Close}
	}
	return out
}

func TestAdjustForSplits(t *testing.T) {
	tests := []struct {
		name   string
		bars   []domain.PriceBar
		splits []domain.Split
		want   []adjusted
	}{
		{
			name: "a 10-for-1 split divides the closes before the split date",
			bars: []domain.PriceBar{
				bar("NVDA", "2024-06-06", 1100),
				bar("NVDA", "2024-06-07", 1150),
				bar("NVDA", "2024-06-10", 116),
				bar("NVDA", "2024-06-11", 117.5),
			},
			splits: []domain.Split{split("NVDA", "2024-06-10", 1, 10)},
			want: []adjusted{
				{"2024-06-06", 10, 110},
				{"2024-06-07", 10, 115},
				{"2024-06-10", 1, 116},
				{"2024-06-11", 1, 117.5},
			},
		},
		{
			name: "factors of later splits multiply",
			bars: []domain.PriceBar{
				bar("NVDA", "2021-07-19", 800),
				bar("NVDA", "2021-07-20", 200),
				bar("NVDA", "2024-06-07", 1200),
				bar("NVDA", "2024-06-10", 120),
			},
			// Given out of date order on purpose.
			splits: []domain.Split{split("NVDA", "2024-06-10", 1, 10), split("NVDA", "2021-07-20", 1, 4)},
			want: []adjusted{
				{"2021-07-19", 40, 20},
				{"2021-07-20", 10, 20},
				{"2024-06-07", 10, 120},
				{"2024-06-10", 1, 120},
			},
		},
		{
			name:   "a reverse split multiplies the earlier closes",
			bars:   []domain.PriceBar{bar("XYZ", "2024-01-02", 2), bar("XYZ", "2024-01-03", 20)},
			splits: []domain.Split{split("XYZ", "2024-01-03", 10, 1)},
			want:   []adjusted{{"2024-01-02", 0.1, 20}, {"2024-01-03", 1, 20}},
		},
		{
			name:   "a split of another symbol does not apply",
			bars:   []domain.PriceBar{bar("AAPL", "2024-06-07", 190)},
			splits: []domain.Split{split("NVDA", "2024-06-10", 1, 10)},
			want:   []adjusted{{"2024-06-07", 1, 190}},
		},
		{
			name:   "a split after every bar applies to every bar",
			bars:   []domain.PriceBar{bar("NVDA", "2024-06-07", 1150)},
			splits: []domain.Split{split("NVDA", "2024-06-10", 1, 10)},
			want:   []adjusted{{"2024-06-07", 10, 115}},
		},
		{
			name: "without splits the closes stay raw",
			bars: []domain.PriceBar{bar("KO", "2024-06-07", 62.5)},
			want: []adjusted{{"2024-06-07", 1, 62.5}},
		},
		{
			name: "no bars give no adjusted bars",
			want: []adjusted{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := prices.AdjustForSplits(tt.bars, tt.splits)
			if err != nil {
				t.Fatalf("AdjustForSplits() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, summarize(got), cmpopts.EquateApprox(0, 1e-9)); diff != "" {
				t.Errorf("AdjustForSplits() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAdjustForSplitsKeepsTheRawBar(t *testing.T) {
	raw := domain.PriceBar{Symbol: "NVDA", Date: date("2024-06-07"), Open: 1140, High: 1160, Low: 1130, Close: 1150, Volume: 40000000, Source: "massive"}
	got, err := prices.AdjustForSplits([]domain.PriceBar{raw}, []domain.Split{split("NVDA", "2024-06-10", 1, 10)})
	if err != nil {
		t.Fatalf("AdjustForSplits() error = %v", err)
	}
	if diff := cmp.Diff(raw, got[0].Bar); diff != "" {
		t.Errorf("Bar mismatch (-want +got):\n%s", diff)
	}
}

func TestAdjustForSplitsRejectsARatioThatIsNotPositive(t *testing.T) {
	bars := []domain.PriceBar{bar("NVDA", "2024-06-07", 1150)}
	for _, s := range []domain.Split{
		split("NVDA", "2024-06-10", 0, 10),
		split("NVDA", "2024-06-10", 1, 0),
		split("NVDA", "2024-06-10", -1, 10),
	} {
		if _, err := prices.AdjustForSplits(bars, []domain.Split{s}); err == nil {
			t.Errorf("AdjustForSplits(split %v-for-%v) error = nil, want an error", s.To, s.From)
		}
	}
}
