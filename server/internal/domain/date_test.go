package domain_test

import (
	"database/sql/driver"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

func TestParseDate(t *testing.T) {
	tests := []struct {
		in   string
		want domain.Date
	}{
		{"2024-06-10", domain.Date{Year: 2024, Month: time.June, Day: 10}},
		{"2024-02-29", domain.Date{Year: 2024, Month: time.February, Day: 29}},
		{"2000-02-29", domain.Date{Year: 2000, Month: time.February, Day: 29}},
		{"1999-12-31", domain.Date{Year: 1999, Month: time.December, Day: 31}},
	}
	for _, tt := range tests {
		got, err := domain.ParseDate(tt.in)
		if err != nil {
			t.Errorf("ParseDate(%q) error = %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseDate(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

func TestParseDateRejectsInvalidInput(t *testing.T) {
	for _, in := range []string{
		"", "2023-02-29", "1900-02-29", "2024-13-01", "2024-00-10", "2024-04-31",
		"2024-6-10", "24-06-10", "2024/06/10", "2024-06-10T00:00:00Z", "2024-06-10 ",
	} {
		if got, err := domain.ParseDate(in); err == nil {
			t.Errorf("ParseDate(%q) = %v, want an error", in, got)
		}
	}
}

func TestNewDateNormalizesOverflow(t *testing.T) {
	if got, want := domain.NewDate(2023, time.February, 29), domain.NewDate(2023, time.March, 1); got != want {
		t.Errorf("NewDate(2023, February, 29) = %v, want %v", got, want)
	}
}

func TestDateString(t *testing.T) {
	if got := domain.NewDate(987, time.March, 5).String(); got != "0987-03-05" {
		t.Errorf("String() = %q, want %q", got, "0987-03-05")
	}
	if got := (domain.Date{}).String(); got != "" {
		t.Errorf("zero Date String() = %q, want empty", got)
	}
}

func TestDateOfUsesTheCalendarDayOfTheTimeZone(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("time zone data not available: %v", err)
	}
	// 2024-06-10 22:30 in New York is already 2024-06-11 in UTC.
	tm := time.Date(2024, time.June, 10, 22, 30, 0, 0, newYork)
	if got, want := domain.DateOf(tm), domain.NewDate(2024, time.June, 10); got != want {
		t.Errorf("DateOf(%v) = %v, want %v", tm, got, want)
	}
}

func TestDateTimeIsUTCMidnight(t *testing.T) {
	got := domain.NewDate(2024, time.June, 10).Time()
	want := time.Date(2024, time.June, 10, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) || got.Location() != time.UTC {
		t.Errorf("Time() = %v, want %v", got, want)
	}
}

func TestDateComparisons(t *testing.T) {
	a := domain.NewDate(2024, time.February, 28)
	b := domain.NewDate(2024, time.February, 29)
	if !a.Before(b) || a.After(b) || a.Equal(b) {
		t.Errorf("%v compared with %v: Before=%t After=%t Equal=%t", a, b, a.Before(b), a.After(b), a.Equal(b))
	}
	if !b.After(a) || b.Before(a) {
		t.Errorf("%v compared with %v: After=%t Before=%t", b, a, b.After(a), b.Before(a))
	}
	if !a.Equal(domain.NewDate(2024, time.February, 28)) {
		t.Errorf("%v.Equal(same date) = false", a)
	}
}

func TestDateAddDays(t *testing.T) {
	tests := []struct {
		from domain.Date
		days int
		want domain.Date
	}{
		{domain.NewDate(2024, time.February, 28), 1, domain.NewDate(2024, time.February, 29)},
		{domain.NewDate(2023, time.February, 28), 1, domain.NewDate(2023, time.March, 1)},
		{domain.NewDate(2024, time.December, 31), 1, domain.NewDate(2025, time.January, 1)},
		{domain.NewDate(2024, time.March, 1), -1, domain.NewDate(2024, time.February, 29)},
		{domain.NewDate(2024, time.June, 10), 0, domain.NewDate(2024, time.June, 10)},
		{domain.NewDate(2024, time.January, 1), 366, domain.NewDate(2025, time.January, 1)},
	}
	for _, tt := range tests {
		if got := tt.from.AddDays(tt.days); got != tt.want {
			t.Errorf("%v.AddDays(%d) = %v, want %v", tt.from, tt.days, got, tt.want)
		}
	}
}

func TestDateJSONRoundTrip(t *testing.T) {
	type doc struct {
		Filed  domain.Date  `json:"filed"`
		Report *domain.Date `json:"report"`
	}
	report := domain.NewDate(2024, time.April, 28)
	in := doc{Filed: domain.NewDate(2024, time.May, 29), Report: &report}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if got, want := string(data), `{"filed":"2024-05-29","report":"2024-04-28"}`; got != want {
		t.Errorf("Marshal() = %s, want %s", got, want)
	}

	var out doc
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if diff := cmp.Diff(in, out); diff != "" {
		t.Errorf("round trip mismatch (-want +got):\n%s", diff)
	}
}

func TestDateJSONNull(t *testing.T) {
	data, err := json.Marshal(domain.Date{})
	if err != nil {
		t.Fatalf("Marshal(zero) error = %v", err)
	}
	if string(data) != "null" {
		t.Errorf("Marshal(zero) = %s, want null", data)
	}
	d := domain.NewDate(2024, time.June, 10)
	if err := json.Unmarshal([]byte("null"), &d); err != nil {
		t.Fatalf("Unmarshal(null) error = %v", err)
	}
	if !d.IsZero() {
		t.Errorf("Unmarshal(null) = %v, want the zero Date", d)
	}
}

func TestDateJSONRejectsInvalidInput(t *testing.T) {
	for _, in := range []string{`"2024-02-30"`, `"2024-06-10T00:00:00Z"`, `20240610`, `""`} {
		var d domain.Date
		if err := json.Unmarshal([]byte(in), &d); err == nil {
			t.Errorf("Unmarshal(%s) = %v, want an error", in, d)
		}
	}
}

func TestDateSQLRoundTrip(t *testing.T) {
	d := domain.NewDate(2024, time.June, 10)
	v, err := d.Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}
	if v != "2024-06-10" {
		t.Errorf("Value() = %#v, want %q", v, "2024-06-10")
	}

	var got domain.Date
	if err := got.Scan(v); err != nil {
		t.Fatalf("Scan(%#v) error = %v", v, err)
	}
	if got != d {
		t.Errorf("Scan(Value()) = %v, want %v", got, d)
	}
}

func TestDateValueOfZeroIsNull(t *testing.T) {
	v, err := domain.Date{}.Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}
	if v != nil {
		t.Errorf("Value() = %#v, want nil", v)
	}
}

func TestDateScan(t *testing.T) {
	newYork := time.FixedZone("EST", -5*60*60)
	tests := []struct {
		name string
		src  any
		want domain.Date
	}{
		{"null", nil, domain.Date{}},
		{"string", "2024-06-10", domain.NewDate(2024, time.June, 10)},
		{"bytes", []byte("2024-06-10"), domain.NewDate(2024, time.June, 10)},
		{"time at UTC midnight", time.Date(2024, time.June, 10, 0, 0, 0, 0, time.UTC), domain.NewDate(2024, time.June, 10)},
		{"time in another zone", time.Date(2024, time.June, 10, 23, 0, 0, 0, newYork), domain.NewDate(2024, time.June, 10)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.NewDate(1999, time.January, 1)
			if err := got.Scan(tt.src); err != nil {
				t.Fatalf("Scan(%#v) error = %v", tt.src, err)
			}
			if got != tt.want {
				t.Errorf("Scan(%#v) = %v, want %v", tt.src, got, tt.want)
			}
		})
	}
}

func TestDateScanRejectsInvalidInput(t *testing.T) {
	for _, src := range []any{"2024-02-30", int64(20240610), 3.5} {
		var d domain.Date
		if err := d.Scan(src); err == nil {
			t.Errorf("Scan(%#v) = %v, want an error", src, d)
		}
	}
}

// The compile-time checks that pgx and database/sql rely on.
var (
	_ driver.Valuer    = domain.Date{}
	_ json.Marshaler   = domain.Date{}
	_ json.Unmarshaler = (*domain.Date)(nil)
)
