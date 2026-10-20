// Package payterm は「月末締め翌月末払い」のような支払条件から、締め日と支払日を求める。
// 支払日が休みの日のときは、前営業日・翌営業日に寄せられる（会社の休みも反映）。
package payterm

import (
	"fmt"
	"strings"
	"time"

	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/holiday"
	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/jpdate"
)

// EndOfMonth は「末日」を表す値。
const EndOfMonth = 31

// Terms は支払条件。
type Terms struct {
	// ClosingDay は締め日（1〜28、または 31=末日）。
	ClosingDay int
	// MonthOffset は締めた月から何か月後に払うか（0=当月、1=翌月、2=翌々月）。
	MonthOffset int
	// PayDay は支払日（1〜28、または 31=末日）。
	PayDay int
	// Adjust は支払日が休みのとき：previous=前営業日、next=翌営業日、none=そのまま。
	Adjust string
}

// Result は計算結果。
type Result struct {
	TransactionDate jpdate.Date `json:"transaction_date"`
	ClosingDate     jpdate.Date `json:"closing_date"`
	ScheduledDate   jpdate.Date `json:"scheduled_pay_date"`
	PayDate         jpdate.Date `json:"pay_date"`
	PayWeekday      string      `json:"pay_weekday"`
	Adjusted        bool        `json:"adjusted"`
	AdjustReason    string      `json:"adjust_reason,omitempty"`
	DaysFromTxn     int         `json:"days_from_transaction"`
	TermsLabel      string      `json:"terms_label"`
}

func dayLabel(d int) string {
	if d >= EndOfMonth {
		return "末日"
	}
	return fmt.Sprintf("%d日", d)
}

// Label は支払条件の日本語表記（「月末締め翌月末払い」など）。
func (t Terms) Label() string {
	closing := dayLabel(t.ClosingDay) + "締め"
	if t.ClosingDay >= EndOfMonth {
		closing = "月末締め"
	}
	month := [...]string{"当月", "翌月", "翌々月"}
	m := fmt.Sprintf("%dか月後の", t.MonthOffset)
	if t.MonthOffset >= 0 && t.MonthOffset < len(month) {
		m = month[t.MonthOffset]
	}
	pay := dayLabel(t.PayDay)
	if t.PayDay >= EndOfMonth {
		pay = "末"
	}
	return closing + m + pay + "払い"
}

func dayIn(y int, m time.Month, d int) jpdate.Date {
	last := jpdate.LastDayOfMonth(y, m)
	if d >= EndOfMonth || d > last {
		d = last
	}
	return jpdate.MustNew(y, m, d)
}

// Due は取引日（納品日・検収日など）から締め日と支払日を求める。
func Due(txn jpdate.Date, t Terms, cal *holiday.Calendar) (Result, error) {
	if t.ClosingDay < 1 || (t.ClosingDay > 28 && t.ClosingDay != EndOfMonth) {
		return Result{}, fmt.Errorf("締め日 %d は 1〜28 か 31（末日）で指定してください", t.ClosingDay)
	}
	if t.PayDay < 1 || (t.PayDay > 28 && t.PayDay != EndOfMonth) {
		return Result{}, fmt.Errorf("支払日 %d は 1〜28 か 31（末日）で指定してください", t.PayDay)
	}
	if t.MonthOffset < 0 || t.MonthOffset > 12 {
		return Result{}, fmt.Errorf("支払月 %d は 0（当月）〜12 で指定してください", t.MonthOffset)
	}
	dir := 0
	switch strings.ToLower(strings.TrimSpace(t.Adjust)) {
	case "", "previous", "prev", "before", "前営業日":
		dir = -1
	case "next", "after", "翌営業日":
		dir = 1
	case "none", "そのまま":
		dir = 0
	default:
		return Result{}, fmt.Errorf("休日の扱い %q は previous（前営業日）・next（翌営業日）・none（そのまま）です", t.Adjust)
	}
	if cal == nil {
		cal = &holiday.Calendar{}
	}
	// 祝日を計算できる範囲（支払日が翌年にずれる分も含めて）の外の取引日は、月の計算の前に断る。
	if txn.IsZero() || txn.Year() < holiday.MinYear || txn.Year() > holiday.MaxYear-2 {
		return Result{}, fmt.Errorf("取引日は %d〜%d年 で指定してください", holiday.MinYear, holiday.MaxYear-2)
	}
	closing := dayIn(txn.Year(), txn.Month(), t.ClosingDay)
	if txn.After(closing) {
		first, err := jpdate.MustNew(txn.Year(), txn.Month(), 1).AddMonthsClamp(1)
		if err != nil {
			return Result{}, err
		}
		closing = dayIn(first.Year(), first.Month(), t.ClosingDay)
	}
	pm, err := jpdate.MustNew(closing.Year(), closing.Month(), 1).AddMonthsClamp(t.MonthOffset)
	if err != nil {
		return Result{}, err
	}
	scheduled := dayIn(pm.Year(), pm.Month(), t.PayDay)
	if scheduled.Before(closing) {
		return Result{}, fmt.Errorf("支払日（%s）が締め日（%s）より前になります。支払月を見直してください", scheduled, closing)
	}
	pay, err := cal.Shift(scheduled, dir)
	if err != nil {
		return Result{}, err
	}
	res := Result{
		TransactionDate: txn,
		ClosingDate:     closing,
		ScheduledDate:   scheduled,
		PayDate:         pay,
		PayWeekday:      jpdate.WeekdayJA(pay.Weekday()),
		Adjusted:        !pay.Equal(scheduled),
		DaysFromTxn:     txn.DaysUntil(pay),
		TermsLabel:      t.Label(),
	}
	if res.Adjusted {
		st, err := cal.Check(scheduled)
		if err != nil {
			return Result{}, err
		}
		way := "前営業日"
		if dir > 0 {
			way = "翌営業日"
		}
		res.AdjustReason = fmt.Sprintf("%s（%s）が%sのため%sに寄せました", scheduled, jpdate.WeekdayJA(scheduled.Weekday()), st.Reason, way)
	}
	return res, nil
}
