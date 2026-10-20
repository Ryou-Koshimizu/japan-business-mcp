// Package jpdate は、時刻を持たない「暦の日付」を扱う小さな型を提供する。
//
// 祝日・営業日・和暦・支払期日の計算はすべて日付単位で行うため、
// time.Time のタイムゾーンや夏時間に振り回されないよう UTC の 0 時に固定して持つ。
package jpdate

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Date は年月日だけを持つ日付。ゼロ値は「未設定」。
type Date struct {
	t time.Time
}

// New は年・月・日から Date を作る。存在しない日付（2月30日など）はエラー。
func New(y int, m time.Month, d int) (Date, error) {
	if y < 1 || y > 9999 {
		return Date{}, fmt.Errorf("年 %d は扱える範囲（1〜9999）の外です", y)
	}
	if m < 1 || m > 12 {
		return Date{}, fmt.Errorf("月 %d は 1〜12 で指定してください", int(m))
	}
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	if t.Year() != y || t.Month() != m || t.Day() != d {
		return Date{}, fmt.Errorf("%d年%d月%d日 は存在しない日付です", y, int(m), d)
	}
	return Date{t: t}, nil
}

// MustNew は New の失敗で panic する版。テストと定数表のためだけに使う。
func MustNew(y int, m time.Month, d int) Date {
	dt, err := New(y, m, d)
	if err != nil {
		panic(err)
	}
	return dt
}

// FromTime は time.Time の年月日部分だけを取り出す。
func FromTime(t time.Time) Date {
	return Date{t: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)}
}

// ErrEmpty は空の入力を表す。
var ErrEmpty = errors.New("日付が空です")

// Parse は次の書き方を受け付ける（全角数字も可）。
//
//	2026-10-20 / 2026/10/20 / 2026.10.20 / 20261020 / 2026年10月20日
func Parse(s string) (Date, error) {
	s = strings.TrimSpace(toHalfWidthDigits(s))
	if s == "" {
		return Date{}, ErrEmpty
	}
	if len(s) == 8 && isAllDigits(s) {
		y, _ := strconv.Atoi(s[:4])
		m, _ := strconv.Atoi(s[4:6])
		d, _ := strconv.Atoi(s[6:])
		return New(y, time.Month(m), d)
	}
	r := strings.NewReplacer("年", "-", "月", "-", "日", "", "/", "-", ".", "-")
	norm := r.Replace(s)
	parts := strings.Split(norm, "-")
	if len(parts) != 3 {
		return Date{}, fmt.Errorf("日付 %q を読めません（例: 2026-10-20）", s)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || !isAllDigits(p) || len(p) > 4 {
			return Date{}, fmt.Errorf("日付 %q を読めません（例: 2026-10-20）", s)
		}
		nums[i], _ = strconv.Atoi(p)
	}
	if len(strings.TrimSpace(parts[0])) != 4 {
		return Date{}, fmt.Errorf("日付 %q は西暦4桁で書いてください（和暦は wareki_convert で変換できます）", s)
	}
	return New(nums[0], time.Month(nums[1]), nums[2])
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func toHalfWidthDigits(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= '０' && r <= '９':
			return r - '０' + '0'
		case r == '／':
			return '/'
		case r == '－' || r == '−' || r == '‐' || r == '–':
			return '-'
		case r == '．':
			return '.'
		case unicode.IsSpace(r):
			return ' '
		}
		return r
	}, s)
}

// IsZero は未設定かどうか。
func (d Date) IsZero() bool { return d.t.IsZero() }

// Year などは time.Time と同じ意味。
func (d Date) Year() int                    { return d.t.Year() }
func (d Date) Month() time.Month            { return d.t.Month() }
func (d Date) Day() int                     { return d.t.Day() }
func (d Date) Weekday() time.Weekday        { return d.t.Weekday() }
func (d Date) Time() time.Time              { return d.t }
func (d Date) AddDays(n int) Date           { return Date{t: d.t.AddDate(0, 0, n)} }
func (d Date) Before(o Date) bool           { return d.t.Before(o.t) }
func (d Date) After(o Date) bool            { return d.t.After(o.t) }
func (d Date) Equal(o Date) bool            { return d.t.Equal(o.t) }
func (d Date) Compare(o Date) int           { return d.t.Compare(o.t) }
func (d Date) DaysUntil(o Date) int         { return int(o.t.Sub(d.t).Hours() / 24) }
func (d Date) String() string               { return d.t.Format("2006-01-02") }
func (d Date) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalText は Parse と同じ書き方を受け付ける。
func (d *Date) UnmarshalText(b []byte) error {
	p, err := Parse(string(b))
	if err != nil {
		return err
	}
	*d = p
	return nil
}

// LastDayOfMonth はその月の末日。
func LastDayOfMonth(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// AddMonthsClamp は月を足し、日がその月に無ければ末日に寄せる（1月31日＋1か月→2月28日）。
// 結果が1〜9999年の外になるときはエラー。
func (d Date) AddMonthsClamp(n int) (Date, error) {
	if d.IsZero() {
		return Date{}, ErrEmpty
	}
	if n < -12*10000 || n > 12*10000 {
		return Date{}, fmt.Errorf("月数 %d は大きすぎます", n)
	}
	total := d.Year()*12 + int(d.Month()) - 1 + n
	y := total / 12
	m := time.Month(total%12 + 1)
	if total < 0 || y < 1 || y > 9999 {
		return Date{}, fmt.Errorf("%s から %dか月後は扱える範囲（1〜9999年）の外です", d, n)
	}
	day := d.Day()
	if last := LastDayOfMonth(y, m); day > last {
		day = last
	}
	return New(y, m, day)
}

// NthWeekday は y年m月の第n w曜日（n は 1 始まり）。
func NthWeekday(y int, m time.Month, n int, w time.Weekday) Date {
	first := MustNew(y, m, 1)
	offset := (int(w) - int(first.Weekday()) + 7) % 7
	return first.AddDays(offset + 7*(n-1))
}

// WeekdayJA は曜日の日本語1文字。
func WeekdayJA(w time.Weekday) string {
	return [...]string{"日", "月", "火", "水", "木", "金", "土"}[w]
}
