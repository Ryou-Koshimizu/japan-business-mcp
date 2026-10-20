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
	if got := must(d.AddMonthsClamp(1)).String(); got != "2026-02-28" {
		t.Error(got)
	}
	if got := must(d.AddMonthsClamp(-2)).String(); got != "2025-11-30" {
		t.Error(got)
	}
	if got := must(d.AddMonthsClamp(13)).String(); got != "2027-02-28" {
		t.Error(got)
	}
	if got := must(d.AddMonthsClamp(-13)).String(); got != "2024-12-31" {
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

func must(d Date, err error) Date {
	if err != nil {
		panic(err)
	}
	return d
}

// 範囲の外へ出る月の計算は panic せずエラーを返す（セキュリティ指摘の回帰試験）。
func TestAddMonthsClampOutOfRange(t *testing.T) {
	for _, c := range []struct {
		d Date
		n int
	}{
		{MustNew(9999, time.December, 1), 1},
		{MustNew(1, time.January, 1), -1},
		{MustNew(2026, time.October, 1), 12 * 10000},
		{MustNew(2026, time.October, 1), -12 * 10001},
		{Date{}, 1},
	} {
		if _, err := c.d.AddMonthsClamp(c.n); err == nil {
			t.Errorf("%s %+d はエラーになるはず", c.d, c.n)
		}
	}
	if got := must(MustNew(9999, time.November, 30).AddMonthsClamp(1)); got.String() != "9999-12-30" {
		t.Error(got)
	}
}

func FuzzAddMonthsClamp(f *testing.F) {
	f.Add(9999, 12, 31, 1)
	f.Add(1, 1, 1, -1)
	f.Add(2026, 1, 31, 1)
	f.Add(2026, 10, 20, -24312)
	f.Fuzz(func(t *testing.T, y, m, d, n int) {
		dt, err := New(y, time.Month(m), d)
		if err != nil {
			return
		}
		r, err := dt.AddMonthsClamp(n)
		if err != nil {
			return
		}
		if r.Year() < 1 || r.Year() > 9999 || r.Day() > d {
			t.Fatalf("%s %+d → %s", dt, n, r)
		}
	})
}
