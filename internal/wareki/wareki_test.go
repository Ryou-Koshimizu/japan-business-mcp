package wareki

import (
	"strings"
	"testing"
	"time"

	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/jpdate"
)

func TestFromDate(t *testing.T) {
	cases := map[string]string{
		"1873-01-01": "明治6年1月1日",
		"1912-07-29": "明治45年7月29日",
		"1912-07-30": "大正元年7月30日",
		"1926-12-24": "大正15年12月24日",
		"1926-12-25": "昭和元年12月25日",
		"1989-01-07": "昭和64年1月7日",
		"1989-01-08": "平成元年1月8日",
		"2019-04-30": "平成31年4月30日",
		"2019-05-01": "令和元年5月1日",
		"2026-10-20": "令和8年10月20日",
		"2100-01-01": "令和82年1月1日",
	}
	for in, want := range cases {
		d, _ := jpdate.Parse(in)
		r, err := FromDate(d)
		if err != nil || r.Wareki != want {
			t.Errorf("%s: got %q %v want %q", in, r.Wareki, err, want)
		}
	}
	if _, err := FromDate(jpdate.MustNew(1872, time.December, 31)); err == nil {
		t.Error("太陽暦の採用前はエラー")
	}
}

func TestParseWareki(t *testing.T) {
	cases := map[string]string{
		"令和8年10月20日": "2026-10-20",
		"令和８年１０月２０日": "2026-10-20",
		"令和八年十月二十日":  "2026-10-20",
		"R8.10.20":   "2026-10-20",
		"r8/10/20":   "2026-10-20",
		"H31/4/30":   "2019-04-30",
		"平成元年1月8日":   "1989-01-08",
		"令和元年5月1日":   "2019-05-01",
		"S64.1.7":    "1989-01-07",
		"昭和元年12月25日": "1926-12-25",
		"明治6年1月1日":   "1873-01-01",
		"令 8.10.20":  "2026-10-20",
	}
	for in, want := range cases {
		r, err := ParseWareki(in)
		if err != nil || r.Date.String() != want {
			t.Errorf("%s: got %s %v want %s", in, r.Date, err, want)
		}
	}
	errs := map[string]string{
		"平成31年5月1日": "令和元年5月1日",
		"S64.1.8":   "平成元年1月8日",
		"昭和65年":     "昭和64年まで",
		"令和0年":      "元年",
		"令和8年2月30日": "存在しない",
		"大正元年7月29日": "明治45年7月29日",
	}
	for in, sub := range errs {
		_, err := ParseWareki(in)
		if err == nil || !strings.Contains(err.Error(), sub) {
			t.Errorf("%s: エラーに %q が含まれるはず: %v", in, sub, err)
		}
	}
}

func TestConvert(t *testing.T) {
	rs, err := Convert("1989")
	if err != nil || len(rs) != 2 || rs[0].Wareki != "昭和64年" || rs[1].Wareki != "平成元年" {
		t.Fatalf("%+v %v", rs, err)
	}
	if !strings.Contains(rs[0].Note, "改元") {
		t.Fatal("改元の注記が無い")
	}
	rs, err = Convert("2019年")
	if err != nil || len(rs) != 2 || rs[1].Wareki != "令和元年" {
		t.Fatalf("%+v %v", rs, err)
	}
	rs, err = Convert("令和元年")
	if err != nil || rs[0].Gregorian != "2019年" {
		t.Fatalf("%+v %v", rs, err)
	}
	rs, err = Convert("2026/10/20")
	if err != nil || rs[0].Wareki != "令和8年10月20日" || rs[0].WarekiAlpha != "R8.10.20" || rs[0].Weekday != "火" {
		t.Fatalf("%+v %v", rs, err)
	}
	for _, bad := range []string{"", "きのう", "1867", "令和"} {
		if _, err := Convert(bad); err == nil {
			t.Errorf("%q はエラー", bad)
		}
	}
}

// 1873年から2150年までの全日で、西暦→和暦→西暦が元に戻る。
func TestRoundTripAllDays(t *testing.T) {
	n := 0
	for d := SolarStart; d.Year() <= 2150; d = d.AddDays(1) {
		r, err := FromDate(d)
		if err != nil {
			t.Fatal(err)
		}
		back, err := ParseWareki(r.Wareki)
		if err != nil || !back.Date.Equal(d) {
			t.Fatalf("%s → %s → %s (%v)", d, r.Wareki, back.Date, err)
		}
		back2, err := ParseWareki(r.WarekiAlpha)
		if err != nil || !back2.Date.Equal(d) {
			t.Fatalf("%s → %s → %s (%v)", d, r.WarekiAlpha, back2.Date, err)
		}
		n++
	}
	t.Logf("%d日を往復", n)
}

func FuzzConvert(f *testing.F) {
	for _, s := range []string{"令和8年10月20日", "R8.10.20", "1989", "平成31年5月1日", "明治元年", "令和九十九年", "S"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		rs, err := Convert(s)
		if err != nil {
			return
		}
		for _, r := range rs {
			if r.Era == "" || r.EraYear < 1 {
				t.Fatalf("%q → %+v", s, r)
			}
		}
	})
}
