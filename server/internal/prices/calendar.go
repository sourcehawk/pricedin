package prices

import (
	"time"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// TradingDays returns the US stock market trading days from from to to,
// both included, in order. It is empty when from is after to. Days with an
// early close count as trading days.
func TradingDays(from, to domain.Date) []domain.Date {
	var days []domain.Date
	for d := from; !d.After(to); d = d.AddDays(1) {
		if IsTradingDay(d) {
			days = append(days, d)
		}
	}
	return days
}

// IsTradingDay reports whether the NYSE is open on d. It knows the regular
// holidays for any year, and the special closures since 2001 such as
// national days of mourning. A special closure announced after this code
// was written is missing until someone adds it.
func IsTradingDay(d domain.Date) bool {
	switch d.Time().Weekday() {
	case time.Saturday, time.Sunday:
		return false
	default:
		return !isHoliday(d) && !isSpecialClosure(d)
	}
}

// isSpecialClosure reports the full-day NYSE closures that no yearly rule
// gives.
func isSpecialClosure(d domain.Date) bool {
	switch d.String() {
	case "2001-09-11", "2001-09-12", "2001-09-13", "2001-09-14", // September 11 attacks
		"2004-06-11",               // President Reagan
		"2007-01-02",               // President Ford
		"2012-10-29", "2012-10-30", // Hurricane Sandy
		"2018-12-05", // President George H. W. Bush
		"2025-01-09": // President Carter
		return true
	default:
		return false
	}
}

func isHoliday(d domain.Date) bool {
	y := d.Year
	holidays := []domain.Date{
		nthWeekday(y, time.January, time.Monday, 3),    // Martin Luther King Jr. Day
		nthWeekday(y, time.February, time.Monday, 3),   // Washington's Birthday
		easter(y).AddDays(-2),                          // Good Friday
		lastWeekday(y, time.May, time.Monday),          // Memorial Day
		observed(domain.NewDate(y, time.July, 4)),      // Independence Day
		nthWeekday(y, time.September, time.Monday, 1),  // Labor Day
		nthWeekday(y, time.November, time.Thursday, 4), // Thanksgiving Day
		observed(domain.NewDate(y, time.December, 25)), // Christmas Day
	}
	// NYSE Rule 7.2 keeps the market open on the Friday before a New Year's
	// Day that falls on a Saturday, because that Friday ends the yearly
	// accounting period.
	if newYear := domain.NewDate(y, time.January, 1); newYear.Time().Weekday() != time.Saturday {
		holidays = append(holidays, observed(newYear))
	}
	if y >= 2022 {
		holidays = append(holidays, observed(domain.NewDate(y, time.June, 19)))
	}
	for _, h := range holidays {
		if h == d {
			return true
		}
	}
	return false
}

// observed moves a holiday on a Saturday to the Friday before and a holiday
// on a Sunday to the Monday after.
func observed(d domain.Date) domain.Date {
	switch d.Time().Weekday() {
	case time.Saturday:
		return d.AddDays(-1)
	case time.Sunday:
		return d.AddDays(1)
	default:
		return d
	}
}

// nthWeekday returns the nth (from 1) given weekday of the month.
func nthWeekday(year int, month time.Month, wd time.Weekday, n int) domain.Date {
	first := domain.NewDate(year, month, 1)
	offset := (int(wd) - int(first.Time().Weekday()) + 7) % 7
	return first.AddDays(offset + 7*(n-1))
}

func lastWeekday(year int, month time.Month, wd time.Weekday) domain.Date {
	last := domain.NewDate(year, month+1, 0)
	offset := (int(last.Time().Weekday()) - int(wd) + 7) % 7
	return last.AddDays(-offset)
}

// easter returns Easter Sunday of the Gregorian calendar (the anonymous
// algorithm of Meeus, Jones and Butcher).
func easter(year int) domain.Date {
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
	day := (h+l-7*m+114)%31 + 1
	return domain.NewDate(year, time.Month(month), day)
}
