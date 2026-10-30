package main

import (
	"testing"
	"time"
)

func mustDate(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return d
}

// The full trading calendar is checked against the server's calendar in
// server/internal/prices/massive, through the committed aggs fixtures.
func TestIsTradingDay(t *testing.T) {
	tests := map[string]bool{
		"2024-06-10": true,
		"2024-06-08": false, // Saturday
		"2024-03-29": false, // Good Friday
		"2025-01-09": false, // national day of mourning
		"2026-07-03": false, // Independence Day observed
		"2021-12-31": true,  // New Year's Day 2022 is a Saturday and not observed
		"2021-12-24": false, // Christmas observed
		"2022-06-20": false, // Juneteenth observed
		"2021-06-18": true,  // before Juneteenth became a holiday
	}
	for day, want := range tests {
		if got := isTradingDay(mustDate(day)); got != want {
			t.Errorf("isTradingDay(%s) = %t, want %t", day, got, want)
		}
	}
}
