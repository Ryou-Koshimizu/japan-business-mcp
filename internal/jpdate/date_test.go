package jpdate

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	ok := map[string]string{
		"2026-10-20": "2026-10-20", "2026/10/20": "2026-10-20", "2026.10.20": "2026-10-20",
		"20261020": "2026-10-20", "2026年10月20日": "2026-10-20", "２０２６／１０／２０": "2026-10-20",
		"\t2026-1-5\n": "2026-01-05", "2024-02-29": "2024-02-29",
	}
	for in, want := range ok {
		d, err := Parse(in)
		if err != nil || d.String() != want {
			t.Errorf("%q: %s %v", in, d, err)
		}
	}
	for _, bad := range []string{"", "2026-02-29", "2026-13-01", "26-10-20", "2026-10", "abc", "2026-10-20-1", "R8.10.20"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q はエラー", bad)
		}
	}
}

func TestHelpers(t *testing.T) {
	d := MustNew(2026, time.January, 31)
	if got := d.AddMonthsClamp(1).String(); got != "2026-02-28" {
		t.Error(got)
	}
	if got := d.AddMonthsClamp(-2).String(); got != "2025-11-30" {
		t.Error(got)
	}
	if got := d.AddMonthsClamp(13).String(); got != "2027-02-28" {
		t.Error(got)
	}
	if got := d.AddMonthsClamp(-13).String(); got != "2024-12-31" {
		t.Error(got)
	}
	if got := NthWeekday(2026, time.January, 2, time.Monday).String(); got != "2026-01-12" {
		t.Error(got)
	}
	if LastDayOfMonth(2024, time.February) != 29 || LastDayOfMonth(2100, time.February) != 28 {
		t.Error("うるう年")
	}
	if MustNew(2026, 10, 1).DaysUntil(MustNew(2026, 10, 31)) != 30 {
		t.Error("DaysUntil")
	}
	var x Date
	if err := x.UnmarshalText([]byte("2026-10-20")); err != nil || x.String() != "2026-10-20" || WeekdayJA(x.Weekday()) != "火" {
		t.Error(x, err)
	}
	if _, err := New(0, 1, 1); err == nil {
		t.Error("0年はエラー")
	}
}
