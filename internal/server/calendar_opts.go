package server

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/holiday"
	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/jpdate"
)

// CalendarOptions は会社ごとの営業日の決まり（道具の引数、または起動時の設定ファイル）。
type CalendarOptions struct {
	CompanyHolidays []string `json:"company_holidays,omitempty" jsonschema:"会社の休み。毎年くり返すなら 12-29、特定の日なら 2026-08-14。範囲は 12-29..01-03 や 2026-08-13..2026-08-16 のように .. でつなぐ"`
	WorkDays        []string `json:"work_days,omitempty" jsonschema:"休みの日でも営業する日（土曜出勤日など）。YYYY-MM-DD"`
	WorkSaturday    bool     `json:"work_saturday,omitempty" jsonschema:"土曜を営業日にする"`
	WorkSunday      bool     `json:"work_sunday,omitempty" jsonschema:"日曜を営業日にする"`
	WorkOnHolidays  bool     `json:"work_on_holidays,omitempty" jsonschema:"祝日・休日も営業日にする"`
}

// LoadCalendarFile は起動時の既定の会社カレンダー（JSON）を読む。
func LoadCalendarFile(path string) (CalendarOptions, error) {
	var c CalendarOptions
	b, err := os.ReadFile(path) // #nosec G304 -- 起動した人が自分で指定する設定ファイル
	if err != nil {
		return c, fmt.Errorf("会社カレンダーの読み込み: %w", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("会社カレンダー %s の形式: %w", path, err)
	}
	if _, err := c.build(CalendarOptions{}); err != nil {
		return c, err
	}
	return c, nil
}

// build は既定（base）に道具の引数（c）を足してカレンダーを作る。
func (c CalendarOptions) build(base CalendarOptions) (*holiday.Calendar, error) {
	cal := &holiday.Calendar{
		WorkSaturday:   base.WorkSaturday || c.WorkSaturday,
		WorkSunday:     base.WorkSunday || c.WorkSunday,
		WorkOnHolidays: base.WorkOnHolidays || c.WorkOnHolidays,
		Closed:         map[jpdate.Date]string{},
		Open:           map[jpdate.Date]bool{},
	}
	hol := append(append([]string{}, base.CompanyHolidays...), c.CompanyHolidays...)
	if len(hol) > 2000 {
		return nil, fmt.Errorf("会社の休みは2000件までです")
	}
	for _, s := range hol {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if a, b, ok := strings.Cut(s, ".."); ok {
			if err := addRange(cal, strings.TrimSpace(a), strings.TrimSpace(b)); err != nil {
				return nil, err
			}
			continue
		}
		if d, err := jpdate.Parse(s); err == nil {
			cal.Closed[d] = "会社の休み"
			continue
		}
		md, err := holiday.ParseMonthDay(s)
		if err != nil {
			return nil, fmt.Errorf("会社の休み %q を読めません（例: 12-29、2026-08-14、12-29..01-03）", s)
		}
		cal.Annual = append(cal.Annual, md)
	}
	for _, s := range append(append([]string{}, base.WorkDays...), c.WorkDays...) {
		d, err := jpdate.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("営業する日 %q: %w", s, err)
		}
		cal.Open[d] = true
	}
	return cal, nil
}

func addRange(cal *holiday.Calendar, a, b string) error {
	if da, err := jpdate.Parse(a); err == nil {
		db, err := jpdate.Parse(b)
		if err != nil {
			return fmt.Errorf("範囲の終わり %q: %w", b, err)
		}
		if db.Before(da) || da.DaysUntil(db) > 366 {
			return fmt.Errorf("範囲 %s..%s は、始まり≦終わり・1年以内で指定してください", a, b)
		}
		for d := da; !d.After(db); d = d.AddDays(1) {
			cal.Closed[d] = "会社の休み"
		}
		return nil
	}
	ma, err := holiday.ParseMonthDay(a)
	if err != nil {
		return err
	}
	mb, err := holiday.ParseMonthDay(b)
	if err != nil {
		return err
	}
	// うるう年を含む 2024年の暦で、月日を順にたどる（12-29..01-03 のような年またぎも可）
	d := jpdate.MustNew(2024, ma.Month, ma.Day)
	for i := 0; i < 366; i++ {
		cal.Annual = append(cal.Annual, holiday.MonthDay{Month: d.Month(), Day: d.Day()})
		if d.Month() == mb.Month && d.Day() == mb.Day {
			return nil
		}
		d = d.AddDays(1)
		if d.Year() == 2025 {
			d = jpdate.MustNew(2024, 1, 1)
		}
	}
	return fmt.Errorf("範囲 %s..%s を読めません", a, b)
}
