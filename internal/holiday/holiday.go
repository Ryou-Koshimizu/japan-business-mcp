// Package holiday は「国民の祝日に関する法律」（昭和23年法律第178号）と
// その特例法の規則から、日本の祝日・休日を計算する。
//
// 表を持たずに規則で計算する：
//   - 固定日の祝日（元日、建国記念の日、昭和の日 など）と、その制定・改正の年
//   - ハッピーマンデー（成人の日・海の日・敬老の日・体育の日／スポーツの日）
//   - 春分の日・秋分の日（天文計算の近似式）
//   - 振替休日（1973年4月12日から。2007年からは「その後で最も近い祝日でない日」）
//   - 国民の休日（1985年12月27日から。祝日に挟まれた平日）
//   - 東京オリンピック・パラリンピックの特措法による2020・2021年の移動
//   - 皇室の慶弔の日（結婚の儀・大喪の礼・即位の日・即位礼正殿の儀）
package holiday

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/jpdate"
)

// Kind は休日の種類。
type Kind string

const (
	// National は「国民の祝日」（法第2条）。
	National Kind = "national"
	// Substitute は振替休日（法第3条第2項）。
	Substitute Kind = "substitute"
	// Citizens は「国民の休日」（法第3条第3項）。
	Citizens Kind = "citizens"
	// Special は皇室の慶弔に伴い特別法で定めた休日。
	Special Kind = "special"
)

// KindLabel は種類の日本語名。
func KindLabel(k Kind) string {
	switch k {
	case National:
		return "国民の祝日"
	case Substitute:
		return "振替休日"
	case Citizens:
		return "国民の休日"
	case Special:
		return "特別法による休日"
	}
	return string(k)
}

// Holiday は1日分の祝日・休日。
type Holiday struct {
	Date jpdate.Date `json:"date"`
	Name string      `json:"name"`
	Kind Kind        `json:"kind"`
}

// 対応する年の範囲。春分・秋分の近似式が使える範囲（〜2150年）と、
// 法の施行後最初の通年（1949年）で区切る。
const (
	MinYear = 1949
	MaxYear = 2150
)

// LastGazettedYear は、春分の日・秋分の日が官報（暦要項）で公表済みの最後の年。
// 暦要項は毎年2月に翌年分が公表される。これより後の年の春分・秋分は計算式による見込み。
const LastGazettedYear = 2027

// ErrOutOfRange は対応範囲外の年。
type ErrOutOfRange struct{ Year int }

func (e ErrOutOfRange) Error() string {
	return fmt.Sprintf("%d年は対応範囲（%d〜%d年）の外です", e.Year, MinYear, MaxYear)
}

var (
	// 振替休日の規定の施行日（昭和48年法律第10号）。
	substituteStart = jpdate.MustNew(1973, time.April, 12)
	// 振替休日の規定が「最も近い祝日でない日」に改められた日（平成17年法律第43号）。
	law2007 = jpdate.MustNew(2007, time.January, 1)
	// 国民の休日の規定の施行日（昭和60年法律第103号）。
	citizensStart = jpdate.MustNew(1985, time.December, 27)
)

// special は特別法による休日。asNational は「国民の祝日とみなす」規定の有無
// （2019年5月1日の天皇の即位の日だけが該当し、前後の4月30日・5月2日が国民の休日になった）。
var special = []struct {
	date       jpdate.Date
	name       string
	asNational bool
}{
	{jpdate.MustNew(1959, time.April, 10), "皇太子明仁親王の結婚の儀", false},
	{jpdate.MustNew(1989, time.February, 24), "昭和天皇の大喪の礼", false},
	{jpdate.MustNew(1990, time.November, 12), "即位礼正殿の儀", false},
	{jpdate.MustNew(1993, time.June, 9), "皇太子徳仁親王の結婚の儀", false},
	{jpdate.MustNew(2019, time.May, 1), "天皇の即位の日", true},
	{jpdate.MustNew(2019, time.October, 22), "即位礼正殿の儀", true},
}

