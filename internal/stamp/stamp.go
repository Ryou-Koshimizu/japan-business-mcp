// Package stamp は、領収書（印紙税法 別表第一 第17号文書「金銭又は有価証券の受取書」）に
// 貼る収入印紙の額を求める。2014年4月1日以降に作成する受取書の税率による。
package stamp

import "fmt"

// Input は判定の入力。
type Input struct {
	// Amount は受取金額（円）。0 かつ AmountUnstated=true なら「金額の記載のない受取書」。
	Amount int64
	// TaxStated は、領収書に消費税額等が区分して書かれているか。書かれていれば税抜の金額で判定する。
	TaxStated bool
	// ConsumptionTax は区分して書かれた消費税額等。
	ConsumptionTax int64
	// AmountUnstated は金額を書かない受取書か。
	AmountUnstated bool
	// SalesProceeds は売上代金の受取書か（false なら借入金の受取りなど「売上代金以外」）。
	SalesProceeds bool
	// Electronic は電子データ（PDF・メール等）だけで渡すか。紙で交付しないものは課税文書に当たらない。
	Electronic bool
	// NonBusiness は営業に関しない受取書か（個人が私的に家財を売った代金など）。
	NonBusiness bool
}

// Result は判定結果。
type Result struct {
	Stamp      int64  `json:"stamp_tax"`
	BaseAmount int64  `json:"amount_for_judgement"`
	Bracket    string `json:"bracket"`
	Reason     string `json:"reason"`
}

type bracket struct {
	upTo  int64 // この額以下
	stamp int64
	label string
}

var salesTable = []bracket{
	{49_999, 0, "5万円未満"},
	{1_000_000, 200, "100万円以下"},
	{2_000_000, 400, "100万円を超え200万円以下"},
	{3_000_000, 600, "200万円を超え300万円以下"},
	{5_000_000, 1_000, "300万円を超え500万円以下"},
	{10_000_000, 2_000, "500万円を超え1千万円以下"},
	{20_000_000, 4_000, "1千万円を超え2千万円以下"},
	{30_000_000, 6_000, "2千万円を超え3千万円以下"},
	{50_000_000, 10_000, "3千万円を超え5千万円以下"},
	{100_000_000, 20_000, "5千万円を超え1億円以下"},
	{200_000_000, 40_000, "1億円を超え2億円以下"},
	{300_000_000, 60_000, "2億円を超え3億円以下"},
	{500_000_000, 100_000, "3億円を超え5億円以下"},
	{1_000_000_000, 150_000, "5億円を超え10億円以下"},
}

// Calculate は印紙税額を返す。
func Calculate(in Input) (Result, error) {
	if in.Amount < 0 || in.Amount > 1_000_000_000_000_000 {
		return Result{}, fmt.Errorf("金額 %d円 は範囲の外です", in.Amount)
	}
	if in.ConsumptionTax < 0 || in.ConsumptionTax > in.Amount {
		return Result{}, fmt.Errorf("消費税額 %d円 は 0 以上・受取金額以下で指定してください", in.ConsumptionTax)
	}
	if in.Electronic {
		return Result{Reason: "電子データ（PDF・メールなど）だけで渡す領収書は、紙の文書を作らないため印紙税の課税文書に当たりません"}, nil
	}
	if in.NonBusiness {
		return Result{Reason: "営業に関しない受取書は非課税です（第17号文書の非課税物件）"}, nil
	}
	base := in.Amount
	if in.TaxStated {
		base -= in.ConsumptionTax
	}
	if in.AmountUnstated {
		return Result{Stamp: 200, Bracket: "受取金額の記載のないもの", Reason: "金額を書かない受取書は200円です"}, nil
	}
	res := Result{BaseAmount: base}
	if !in.SalesProceeds {
		if base < 50_000 {
			res.Bracket, res.Reason = "5万円未満", "5万円未満の受取書は非課税です"
			return res, nil
		}
		res.Stamp, res.Bracket, res.Reason = 200, "売上代金以外（5万円以上）", "売上代金以外の金銭の受取書は、5万円以上なら一律200円です（第17号の2）"
		return res, nil
	}
	for _, b := range salesTable {
		if base <= b.upTo {
			res.Stamp, res.Bracket = b.stamp, b.label
			break
		}
	}
	if res.Bracket == "" {
		res.Stamp, res.Bracket = 200_000, "10億円を超えるもの"
	}
	switch {
	case res.Stamp == 0:
		res.Reason = "受取金額が5万円未満のため非課税です"
	case in.TaxStated:
		res.Reason = fmt.Sprintf("消費税額等が区分して書かれているため、税抜の %d円 で判定しました（第17号の1）", base)
	default:
		res.Reason = fmt.Sprintf("受取金額 %d円 で判定しました。消費税額等を区分して書くと税抜の金額で判定できます（第17号の1）", base)
	}
	return res, nil
}
