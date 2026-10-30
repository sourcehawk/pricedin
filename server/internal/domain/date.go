package domain

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

const dateLayout = "2006-01-02"

// Date is a calendar day with no time zone, such as a filing date or the
// date of a daily close. The zero Date means "no date": it marshals to JSON
// null and to SQL NULL.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// NewDate returns the date for year, month and day. Out-of-range values
// normalize the way time.Date does: February 30 becomes March 1 or 2.
func NewDate(year int, month time.Month, day int) Date {
	return DateOf(time.Date(year, month, day, 0, 0, 0, 0, time.UTC))
}

// DateOf returns the calendar day of t in t's own location.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{Year: y, Month: m, Day: d}
}

// ParseDate parses exactly "YYYY-MM-DD". It rejects dates that do not exist,
// such as 2023-02-29.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return Date{}, fmt.Errorf("parse date %q: %w", s, err)
	}
	return DateOf(t), nil
}

// String returns "YYYY-MM-DD", or "" for the zero Date.
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return d.Time().Format(dateLayout)
}

// IsZero reports whether d is the zero Date.
func (d Date) IsZero() bool {
	return d == Date{}
}

// Time returns midnight UTC at the start of d.
func (d Date) Time() time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

// Before reports whether d is earlier than other.
func (d Date) Before(other Date) bool {
	return d.Time().Before(other.Time())
}

// After reports whether d is later than other.
func (d Date) After(other Date) bool {
	return d.Time().After(other.Time())
}

// Equal reports whether d and other are the same day.
func (d Date) Equal(other Date) bool {
	return d.Time().Equal(other.Time())
}

// AddDays returns the date n calendar days after d. A negative n goes back.
func (d Date) AddDays(n int) Date {
	return NewDate(d.Year, d.Month, d.Day+n)
}

// MarshalJSON writes "YYYY-MM-DD", or null for the zero Date.
func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(d.String())
}

// UnmarshalJSON reads "YYYY-MM-DD", or null as the zero Date.
func (d *Date) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*d = Date{}
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("date: want a \"YYYY-MM-DD\" string: %w", err)
	}
	parsed, err := ParseDate(s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// Value implements driver.Valuer: "YYYY-MM-DD", or NULL for the zero Date.
func (d Date) Value() (driver.Value, error) {
	if d.IsZero() {
		return nil, nil
	}
	return d.String(), nil
}

// Scan implements sql.Scanner. It accepts NULL (the zero Date), a time.Time
// (its calendar day in its own location), and "YYYY-MM-DD" as a string or
// bytes.
func (d *Date) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*d = Date{}
		return nil
	case time.Time:
		*d = DateOf(v)
		return nil
	case string:
		return d.scanString(v)
	case []byte:
		return d.scanString(string(v))
	default:
		return fmt.Errorf("scan date: unsupported type %T", src)
	}
}

func (d *Date) scanString(s string) error {
	parsed, err := ParseDate(s)
	if err != nil {
		return fmt.Errorf("scan date: %w", err)
	}
	*d = parsed
	return nil
}