// equinoxDay は春分（spring=true）・秋分の日付（3月または9月の日）。
// 国立天文台の暦計算に基づく広く使われる近似式で、1900〜2150年で用いる。
func equinoxDay(y int, spring bool) int {
	var base float64
	switch {
	case y <= 1979:
		if spring {
			base = 20.8357
		} else {
			base = 23.2588
		}
		// 1980年より前は (Y-1983)/4 を 0 方向に切り捨てる（元の式の int() のとおり）。
		return int(math.Floor(base+0.242194*float64(y-1980))) - (y-1983)/4
	case y <= 2099:
		if spring {
			base = 20.8431
		} else {
			base = 23.2488
		}
	default:
		if spring {
			base = 21.8510
		} else {
			base = 24.2488
		}
	}
	return int(math.Floor(base+0.242194*float64(y-1980))) - floorDiv(y-1980, 4)
}

func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// nationalHolidays は法第2条の「国民の祝日」と特別法の休日を返す（振替・国民の休日を除く）。
func nationalHolidays(y int) []Holiday {
	var hs []Holiday
	add := func(m time.Month, d int, name string) {
		hs = append(hs, Holiday{Date: jpdate.MustNew(y, m, d), Name: name, Kind: National})
	}
	addDate := func(dt jpdate.Date, name string) {
		hs = append(hs, Holiday{Date: dt, Name: name, Kind: National})
	}

	add(time.January, 1, "元日")

	if y <= 1999 {
		add(time.January, 15, "成人の日")
	} else {
		addDate(jpdate.NthWeekday(y, time.January, 2, time.Monday), "成人の日")
	}

	if y >= 1967 {
		add(time.February, 11, "建国記念の日")
	}

	switch {
	case y >= 2020:
		add(time.February, 23, "天皇誕生日")
	case y >= 1989 && y <= 2018:
		add(time.December, 23, "天皇誕生日")
	case y <= 1988:
		add(time.April, 29, "天皇誕生日")
	}

	add(time.March, equinoxDay(y, true), "春分の日")

	switch {
	case y >= 2007:
		add(time.April, 29, "昭和の日")
	case y >= 1989:
		add(time.April, 29, "みどりの日")
	}

	add(time.May, 3, "憲法記念日")
	if y >= 2007 {
		add(time.May, 4, "みどりの日")
	}
	add(time.May, 5, "こどもの日")

	switch {
	case y == 2020:
		add(time.July, 23, "海の日")
	case y == 2021:
		add(time.July, 22, "海の日")
	case y >= 2003:
		addDate(jpdate.NthWeekday(y, time.July, 3, time.Monday), "海の日")
	case y >= 1996:
		add(time.July, 20, "海の日")
	}

	switch {
	case y == 2020:
		add(time.August, 10, "山の日")
	case y == 2021:
		add(time.August, 8, "山の日")
	case y >= 2016:
		add(time.August, 11, "山の日")
	}

	switch {
	case y >= 2003:
		addDate(jpdate.NthWeekday(y, time.September, 3, time.Monday), "敬老の日")
	case y >= 1966:
		add(time.September, 15, "敬老の日")
	}

	add(time.September, equinoxDay(y, false), "秋分の日")

	switch {
	case y == 2020:
		add(time.July, 24, "スポーツの日")
	case y == 2021:
		add(time.July, 23, "スポーツの日")
	case y >= 2020:
		addDate(jpdate.NthWeekday(y, time.October, 2, time.Monday), "スポーツの日")
	case y >= 2000:
		addDate(jpdate.NthWeekday(y, time.October, 2, time.Monday), "体育の日")
	case y >= 1966:
		add(time.October, 10, "体育の日")
	}

	add(time.November, 3, "文化の日")
	add(time.November, 23, "勤労感謝の日")

	for _, s := range special {
		if s.date.Year() == y {
			hs = append(hs, Holiday{Date: s.date, Name: s.name, Kind: Special})
		}
	}
	return hs
}

