// Package wareki は和暦（明治・大正・昭和・平成・令和）と西暦を相互に変換する。
//
// 改元の日（元号法と各改元の政令）：
//
//	明治 1868-10-23（太陽暦の採用は明治6年 = 1873-01-01）
//	大正 1912-07-30
//	昭和 1926-12-25
//	平成 1989-01-08
//	令和 2019-05-01
//
// 日付の変換は太陽暦を採用した 1873-01-01（明治6年1月1日）以降を扱う。
// それより前の明治の日付は旧暦のため、年だけの変換（明治元年=1868年）に対応する。
package wareki

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/jpdate"
	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/textnorm"
)

// Era は元号。
type Era struct {
	Name  string      `json:"name"`
	Alpha string      `json:"alphabet"`
	Start jpdate.Date `json:"start"`
	End   jpdate.Date `json:"end,omitempty"` // 最後の日。令和はゼロ値
}

// Eras は古い順。
var Eras = []Era{
	{"明治", "M", jpdate.MustNew(1868, time.October, 23), jpdate.MustNew(1912, time.July, 29)},
	{"大正", "T", jpdate.MustNew(1912, time.July, 30), jpdate.MustNew(1926, time.December, 24)},
	{"昭和", "S", jpdate.MustNew(1926, time.December, 25), jpdate.MustNew(1989, time.January, 7)},
	{"平成", "H", jpdate.MustNew(1989, time.January, 8), jpdate.MustNew(2019, time.April, 30)},
	{"令和", "R", jpdate.MustNew(2019, time.May, 1), jpdate.Date{}},
}

// SolarStart は太陽暦の採用日。これより前の日付は変換しない。
var SolarStart = jpdate.MustNew(1873, time.January, 1)

// Result は変換結果。
type Result struct {
	Input       string      `json:"input"`
	Date        jpdate.Date `json:"date,omitempty"`
	Gregorian   string      `json:"gregorian"`
	Era         string      `json:"era"`
	EraYear     int         `json:"era_year"`
	Wareki      string      `json:"wareki"`
	WarekiAlpha string      `json:"wareki_alphabet"`
	Weekday     string      `json:"weekday,omitempty"`
	YearOnly    bool        `json:"year_only,omitempty"`
	Note        string      `json:"note,omitempty"`
}

func yearLabel(n int) string {
	if n == 1 {
		return "元"
	}
	return strconv.Itoa(n)
}

// FromDate は西暦の日付を和暦にする。
func FromDate(d jpdate.Date) (Result, error) {
	if d.Before(SolarStart) {
		return Result{}, fmt.Errorf("%s は太陽暦の採用（1873年1月1日）より前のため、日付の変換はできません。年だけなら %d年 で変換できます", d, d.Year())
	}
	for i := len(Eras) - 1; i >= 0; i-- {
		e := Eras[i]
		if !d.Before(e.Start) {
			y := d.Year() - e.Start.Year() + 1
			return Result{
				Date:        d,
				Gregorian:   fmt.Sprintf("%d年%d月%d日", d.Year(), d.Month(), d.Day()),
				Era:         e.Name,
				EraYear:     y,
				Wareki:      fmt.Sprintf("%s%s年%d月%d日", e.Name, yearLabel(y), d.Month(), d.Day()),
				WarekiAlpha: fmt.Sprintf("%s%d.%d.%d", e.Alpha, y, d.Month(), d.Day()),
				Weekday:     jpdate.WeekdayJA(d.Weekday()),
			}, nil
		}
	}
	return Result{}, fmt.Errorf("%s に対応する元号がありません", d)
}

// FromYear は西暦の年を和暦の年にする。改元の年は2つの元号を返す。
func FromYear(y int) ([]Result, error) {
	if y < 1868 || y > 9999 {
		return nil, fmt.Errorf("%d年は明治元年（1868年）より前か、範囲の外です", y)
	}
	var out []Result
	for _, e := range Eras {
		endY := 9999
		if !e.End.IsZero() {
			endY = e.End.Year()
		}
		if y >= e.Start.Year() && y <= endY {
			n := y - e.Start.Year() + 1
			out = append(out, Result{
				Gregorian:   fmt.Sprintf("%d年", y),
				Era:         e.Name,
				EraYear:     n,
				Wareki:      fmt.Sprintf("%s%s年", e.Name, yearLabel(n)),
				WarekiAlpha: fmt.Sprintf("%s%d", e.Alpha, n),
				YearOnly:    true,
			})
		}
	}
	if len(out) == 2 {
		note := fmt.Sprintf("%d年は改元の年です（%sは%d月%d日まで、%sは%d月%d日から）",
			y, out[0].Era, eraByName(out[0].Era).End.Month(), eraByName(out[0].Era).End.Day(),
			out[1].Era, eraByName(out[1].Era).Start.Month(), eraByName(out[1].Era).Start.Day())
		for i := range out {
			out[i].Note = note
		}
	}
	return out, nil
}

func eraByName(name string) Era {
	for _, e := range Eras {
		if e.Name == name {
			return e
		}
	}
	return Era{}
}

