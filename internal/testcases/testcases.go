// Package testcases は、道具の呼び出し例とその正解（ゴールデンファイル）を、
// サーバーの試験・実行ファイルの結合試験・遊び場の例で共有する。
package testcases

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"
)

// Case は道具の呼び出しと、その結果を保存するファイル名。cmd の結合試験でも使う。
type Case struct {
	Name string
	Tool string
	Args map[string]any
}

// Cases は全道具を、実務でよくある入力で呼ぶ。
var Cases = []Case{
	{"holidays_2026", "holidays_list", map[string]any{"year": 2026}},
	{"holidays_2019_range", "holidays_list", map[string]any{"from": "2019-04-27", "to": "2019-05-06"}},
	{"holidays_2030_estimate", "holidays_list", map[string]any{"from": "2030-03-01", "to": "2030-03-31"}},
	{"check_silver_week", "business_day_check", map[string]any{"date": "2026-09-22"}},
	{"check_company_closed", "business_day_check", map[string]any{"date": "2026-12-30", "calendar": map[string]any{"company_holidays": []string{"12-29..01-03"}}}},
	{"add_new_year", "business_day_add", map[string]any{"date": "2026-12-28", "days": 1, "calendar": map[string]any{"company_holidays": []string{"12-29..01-03"}}}},
	{"add_back_gw", "business_day_add", map[string]any{"date": "2026-05-07", "days": -3}},
	{"count_october", "business_day_count", map[string]any{"from": "2026-10-01", "to": "2026-10-31"}},
	{"due_eom_next_eom", "payment_due_date", map[string]any{"transaction_date": "2026-01-15", "closing_day": 31, "month_offset": 1, "pay_day": 31}},
	{"due_gw_next", "payment_due_date", map[string]any{"transaction_date": "2026-03-10", "closing_day": 31, "month_offset": 2, "pay_day": 5, "holiday_adjust": "next"}},
	{"numbers_mixed", "registration_number_validate", map[string]any{"numbers": []string{"T7000012050002", "７０００－０１２０－５０００２", "T7000012050003", "T12345"}}},
	{"tax_once_per_rate", "consumption_tax_calculate", map[string]any{"lines": []map[string]any{
		{"description": "部品A", "amount": 105, "rate": 10}, {"description": "部品B", "amount": 105, "rate": 10},
		{"description": "お茶", "unit_price": 130, "quantity": 3, "rate": 8},
	}}},
	{"tax_inclusive_round", "consumption_tax_calculate", map[string]any{"price_mode": "inclusive", "rounding": "round", "lines": []map[string]any{{"amount": 999, "rate": 10}}}},
	{"invoice_ok", "qualified_invoice_check", map[string]any{
		"issuer_name": "株式会社サンプル商事", "registration_number": "T7000012050002", "transaction_date": "2026-10-20", "recipient_name": "株式会社取引先",
		"items":       []map[string]any{{"description": "事務用品", "amount": 210, "rate": 10}, {"description": "お茶", "amount": 390, "rate": 8, "reduced_mark": true}},
		"rate_totals": []map[string]any{{"rate": 10, "total": 210, "tax": 21}, {"rate": 8, "total": 390, "tax": 31}},
	}},
	{"invoice_problems", "qualified_invoice_check", map[string]any{
		"issuer_name": "株式会社サンプル商事", "registration_number": "7000012050002", "transaction_date": "2026-10-20",
		"items":       []map[string]any{{"description": "部品A", "amount": 105, "rate": 10}, {"description": "部品B", "amount": 105, "rate": 10}, {"description": "お茶", "amount": 390, "rate": 8}},
		"rate_totals": []map[string]any{{"rate": 10, "total": 210, "tax": 20}, {"rate": 8, "total": 390, "tax": 31}},
	}},
	{"withholding_300k", "withholding_tax_calculate", map[string]any{"amount": 300000, "consumption_tax": 30000, "payment_date": "2026-10-31"}},
	{"withholding_over_1m", "withholding_tax_calculate", map[string]any{"amount": 1500000, "payment_date": "2026-10-31"}},
	{"withholding_reverse", "withholding_tax_calculate", map[string]any{"net_target": 100000, "payment_date": "2026-10-31"}},
	{"stamp_tax_stated", "revenue_stamp_receipt", map[string]any{"amount": 55000, "consumption_tax": 5000}},
	{"stamp_3m", "revenue_stamp_receipt", map[string]any{"amount": 3000000}},
	{"wareki_reiwa_kanji", "wareki_convert", map[string]any{"input": "令和八年十月二十日"}},
	{"wareki_year_1989", "wareki_convert", map[string]any{"input": "1989"}},
	{"normalize_address", "text_normalize", map[string]any{"text": "〒１００ー０００１　東京都千代田区千代田１丁目１番１号", "mode": "address"}},
	{"normalize_katakana", "text_normalize", map[string]any{"text": "ｶﾌﾞｼｷｶﾞｲｼｬ　さんぷる", "kana": "katakana"}},
}

// Canonical は比較用に JSON を整形する（キーの順をそろえ、字下げする）。
func Canonical(t testing.TB, v any) []byte {
	t.Helper()
	b, err := CanonicalBytes(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// CanonicalBytes は Canonical の testing を使わない版。
func CanonicalBytes(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var x any
	if err := json.Unmarshal(b, &x); err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(x, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// Path はゴールデンファイルの絶対パス。
func Path(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "golden", name+".json")
}