// isSpecialAsNational は特別法の休日のうち「国民の祝日とみなす」もの。
func isSpecialAsNational(d jpdate.Date) bool {
	for _, s := range special {
		if s.asNational && s.date.Equal(d) {
			return true
		}
	}
	return false
}

var cache sync.Map // int -> []Holiday

// Year は y年のすべての祝日・休日を日付順に返す。戻り値は呼び出し側で書き換えてよい（複製を返す）。
func Year(y int) ([]Holiday, error) {
	if y < MinYear || y > MaxYear {
		return nil, ErrOutOfRange{Year: y}
	}
	if v, ok := cache.Load(y); ok {
		return append([]Holiday(nil), v.([]Holiday)...), nil
	}
	hs := compute(y)
	cache.Store(y, hs)
	return append([]Holiday(nil), hs...), nil
}

func compute(y int) []Holiday {
	base := nationalHolidays(y)
	// 年をまたぐ振替（12月31日が日曜の祝日、のような日）は現行法では起きないが、
	// 前後の年の祝日も見て判定できるよう、前後年の分も索引に入れる。
	idx := map[jpdate.Date]Holiday{}
	for _, yy := range []int{y - 1, y, y + 1} {
		if yy < MinYear-1 || yy > MaxYear+1 {
			continue
		}
		var hs []Holiday
		if yy == y {
			hs = base
		} else {
			hs = nationalHolidays(yy)
		}
		for _, h := range hs {
			idx[h.Date] = h
		}
	}
	// 振替・国民の休日の判定に使う「国民の祝日」。特別法の休日は、みなし規定のあるものだけ含める。
	isNational := func(d jpdate.Date) bool {
		h, ok := idx[d]
		if !ok {
			return false
		}
		if h.Kind == Special {
			return isSpecialAsNational(d)
		}
		return true
	}

	out := append([]Holiday(nil), base...)
	subs := map[jpdate.Date]bool{}

	// 振替休日（法第3条第2項）
	for _, h := range base {
		if h.Date.Weekday() != time.Sunday || h.Date.Before(substituteStart) || !isNational(h.Date) {
			continue
		}
		next := h.Date.AddDays(1)
		if h.Date.Before(law2007) {
			// 旧規定：「その翌日を休日とする」。翌日が祝日ならそれ以上は動かない。
			if isNational(next) {
				continue
			}
		} else {
			// 現行規定：「その日後においてその日に最も近い国民の祝日でない日」
			for isNational(next) {
				next = next.AddDays(1)
			}
		}
		if next.Year() != y {
			continue
		}
		if _, dup := idx[next]; dup {
			continue
		}
		subs[next] = true
		out = append(out, Holiday{Date: next, Name: "振替休日", Kind: Substitute})
	}

	// 国民の休日（法第3条第3項）
	for d := jpdate.MustNew(y, time.January, 2); d.Year() == y; d = d.AddDays(1) {
		if d.Before(citizensStart) {
			continue
		}
		if _, ok := idx[d]; ok {
			continue
		}
		if subs[d] {
			continue
		}
		if !isNational(d.AddDays(-1)) || !isNational(d.AddDays(1)) {
			continue
		}
		if d.Before(law2007) && d.Weekday() == time.Sunday {
			// 旧規定は日曜日を除いていた
			continue
		}
		out = append(out, Holiday{Date: d, Name: "国民の休日", Kind: Citizens})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out
}

// Lookup はその日が祝日・休日なら返す。
func Lookup(d jpdate.Date) (Holiday, bool, error) {
	hs, err := Year(d.Year())
	if err != nil {
		return Holiday{}, false, err
	}
	for _, h := range hs {
		if h.Date.Equal(d) {
			return h, true, nil
		}
	}
	return Holiday{}, false, nil
}

// IsEstimate は、その日付の祝日が「計算式による見込み」かどうか
// （春分・秋分が官報で未公表の年）。
func IsEstimate(y int) bool { return y > LastGazettedYear }