var (
	reEra = regexp.MustCompile(`^(明治|大正|昭和|平成|令和|明|大|昭|平|令|[MTSHRmtshr])\.?\s*(元|\d{1,3})\s*(?:年|[./\-])?\s*(?:(\d{1,2})\s*(?:月|[./\-])\s*(\d{1,2})\s*日?)?$`)
)

var alphaToEra = map[string]string{
	"M": "明治", "T": "大正", "S": "昭和", "H": "平成", "R": "令和",
	"明": "明治", "大": "大正", "昭": "昭和", "平": "平成", "令": "令和",
}

// ParseWareki は「令和8年10月20日」「R8.10.20」「令和元年」「H31/4/30」「令和八年十月二十日」などを読む。
// 日付が元号の期間の外（平成31年5月1日 など）のときは、正しい元号を添えてエラーにする。
func ParseWareki(s string) (Result, error) {
	in := s
	s = normalizeInput(s)
	m := reEra.FindStringSubmatch(s)
	if m == nil {
		return Result{}, fmt.Errorf("和暦 %q を読めません（例: 令和8年10月20日、R8.10.20、令和元年）", in)
	}
	name := m[1]
	if full, ok := alphaToEra[strings.ToUpper(name)]; ok {
		name = full
	}
	e := eraByName(name)
	n := 1
	if m[2] != "元" {
		n, _ = strconv.Atoi(m[2])
	}
	if n < 1 {
		return Result{}, fmt.Errorf("%s0年はありません（1年目は元年）", name)
	}
	y := e.Start.Year() + n - 1
	if !e.End.IsZero() && y > e.End.Year() {
		return Result{}, fmt.Errorf("%s%d年はありません（%sは%d年＝%s%d年まで）", name, n, name, e.End.Year(), name, e.End.Year()-e.Start.Year()+1)
	}
	if m[3] == "" {
		rs, err := FromYear(y)
		if err != nil {
			return Result{}, err
		}
		for _, r := range rs {
			if r.Era == name {
				r.Input = in
				r.Gregorian = fmt.Sprintf("%d年", y)
				return r, nil
			}
		}
	}
	mo, _ := strconv.Atoi(m[3])
	da, _ := strconv.Atoi(m[4])
	d, err := jpdate.New(y, time.Month(mo), da)
	if err != nil {
		return Result{}, err
	}
	if d.Before(e.Start) || (!e.End.IsZero() && d.After(e.End)) {
		correct, err := FromDate(d)
		if err != nil {
			return Result{}, fmt.Errorf("%sの期間（%s〜）の外の日付です", name, e.Start)
		}
		return Result{}, fmt.Errorf("%s%s年%d月%d日 は%sの期間の外です。%s は %s です", name, yearLabel(n), mo, da, name, d, correct.Wareki)
	}
	r, err := FromDate(d)
	if err != nil {
		return Result{}, err
	}
	r.Input = in
	return r, nil
}

// Convert は入力を見て、和暦→西暦か西暦→和暦かを自動で判断する。
func Convert(s string) ([]Result, error) {
	norm := normalizeInput(s)
	if norm == "" {
		return nil, fmt.Errorf("入力が空です")
	}
	if reEra.MatchString(norm) {
		r, err := ParseWareki(s)
		if err != nil {
			return nil, err
		}
		return []Result{r}, nil
	}
	if d, err := jpdate.Parse(norm); err == nil {
		r, err := FromDate(d)
		if err != nil {
			return nil, err
		}
		r.Input = s
		return []Result{r}, nil
	}
	ys := strings.TrimSuffix(norm, "年")
	if y, err := strconv.Atoi(ys); err == nil && len(ys) == 4 {
		rs, err := FromYear(y)
		if err != nil {
			return nil, err
		}
		for i := range rs {
			rs[i].Input = s
		}
		return rs, nil
	}
	return nil, fmt.Errorf("%q を日付として読めません（例: 2026-10-20、2026年、令和8年10月20日、R8.10.20）", s)
}

// normalizeInput は全角を半角に、漢数字を算用数字にする。
func normalizeInput(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= '０' && r <= '９':
			return r - '０' + '0'
		case r >= 'Ａ' && r <= 'Ｚ':
			return r - 'Ａ' + 'A'
		case r >= 'ａ' && r <= 'ｚ':
			return r - 'ａ' + 'a'
		case r == '．':
			return '.'
		case r == '／':
			return '/'
		case r == '－' || r == '−' || r == '‐':
			return '-'
		case r == '　':
			return ' '
		}
		return r
	}, s)
	return kanjiNumbersToDigits(s)
}

var reKanjiNum = regexp.MustCompile(`[〇一二三四五六七八九十]+`)

func kanjiNumbersToDigits(s string) string {
	return reKanjiNum.ReplaceAllStringFunc(s, func(k string) string {
		if v, ok := textnorm.KanjiToInt(k); ok {
			return strconv.Itoa(v)
		}
		return k
	})
}
