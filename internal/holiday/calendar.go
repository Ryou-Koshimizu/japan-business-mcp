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
	// Annual は毎年の会社の休み（年末年始・夏季休暇など）。
	Annual []MonthDay
	// Closed は特定の日の会社の休み（創立記念日・臨時休業など）。
	Closed map[jpdate.Date]string
	// Open は、本来休みでも営業する日（土曜出勤日など）。ほかの設定より優先する。
	Open map[jpdate.Date]bool
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
	for _, md := range c.Annual {
		if md.Month == d.Month() && md.Day == d.Day() {
			st.Reason = fmt.Sprintf("会社の休み（毎年 %d月%d日）", md.Month, md.Day)
			return st, nil
		}
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

// 1回の計算で進める上限（設定の誤りで営業日が1日も無いときに止まるため）。
const maxScanDays = 366 * 30

// AddBusinessDays は d から n 営業日後（n<0 なら前）の日を返す。起点の日は数えない。
// n=0 のときは、d が営業日なら d、休みなら次の営業日を返す。
func (c *Calendar) AddBusinessDays(d jpdate.Date, n int) (jpdate.Date, error) {
	if n < -36500 || n > 36500 {
		return jpdate.Date{}, fmt.Errorf("営業日数 %d は大きすぎます（±36500 まで）", n)
	}
	step := 1
	if n < 0 {
		step = -1
	}
	cur := d
	if n == 0 {
		for i := 0; i < maxScanDays; i++ {
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
		if scanned > maxScanDays+remain*7 {
			return jpdate.Date{}, errNoBusinessDay
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

var errNoBusinessDay = fmt.Errorf("営業日が見つかりません（休みの設定を見直してください）")

// Shift は休みの日を前後の営業日に寄せる。dir>0 で翌営業日、dir<0 で前営業日、0 はそのまま。
func (c *Calendar) Shift(d jpdate.Date, dir int) (jpdate.Date, error) {
	if dir == 0 {
		return d, nil
	}
	cur := d
	for i := 0; i < maxScanDays; i++ {
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
	if from.DaysUntil(to) > 366*100 {
		return res, fmt.Errorf("期間が長すぎます（100年まで）")
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
	for _, md := range c.Annual {
		if md.Month == d.Month() && md.Day == d.Day() {
			return true
		}
	}
	return false
}
