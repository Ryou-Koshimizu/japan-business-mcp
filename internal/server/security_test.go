package server

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/holiday"
)

// 範囲の指定は展開した後の日数で上限を数える（セキュリティ指摘の回帰試験）。
func TestCalendarExpandedLimit(t *testing.T) {
	many := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		many = append(many, "01-01..12-31") // 1件で366日に展開される
	}
	if _, err := (CalendarOptions{CompanyHolidays: many}).build(CalendarOptions{}); err == nil || !strings.Contains(err.Error(), "2000日まで") {
		t.Fatalf("展開後の上限で止まるはず: %v", err)
	}
	dated := []string{}
	for y := 2026; y < 2036; y++ {
		dated = append(dated, strconv.Itoa(y)+"-01-01.."+strconv.Itoa(y)+"-12-31")
	}
	if _, err := (CalendarOptions{CompanyHolidays: dated}).build(CalendarOptions{}); err == nil {
		t.Fatal("日付の範囲も展開後の日数で止まるはず")
	}
	// 既定（起動時の設定）と引数を合わせて数える
	base := CalendarOptions{CompanyHolidays: []string{"2026-01-01..2026-12-31", "2027-01-01..2027-12-31", "2028-01-01..2028-12-31"}}
	if _, err := (CalendarOptions{CompanyHolidays: []string{"2029-01-01..2029-12-31", "2030-01-01..2030-12-31", "2031-01-01..2031-12-31"}}).build(base); err == nil {
		t.Fatal("既定と引数の合計で止まるはず")
	}
	// 毎年の休みは同じ月日が重なっても1つ（上限内なら受け付ける）
	cal, err := (CalendarOptions{CompanyHolidays: []string{"12-29..01-03", "12-29..01-03", "12-30"}}).build(CalendarOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !cal.IsAnnual(12, 30) || cal.IsAnnual(12, 28) {
		t.Fatal("毎年の休みの集合")
	}
	if cal.ScanLimit != RequestScanLimit {
		t.Fatal("引数で休みを渡したときは走査の上限を下げる")
	}
	if cal, _ := (CalendarOptions{}).build(CalendarOptions{}); cal.ScanLimit != 0 {
		t.Fatal("引数が無いときは既定の上限")
	}
}

func TestToolsDoNotCrashOnExtremeInput(t *testing.T) {
	cs := connect(t, Config{})
	cases := []struct {
		tool string
		args map[string]any
	}{
		{"payment_due_date", map[string]any{"transaction_date": "9999-12-31", "closing_day": 31, "month_offset": 12, "pay_day": 31}},
		{"payment_due_date", map[string]any{"transaction_date": "0001-01-01", "closing_day": 1, "month_offset": 0, "pay_day": 1}},
		{"business_day_add", map[string]any{"date": "9999-12-31", "days": 5}},
		{"business_day_add", map[string]any{"date": "2026-10-20", "days": holiday.MaxBusinessDays, "calendar": map[string]any{"company_holidays": []string{"12-29..01-03"}}}},
		{"business_day_count", map[string]any{"from": "0001-01-01", "to": "9999-12-31"}},
		{"business_day_check", map[string]any{"date": "0001-01-01"}},
		{"wareki_convert", map[string]any{"input": "令和9999年1月1日"}},
	}
	for _, c := range cases {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: c.tool, Arguments: c.args})
		if err != nil {
			t.Fatalf("%s: セッションが壊れた: %v", c.tool, err)
		}
		if !res.IsError {
			t.Fatalf("%s %v はエラー応答になるはず", c.tool, c.args)
		}
	}
	// その後も普通に使える
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "wareki_convert", Arguments: map[string]any{"input": "2026"}})
	if err != nil || res.IsError {
		t.Fatal("続けて使えない")
	}
}

// 道具の中の panic はエラー応答に変わり、サーバーは動き続ける。
func TestPanicBecomesToolError(t *testing.T) {
	s := mcp.NewServer(&mcp.Implementation{Name: "p", Version: "0"}, nil)
	type in struct {
		X int `json:"x"`
	}
	type out struct {
		Y int `json:"y"`
	}
	addTool(s, "boom", "boom", "panic する", func(context.Context, *mcp.CallToolRequest, in) (*mcp.CallToolResult, out, error) {
		panic(errors.New("想定外"))
	})
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := s.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for i := 0; i < 2; i++ {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "boom", Arguments: map[string]any{"x": 1}})
		if err != nil || !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "内部エラー") {
			t.Fatalf("panic がエラー応答にならない: %v %v", err, res)
		}
	}
}

func FuzzCalendarOptions(f *testing.F) {
	f.Add("12-29..01-03", "2026-08-13..2026-08-16", "2026-10-24", 3)
	f.Add("01-01..12-31", "01-01..12-31", "", 20)
	f.Add("2026-01-01..2026-12-31", "9999-12-01..9999-12-31", "0001-01-01", 7)
	f.Add("02-29..02-28", "13-01", "x", 1)
	f.Fuzz(func(t *testing.T, a, b, work string, repeat int) {
		if repeat < 0 || repeat > 50 {
			return
		}
		hol := []string{}
		for i := 0; i < repeat; i++ {
			hol = append(hol, a, b)
		}
		cal, err := (CalendarOptions{CompanyHolidays: hol, WorkDays: []string{work}}).build(CalendarOptions{})
		if err != nil {
			return
		}
		if len(cal.Closed)+len(cal.Open) > holiday.MaxCompanyDays {
			t.Fatalf("上限を超えて積まれた: %d", len(cal.Closed)+len(cal.Open))
		}
	})
}
