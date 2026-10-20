package ctax

import (
	"fmt"
	"strings"

	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/corpnum"
	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/jpdate"
)

// InvoiceKind は適格請求書の種類。
type InvoiceKind string

const (
	Standard   InvoiceKind = "standard"   // 適格請求書
	Simplified InvoiceKind = "simplified" // 適格簡易請求書（小売・飲食・タクシーなど）
)

// InvoiceItem は請求書の明細1行。
type InvoiceItem struct {
	Description string `json:"description"`
	Amount      int64  `json:"amount"`
	Rate        int    `json:"rate"`
	// ReducedMark は「軽減税率の対象品目である旨」（※印など）が書かれているか。8% の行で必要。
	ReducedMark bool `json:"reduced_mark,omitempty"`
}

// InvoiceRateTotal は請求書に書かれた「税率ごとに区分した合計と税額」。
// この行があること自体を「適用税率が書かれている」とみなす。
type InvoiceRateTotal struct {
	Rate  int    `json:"rate"`
	Total *int64 `json:"total,omitempty"`
	Tax   *int64 `json:"tax,omitempty"`
}

// Invoice は確認する請求書の内容。書かれていない項目は空のまま渡す。
type Invoice struct {
	Kind               InvoiceKind        `json:"kind,omitempty"`
	IssuerName         string             `json:"issuer_name,omitempty"`
	RegistrationNumber string             `json:"registration_number,omitempty"`
	TransactionDate    string             `json:"transaction_date,omitempty"`
	RecipientName      string             `json:"recipient_name,omitempty"`
	PriceMode          string             `json:"price_mode,omitempty"`
	Items              []InvoiceItem      `json:"items,omitempty"`
	RateTotals         []InvoiceRateTotal `json:"rate_totals,omitempty"`
}

// Finding は1項目の確認結果。
type Finding struct {
	Item    string `json:"item"`
	Status  string `json:"status"` // ok / missing / error / warning
	Message string `json:"message"`
}

// CheckResult は確認結果の全体。
type CheckResult struct {
	Kind     InvoiceKind `json:"kind"`
	OK       bool        `json:"ok"`
	Findings []Finding   `json:"findings"`
	Problems int         `json:"problems"`
	Summary  string      `json:"summary"`
}

