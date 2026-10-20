package holiday

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/jpdate"
)

// MonthDay は毎年くり返す月日（年末年始の休みなど）。
type MonthDay struct {
	Month time.Month
	Day   int
}

// ParseMonthDay は "12-29" "12/29" "1229" を読む。
func ParseMonthDay(s string) (MonthDay, error) {
	s = strings.TrimSpace(strings.NewReplacer("/", "-", "月", "-", "日", "").Replace(s))
	var m, d int
	var err1, err2 error
	if p := strings.Split(s, "-"); len(p) == 2 {
		m, err1 = strconv.Atoi(p[0])
		d, err2 = strconv.Atoi(p[1])
	} else if len(s) == 4 {
		m, err1 = strconv.Atoi(s[:2])
		d, err2 = strconv.Atoi(s[2:])
	} else {
		return MonthDay{}, fmt.Errorf("毎年の休み %q を読めません（例: 12-29）", s)
	}
	if err1 != nil || err2 != nil || m < 1 || m > 12 || d < 1 || d > jpdate.LastDayOfMonth(2024, time.Month(m)) {
		return MonthDay{}, fmt.Errorf("毎年の休み %q は存在しない月日です", s)
	}
	return MonthDay{Month: time.Month(m), Day: d}, nil
}

// Calendar は会社ごとの営業日の決まり。ゼロ値は「土日・祝日・休日が休み」。
type Calendar struct {
	// WorkSaturday が true なら土曜を営業日とする。
	WorkSaturday bool
	// WorkSunday が true なら日曜を営業日とする。
	WorkSunday bool
	// WorkOnHolidays が true なら祝日・休日も営業日とする（会社の休みと土日だけ見る）。
	WorkOnHolidays bool
	// annual は毎年の会社の休み（年末年始・夏季休暇など）の集合。AddAnnual で足す。
	annual [13][32]bool
	// Closed は特定の日の会社の休み（創立記念日・臨時休業など）。
	Closed map[jpdate.Date]string
	// Open は、本来休みでも営業する日（土曜出勤日など）。ほかの設定より優先する。
	Open map[jpdate.Date]bool
	// ScanLimit は1回の計算でたどる日数の上限。0 なら DefaultScanLimit。
	ScanLimit int
}

// 会社の休み・営業日の指定の上限（範囲を展開した後の日数で数える）。
const MaxCompanyDays = 2000

// DefaultScanLimit は1回の計算でたどる日数の既定の上限（約60年）。
const DefaultScanLimit = 366 * 60

// MaxBusinessDays は n営業日後の n の上限（約40年分）。
const MaxBusinessDays = 10000

// AddAnnual は毎年の会社の休みを足す（同じ月日は1つにまとまる）。
func (c *Calendar) AddAnnual(md MonthDay) {
	if md.Month >= 1 && md.Month <= 12 && md.Day >= 1 && md.Day <= 31 {
		c.annual[md.Month][md.Day] = true
	}
}

// IsAnnual はその月日が毎年の会社の休みか。
func (c *Calendar) IsAnnual(m time.Month, d int) bool {
	return m >= 1 && m <= 12 && d >= 1 && d <= 31 && c.annual[m][d]
}

func (c *Calendar) scanLimit() int {
	if c.ScanLimit > 0 {
		return c.ScanLimit
	}
	return DefaultScanLimit
}

// DayStatus は1日の判定結果。
type DayStatus struct {
	Date       jpdate.Date `json:"date"`
	Weekday    string      `json:"weekday"`
	Business   bool        `json:"is_business_day"`
	Reason     string      `json:"reason"`
	Holiday    *Holiday    `json:"holiday,omitempty"`
	IsEstimate bool        `json:"is_estimate,omitempty"`
}

// Check はその日が営業日かどうかと、その理由を返す。
func (c *Calendar) Check(d jpdate.Date) (DayStatus, error) {
	st := DayStatus{Date: d, Weekday: jpdate.WeekdayJA(d.Weekday())}
	h, isHol, err := Lookup(d)
	if err != nil {
		return st, err
	}
	if isHol {
		hh := h
		st.Holiday = &hh
		st.IsEstimate = IsEstimate(d.Year()) && (h.Name == "春分の日" || h.Name == "秋分の日" || h.Kind != National)
	}
	if c.Open[d] {
		st.Business, st.Reason = true, "営業日（出勤日として指定）"
		return st, nil
	}
	if isHol && !c.WorkOnHolidays {
		st.Reason = h.Name
		if h.Kind == National || h.Kind == Special {
			st.Reason += "（" + KindLabel(h.Kind) + "）"
		}
		return st, nil
	}
	if name, ok := c.Closed[d]; ok {
		if name == "" {
			name = "会社の休み"
		}
		st.Reason = name
		return st, nil
	}
	if c.IsAnnual(d.Month(), d.Day()) {
		st.Reason = fmt.Sprintf("会社の休み（毎年 %d月%d日）", d.Month(), d.Day())
		return st, nil
	}
	switch d.Weekday() {
	case time.Saturday:
		if !c.WorkSaturday {
			st.Reason = "土曜日"
			return st, nil
		}
	case time.Sunday:
		if !c.WorkSunday {
			st.Reason = "日曜日"
			return st, nil
		}
	}
	st.Business, st.Reason = true, "営業日"
	return st, nil
}

