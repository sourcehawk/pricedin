package main

import "time"

// This file copies the NYSE calendar of server/internal/prices, because a
// program outside the server module cannot import its internal packages.
// A test in server/internal/prices/massive checks that the dates of the
// committed aggs fixtures are exactly the server's trading days.

// tradingDays returns the trading days from from to to, both included.
// The times are midnight UTC.
func tradingDays(from, to time.Time) []time.Time {
	var days []time.Time
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if isTradingDay(d) {
			days = append(days, d)
		}
	}
	return days
}

func isTradingDay(d time.Time) bool {
	switch d.Weekday() {
	case time.Saturday, time.Sunday:
		return false
	default:
		return !isHoliday(d) && !isSpecialClosure(d)
	}
}

func isSpecialClosure(d time.Time) bool {
	switch d.Format(time.DateOnly) {
	case "2001-09-11", "2001-09-12", "2001-09-13", "2001-09-14",
		"2004-06-11", "2007-01-02", "2012-10-29", "2012-10-30",
		"2018-12-05", "2025-01-09":
		return true
	default:
		return false
	}
}

func isHoliday(d time.Time) bool {
	y := d.Year()
	holidays := []time.Time{
		nthWeekday(y, time.January, time.Monday, 3),
		nthWeekday(y, time.February, time.Monday, 3),
		easter(y).AddDate(0, 0, -2),
		lastWeekday(y, time.May, time.Monday),
		observed(day(y, time.July, 4)),
		nthWeekday(y, time.September, time.Monday, 1),
		nthWeekday(y, time.November, time.Thursday, 4),
		observed(day(y, time.December, 25)),
	}
	if newYear := day(y, time.January, 1); newYear.Weekday() != time.Saturday {
		holidays = append(holidays, observed(newYear))
	}
	if y >= 2022 {
		holidays = append(holidays, observed(day(y, time.June, 19)))
	}
	for _, h := range holidays {
		if h.Equal(d) {
			return true
		}
	}
	return false
}

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func observed(d time.Time) time.Time {
	switch d.Weekday() {
	case time.Saturday:
		return d.AddDate(0, 0, -1)
	case time.Sunday:
		return d.AddDate(0, 0, 1)
	default:
		return d
	}
}

func nthWeekday(year int, month time.Month, wd time.Weekday, n int) time.Time {
	first := day(year, month, 1)
	offset := (int(wd) - int(first.Weekday()) + 7) % 7
	return first.AddDate(0, 0, offset+7*(n-1))
}

func lastWeekday(year int, month time.Month, wd time.Weekday) time.Time {
	last := day(year, month+1, 0)
	offset := (int(last.Weekday()) - int(wd) + 7) % 7
	return last.AddDate(0, 0, -offset)
}

func easter(year int) time.Time {
	a := year % 19
	b, c := year/100, year%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	dom := (h+l-7*m+114)%31 + 1
	return day(year, time.Month(month), dom)
}
