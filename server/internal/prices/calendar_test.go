package prices_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/prices"
)

func date(s string) domain.Date {
	d, err := domain.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

// The NYSE full-day closures of 2024 to 2026, from the exchange's published
// holiday calendars.
var nyseClosures = map[int][]string{
	2024: {
		"2024-01-01", // New Year's Day
		"2024-01-15", // Martin Luther King Jr. Day
		"2024-02-19", // Washington's Birthday
		"2024-03-29", // Good Friday
		"2024-05-27", // Memorial Day
		"2024-06-19", // Juneteenth
		"2024-07-04", // Independence Day
		"2024-09-02", // Labor Day
		"2024-11-28", // Thanksgiving Day
		"2024-12-25", // Christmas Day
	},
	2025: {
		"2025-01-01",
		"2025-01-09", // national day of mourning for President Carter
		"2025-01-20",
		"2025-02-17",
		"2025-04-18",
		"2025-05-26",
		"2025-06-19",
		"2025-07-04",
		"2025-09-01",
		"2025-11-27",
		"2025-12-25",
	},
	2026: {
		"2026-01-01",
		"2026-01-19",
		"2026-02-16",
		"2026-04-03",
		"2026-05-25",
		"2026-06-19",
		"2026-07-03", // July 4 is a Saturday
		"2026-09-07",
		"2026-11-26",
		"2026-12-25",
	},
}

func TestTradingDaysSkipsEveryNYSEClosureFrom2024To2026(t *testing.T) {
	for year, closures := range nyseClosures {
		closed := map[domain.Date]bool{}
		for _, s := range closures {
			closed[date(s)] = true
		}
		first := domain.NewDate(year, time.January, 1)
		last := domain.NewDate(year, time.December, 31)

		var want []domain.Date
		for d := first; !d.After(last); d = d.AddDays(1) {
			wd := d.Time().Weekday()
			if wd != time.Saturday && wd != time.Sunday && !closed[d] {
				want = append(want, d)
			}
		}
		if diff := cmp.Diff(want, prices.TradingDays(first, last)); diff != "" {
			t.Errorf("TradingDays(%d) mismatch (-want +got):\n%s", year, diff)
		}
	}
}

func TestIsTradingDay(t *testing.T) {
	tests := []struct {
		day  string
		want bool
		why  string
	}{
		{"2024-06-10", true, "an ordinary Monday"},
		{"2024-06-08", false, "a Saturday"},
		{"2024-06-09", false, "a Sunday"},
		{"2022-01-17", false, "Martin Luther King Jr. Day"},
		{"2021-12-31", true, "New Year's Day 2022 is a Saturday and is not observed on the Friday before"},
		{"2022-12-26", false, "Christmas 2022 is a Sunday, observed on Monday"},
		{"2021-12-24", false, "Christmas 2021 is a Saturday, observed on Friday"},
		{"2027-12-24", false, "Christmas 2027 is a Saturday, observed on Friday"},
		{"2027-12-31", true, "New Year's Day 2028 is a Saturday and is not observed"},
		{"2023-01-02", false, "New Year's Day 2023 is a Sunday, observed on Monday"},
		{"2021-06-18", true, "Juneteenth is a holiday only from 2022"},
		{"2022-06-20", false, "Juneteenth 2022 is a Sunday, observed on Monday"},
		{"2027-06-18", false, "Juneteenth 2027 is a Saturday, observed on Friday"},
		{"2020-07-03", false, "Independence Day 2020 is a Saturday, observed on Friday"},
		{"2021-07-05", false, "Independence Day 2021 is a Sunday, observed on Monday"},
		{"2019-04-19", false, "Good Friday 2019"},
		{"2038-04-23", false, "Good Friday 2038, Easter on April 25"},
		{"2008-03-21", false, "Good Friday 2008, Easter on March 23"},
		{"2018-12-05", false, "national day of mourning for President George H. W. Bush"},
		{"2012-10-29", false, "closed for Hurricane Sandy"},
		{"2012-10-30", false, "closed for Hurricane Sandy"},
		{"2024-11-29", true, "the day after Thanksgiving closes early but trades"},
		{"2024-12-24", true, "Christmas Eve closes early but trades"},
	}
	for _, tt := range tests {
		t.Run(tt.day, func(t *testing.T) {
			if got := prices.IsTradingDay(date(tt.day)); got != tt.want {
				t.Errorf("IsTradingDay(%s) = %t, want %t: %s", tt.day, got, tt.want, tt.why)
			}
		})
	}
}

func TestTradingDaysIncludesBothEnds(t *testing.T) {
	got := prices.TradingDays(date("2024-06-07"), date("2024-06-10"))
	want := []domain.Date{date("2024-06-07"), date("2024-06-10")}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("TradingDays mismatch (-want +got):\n%s", diff)
	}
}

func TestTradingDaysIsEmptyWhenFromIsAfterTo(t *testing.T) {
	if got := prices.TradingDays(date("2024-06-11"), date("2024-06-10")); len(got) != 0 {
		t.Errorf("TradingDays(from after to) = %v, want none", got)
	}
}

func TestTradingDaysIsEmptyForAClosedRange(t *testing.T) {
	if got := prices.TradingDays(date("2024-06-08"), date("2024-06-09")); len(got) != 0 {
		t.Errorf("TradingDays(weekend) = %v, want none", got)
	}
}