// IsBusinessDay は Check の真偽だけを返す。
func (c *Calendar) IsBusinessDay(d jpdate.Date) (bool, error) {
	st, err := c.Check(d)
	return st.Business, err
}

// AddBusinessDays は d から n 営業日後（n<0 なら前）の日を返す。起点の日は数えない。
// n=0 のときは、d が営業日なら d、休みなら次の営業日を返す。
func (c *Calendar) AddBusinessDays(d jpdate.Date, n int) (jpdate.Date, error) {
	if n < -MaxBusinessDays || n > MaxBusinessDays {
		return jpdate.Date{}, fmt.Errorf("営業日数 %d は大きすぎます（±%d まで）", n, MaxBusinessDays)
	}
	limit := c.scanLimit()
	step := 1
	if n < 0 {
		step = -1
	}
	cur := d
	if n == 0 {
		for i := 0; i < limit; i++ {
			ok, err := c.IsBusinessDay(cur)
			if err != nil {
				return jpdate.Date{}, err
			}
			if ok {
				return cur, nil
			}
			cur = cur.AddDays(1)
		}
		return jpdate.Date{}, errNoBusinessDay
	}
	remain := n * step
	scanned := 0
	for remain > 0 {
		cur = cur.AddDays(step)
		scanned++
		if scanned > limit {
			return jpdate.Date{}, errScanLimit
		}
		ok, err := c.IsBusinessDay(cur)
		if err != nil {
			return jpdate.Date{}, err
		}
		if ok {
			remain--
		}
	}
	return cur, nil
}

var (
	errNoBusinessDay = fmt.Errorf("営業日が見つかりません（休みの設定を見直してください）")
	errScanLimit     = fmt.Errorf("たどれる日数の上限を超えました（営業日数を減らすか、休みの設定を見直してください）")
)

// Shift は休みの日を前後の営業日に寄せる。dir>0 で翌営業日、dir<0 で前営業日、0 はそのまま。
func (c *Calendar) Shift(d jpdate.Date, dir int) (jpdate.Date, error) {
	if dir == 0 {
		return d, nil
	}
	cur := d
	for i := 0; i < c.scanLimit(); i++ {
		ok, err := c.IsBusinessDay(cur)
		if err != nil {
			return jpdate.Date{}, err
		}
		if ok {
			return cur, nil
		}
		if dir > 0 {
			cur = cur.AddDays(1)
		} else {
			cur = cur.AddDays(-1)
		}
	}
	return jpdate.Date{}, errNoBusinessDay
}

// CountResult は期間の内訳。
type CountResult struct {
	From         jpdate.Date `json:"from"`
	To           jpdate.Date `json:"to"`
	CalendarDays int         `json:"calendar_days"`
	BusinessDays int         `json:"business_days"`
	Holidays     []Holiday   `json:"holidays_in_range"`
	CompanyOff   int         `json:"company_days_off"`
	Weekends     int         `json:"weekend_days"`
}

// Count は from から to まで（両端を含む）の営業日数を数える。
func (c *Calendar) Count(from, to jpdate.Date) (CountResult, error) {
	res := CountResult{From: from, To: to, Holidays: []Holiday{}}
	if to.Before(from) {
		return res, fmt.Errorf("終わりの日 %s が始まりの日 %s より前です", to, from)
	}
	if from.DaysUntil(to) >= c.scanLimit() {
		return res, fmt.Errorf("期間が長すぎます（%d日まで）", c.scanLimit())
	}
	for d := from; !d.After(to); d = d.AddDays(1) {
		res.CalendarDays++
		st, err := c.Check(d)
		if err != nil {
			return res, err
		}
		if st.Holiday != nil {
			res.Holidays = append(res.Holidays, *st.Holiday)
		}
		switch {
		case st.Business:
			res.BusinessDays++
		case st.Holiday != nil && !c.WorkOnHolidays:
		case c.isCompanyOff(d):
			res.CompanyOff++
		default:
			res.Weekends++
		}
	}
	return res, nil
}

func (c *Calendar) isCompanyOff(d jpdate.Date) bool {
	if c.Open[d] {
		return false
	}
	if _, ok := c.Closed[d]; ok {
		return true
	}
	return c.IsAnnual(d.Month(), d.Day())
}
