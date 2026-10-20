package holiday

import (
	"testing"
	"time"

	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/jpdate"
)

func d(s string) jpdate.Date {
	v, err := jpdate.Parse(s)
	if err != nil {
		panic(err)
	}
	return v
}

func newYearOff() *Calendar {
	var a []MonthDay
	for _, s := range []string{"12-29", "12-30", "12-31", "01-02", "01-03"} {
		md, err := ParseMonthDay(s)
		if err != nil {
			panic(err)
		}
		a = append(a, md)
	}
	return &Calendar{Annual: a}
}

func TestCheck(t *testing.T) {
	cal := newYearOff()
	cases := []struct {
		date   string
		biz    bool
		reason string
	}{
		{"2026-10-20", true, "営業日"},
		{"2026-10-24", false, "土曜日"},
		{"2026-10-25", false, "日曜日"},
		{"2026-10-12", false, "スポーツの日（国民の祝日）"},
		{"2026-09-22", false, "国民の休日"},
		{"2026-05-06", false, "振替休日"},
		{"2026-12-29", false, "会社の休み（毎年 12月29日）"},
		{"2027-01-04", true, "営業日"},
	}
	for _, c := range cases {
		st, err := cal.Check(d(c.date))
		if err != nil {
			t.Fatal(err)
		}
		if st.Business != c.biz || st.Reason != c.reason {
			t.Errorf("%s: got %v %q want %v %q", c.date, st.Business, st.Reason, c.biz, c.reason)
		}
	}
	sat := &Calendar{WorkSaturday: true, Open: map[jpdate.Date]bool{d("2026-10-25"): true}, Closed: map[jpdate.Date]string{d("2026-10-21"): "創立記念日"}}
	for date, want := range map[string]bool{"2026-10-24": true, "2026-10-25": true, "2026-10-21": false, "2026-11-01": false} {
		if ok, _ := sat.IsBusinessDay(d(date)); ok != want {
			t.Errorf("土曜営業 %s: %v", date, ok)
		}
	}
	if st, _ := (&Calendar{WorkOnHolidays: true}).Check(d("2026-11-03")); !st.Business {
		t.Error("祝日営業の設定で文化の日（火）は営業日")
	}
}

func TestAddBusinessDays(t *testing.T) {
	cal := newYearOff()
	cases := []struct {
		from string
		n    int
		want string
	}{
		{"2026-12-28", 1, "2027-01-04"},
		{"2027-01-04", -1, "2026-12-28"},
		{"2026-10-20", 5, "2026-10-27"},
		{"2026-10-09", 1, "2026-10-13"}, // 10/12 スポーツの日を飛ばす
		{"2026-09-18", 1, "2026-09-24"}, // シルバーウィーク
		{"2026-10-24", 0, "2026-10-26"}, // 土曜の 0 営業日後 = 次の営業日
		{"2026-10-20", 0, "2026-10-20"},
		{"2026-04-30", 3, "2026-05-08"}, // ゴールデンウィーク
	}
	for _, c := range cases {
		got, err := cal.AddBusinessDays(d(c.from), c.n)
		if err != nil || got.String() != c.want {
			t.Errorf("%s %+d: got %s %v want %s", c.from, c.n, got, err, c.want)
		}
	}
	if _, err := cal.AddBusinessDays(d("2026-10-20"), 40000); err == nil {
		t.Error("大きすぎる n はエラー")
	}
	allOff := &Calendar{}
	for m := 1; m <= 12; m++ {
		for day := 1; day <= 31; day++ {
			if day <= jpdate.LastDayOfMonth(2024, time.Month(m)) {
				allOff.Annual = append(allOff.Annual, MonthDay{time.Month(m), day})
			}
		}
	}
	if _, err := allOff.AddBusinessDays(d("2026-10-20"), 1); err == nil {
		t.Error("営業日が無い設定はエラーで止まる")
	}
}

// AddBusinessDays と Count が食い違わないこと（n 営業日後までの営業日数は n+起点）。
func TestAddAndCountAgree(t *testing.T) {
	cal := newYearOff()
	for from := d("2026-01-01"); from.Year() == 2026; from = from.AddDays(3) {
		for _, n := range []int{1, 7, 30} {
			to, err := cal.AddBusinessDays(from, n)
			if err != nil {
				t.Fatal(err)
			}
			c, err := cal.Count(from.AddDays(1), to)
			if err != nil {
				t.Fatal(err)
			}
			if c.BusinessDays != n {
				t.Fatalf("%s +%d → %s だが間の営業日は %d", from, n, to, c.BusinessDays)
			}
		}
	}
}

func TestCount(t *testing.T) {
	r, err := (&Calendar{}).Count(d("2026-10-01"), d("2026-10-31"))
	if err != nil {
		t.Fatal(err)
	}
	if r.CalendarDays != 31 || r.BusinessDays != 21 || r.Weekends != 9 || len(r.Holidays) != 1 {
		t.Fatalf("%+v", r)
	}
	r, err = newYearOff().Count(d("2026-12-25"), d("2027-01-05"))
	if err != nil {
		t.Fatal(err)
	}
	// 営業日は 12/25(金) 12/28(月) 1/4(月) 1/5(火)。会社の休みは 12/29〜31・1/2・1/3、1/1 は元日
	if r.BusinessDays != 4 || r.CompanyOff != 5 || r.Weekends != 2 || len(r.Holidays) != 1 {
		t.Fatalf("%+v", r)
	}
	if _, err := (&Calendar{}).Count(d("2026-10-31"), d("2026-10-01")); err == nil {
		t.Fatal("逆順はエラー")
	}
}

func TestParseMonthDay(t *testing.T) {
	for _, s := range []string{"12-29", "12/29", "1229", "12月29日"} {
		if md, err := ParseMonthDay(s); err != nil || md.Month != 12 || md.Day != 29 {
			t.Errorf("%s: %+v %v", s, md, err)
		}
	}
	for _, s := range []string{"13-01", "02-30", "abc", ""} {
		if _, err := ParseMonthDay(s); err == nil {
			t.Errorf("%s はエラー", s)
		}
	}
	if _, err := ParseMonthDay("02-29"); err != nil {
		t.Error("2/29 は毎年の休みとして受け付ける（うるう年だけ効く）")
	}
}