// CheckInvoice は適格請求書・適格簡易請求書の記載事項（消費税法第57条の4）を確認する。
//
// 適格請求書の記載事項：
//  1. 発行事業者の氏名又は名称と登録番号
//  2. 取引年月日
//  3. 取引内容（軽減税率の対象品目である旨）
//  4. 税率ごとに区分して合計した対価の額と適用税率
//  5. 税率ごとに区分した消費税額等
//  6. 書類の交付を受ける事業者の氏名又は名称（適格簡易請求書では不要）
//
// 適格簡易請求書では、4 の適用税率と 5 の消費税額等は「どちらか一方」でよい。
func CheckInvoice(inv Invoice) (CheckResult, error) {
	kind := inv.Kind
	if kind == "" {
		kind = Standard
	}
	if kind != Standard && kind != Simplified {
		return CheckResult{}, fmt.Errorf("種類 %q は standard（適格請求書）か simplified（適格簡易請求書）です", inv.Kind)
	}
	mode, err := ParseMode(inv.PriceMode)
	if err != nil {
		return CheckResult{}, err
	}
	res := CheckResult{Kind: kind, Findings: []Finding{}}
	add := func(item, status, msg string) {
		res.Findings = append(res.Findings, Finding{Item: item, Status: status, Message: msg})
		if status == "missing" || status == "error" {
			res.Problems++
		}
	}

	// 1. 発行事業者の名称・登録番号
	if strings.TrimSpace(inv.IssuerName) == "" {
		add("発行事業者の氏名又は名称", "missing", "発行する事業者の名前がありません")
	} else {
		add("発行事業者の氏名又は名称", "ok", inv.IssuerName)
	}
	if strings.TrimSpace(inv.RegistrationNumber) == "" {
		add("登録番号", "missing", "登録番号（T＋13桁）がありません")
	} else {
		r, err := corpnum.Validate(inv.RegistrationNumber)
		switch {
		case err != nil:
			add("登録番号", "error", err.Error())
		case r.Kind != corpnum.RegistrationNumber:
			add("登録番号", "error", "先頭に「T」が必要です（法人番号だけでは登録番号になりません）")
		case !r.Valid:
			add("登録番号", "error", r.Message)
		default:
			add("登録番号", "ok", r.Normalized+"（検査用数字は正しい値。登録の有無は国税庁の公表サイトで確認できます）")
		}
	}

	// 2. 取引年月日
	if strings.TrimSpace(inv.TransactionDate) == "" {
		add("取引年月日", "missing", "取引の日付がありません（月ごとにまとめる場合は「2026年10月分」のような期間でも可）")
	} else if _, err := jpdate.Parse(inv.TransactionDate); err != nil && !looksLikePeriod(inv.TransactionDate) {
		add("取引年月日", "error", "日付として読めません: "+inv.TransactionDate)
	} else {
		add("取引年月日", "ok", inv.TransactionDate)
	}

	// 3. 取引内容と軽減税率の旨
	if len(inv.Items) == 0 {
		add("取引内容", "missing", "明細がありません")
	}
	sums := map[int]int64{}
	used := []int{}
	for i, it := range inv.Items {
		if !ValidRate(it.Rate) {
			add("取引内容", "error", fmt.Sprintf("%d行目の税率 %d%% は扱えません（10・8・0）", i+1, it.Rate))
			continue
		}
		if strings.TrimSpace(it.Description) == "" {
			add("取引内容", "missing", fmt.Sprintf("%d行目に品名・内容がありません", i+1))
		}
		if it.Rate == 8 && !it.ReducedMark {
			add("軽減税率の対象品目である旨", "missing", fmt.Sprintf("%d行目「%s」は8%%ですが、軽減税率の対象である旨（※印と注記など）がありません", i+1, it.Description))
		}
		if _, ok := sums[it.Rate]; !ok {
			used = append(used, it.Rate)
		}
		sums[it.Rate] += it.Amount
	}
	if len(inv.Items) > 0 && res.countItem("取引内容") == 0 {
		add("取引内容", "ok", fmt.Sprintf("明細 %d行", len(inv.Items)))
	}

	// 4・5. 税率ごとの合計と税額
	stated := map[int]InvoiceRateTotal{}
	for _, rt := range inv.RateTotals {
		stated[rt.Rate] = rt
	}
	for _, rate := range used {
		if rate == 0 {
			continue
		}
		rt, ok := stated[rate]
		label := fmt.Sprintf("%d%% 対象", rate)
		if !ok || (rt.Total == nil && rt.Tax == nil) {
			add(label+"の合計と税額", "missing", fmt.Sprintf("%d%% の行がありますが、税率ごとの合計・税額の記載がありません", rate))
			continue
		}
		switch {
		case rt.Total == nil:
			add(label+"の合計", "missing", "税率ごとに区分して合計した対価の額がありません")
		case *rt.Total != sums[rate]:
			add(label+"の合計", "error", fmt.Sprintf("記載の合計 %d円 と明細の合計 %d円 が合いません", *rt.Total, sums[rate]))
		default:
			add(label+"の合計", "ok", fmt.Sprintf("%d円", *rt.Total))
		}
		if rt.Tax == nil {
			if kind == Standard {
				add(label+"の消費税額", "missing", "税率ごとに区分した消費税額がありません")
			} else {
				add(label+"の消費税額", "warning", "適格簡易請求書では適用税率の記載があれば税額は省略できます")
			}
			continue
		}
		// 合計が明細と合わないときは、その指摘を出したうえで明細の合計から税額を確かめる。
		base := sums[rate]
		match := MatchingRoundings(base, rate, mode, *rt.Tax)
		if len(match) > 0 {
			names := make([]string, len(match))
			for i, m := range match {
				names[i] = m.Label()
			}
			add(label+"の消費税額", "ok", fmt.Sprintf("%d円（税率ごとに1回の端数処理：%s と一致）", *rt.Tax, strings.Join(names, "・")))
			continue
		}
		var perLine int64 = -1
		for _, r := range []Rounding{Floor, Round, Ceil} {
			var s int64
			for _, it := range inv.Items {
				if it.Rate == rate {
					s += TaxOn(it.Amount, rate, mode, r)
				}
			}
			if s == *rt.Tax {
				perLine = s
				break
			}
		}
		want := TaxOn(base, rate, mode, Floor)
		if perLine >= 0 {
			add(label+"の消費税額", "error", fmt.Sprintf("記載の税額 %d円 は品目ごとに端数処理して足した値です。税率ごとに1回の端数処理では %d円（切り捨ての場合）", *rt.Tax, want))
		} else {
			add(label+"の消費税額", "error", fmt.Sprintf("記載の税額 %d円 は %d円 から計算した値（切り捨てなら %d円）と合いません", *rt.Tax, base, want))
		}
	}

	// 6. 交付を受ける事業者
	if kind == Standard {
		if strings.TrimSpace(inv.RecipientName) == "" {
			add("交付を受ける事業者の氏名又は名称", "missing", "宛名がありません（宛名を省けるのは適格簡易請求書だけです）")
		} else {
			add("交付を受ける事業者の氏名又は名称", "ok", inv.RecipientName)
		}
	}

	res.OK = res.Problems == 0
	if res.OK {
		res.Summary = fmt.Sprintf("%sの記載事項はそろっています（%d項目を確認）", kindLabel(kind), len(res.Findings))
	} else {
		res.Summary = fmt.Sprintf("%sの記載事項に %d件 の不足・誤りがあります", kindLabel(kind), res.Problems)
	}
	return res, nil
}

func (r *CheckResult) countItem(item string) int {
	n := 0
	for _, f := range r.Findings {
		if f.Item == item {
			n++
		}
	}
	return n
}

func kindLabel(k InvoiceKind) string {
	if k == Simplified {
		return "適格簡易請求書"
	}
	return "適格請求書"
}

// looksLikePeriod は「2026年10月分」「2026-10-01〜2026-10-31」のような期間の書き方かどうか。
func looksLikePeriod(s string) bool {
	if strings.Contains(s, "分") || strings.ContainsAny(s, "〜~") {
		for _, r := range s {
			if (r >= '0' && r <= '9') || (r >= '０' && r <= '９') {
				return true
			}
		}
	}
	return false
}
