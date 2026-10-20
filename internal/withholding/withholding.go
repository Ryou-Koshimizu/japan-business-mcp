// Package withholding は、報酬・料金に対する源泉徴収税額（所得税と復興特別所得税）を計算する。
//
// 所得税法第204条第1項第1号・第2号などの報酬（原稿料・デザイン料・講演料・弁護士や税理士への報酬など）：
//
//	支払金額 100万円以下の部分 … 10.21%
//	100万円を超える部分       … 20.42%（100万円までの税額 102,100円 に加算）
//
// 司法書士・土地家屋調査士・海事代理士への報酬（同項第2号の一部）：
//
//	(支払金額 − 1万円) × 10.21%
//
// 1円未満の端数は切り捨てる。
// 復興特別所得税（2.1%）は 2013年1月1日〜2037年12月31日 に支払うものに加わる。
// 請求書で消費税額が区分されていれば、税抜の金額だけを対象にできる（平成元年直法6-1）。
package withholding

import (
	"fmt"
	"time"

	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/jpdate"
)

// Category は報酬の区分。
type Category string

const (
	General           Category = "general"            // 原稿料・デザイン料・講演料・士業（司法書士等を除く）など
	JudicialScrivener Category = "judicial_scrivener" // 司法書士・土地家屋調査士・海事代理士
)

// Input は計算の入力。
type Input struct {
	// Amount は源泉徴収の対象になる金額（消費税を区分している請求なら税抜の報酬額）。
	Amount int64
	// ConsumptionTax は請求書で区分された消費税額（0 なら加えない）。支払額の計算にだけ使う。
	ConsumptionTax int64
	Category       Category
	// PaymentDate は支払日。空なら今日として扱う（復興特別所得税の有無の判定に使う）。
	PaymentDate jpdate.Date
}

// Result は計算結果。
type Result struct {
	Amount             int64    `json:"amount"`
	Category           Category `json:"category"`
	CategoryLabel      string   `json:"category_label"`
	Withholding        int64    `json:"withholding_tax"`
	ConsumptionTax     int64    `json:"consumption_tax"`
	NetPayment         int64    `json:"net_payment"`
	Formula            string   `json:"formula"`
	WithReconstruction bool     `json:"includes_reconstruction_tax"`
}

var reconstructionEnd = jpdate.MustNew(2037, time.December, 31)
var reconstructionStart = jpdate.MustNew(2013, time.January, 1)

// Calculate は源泉徴収税額と、差し引いた支払額を計算する。
func Calculate(in Input) (Result, error) {
	if in.Amount < 0 || in.Amount > 100_000_000_000 {
		return Result{}, fmt.Errorf("金額 %d円 は 0〜1000億円 の範囲で指定してください", in.Amount)
	}
	if in.ConsumptionTax < 0 || in.ConsumptionTax > in.Amount {
		return Result{}, fmt.Errorf("消費税額 %d円 は 0 以上・報酬額以下で指定してください", in.ConsumptionTax)
	}
	cat := in.Category
	if cat == "" {
		cat = General
	}
	date := in.PaymentDate
	if date.IsZero() {
		date = jpdate.FromTime(time.Now().In(jst))
	}
	recon := !date.Before(reconstructionStart) && !date.After(reconstructionEnd)
	// 税率を万分率で持つ（10.21% → 1021、10% → 1000）
	low, high := int64(1000), int64(2000)
	if recon {
		low, high = 1021, 2042
	}
	res := Result{Amount: in.Amount, Category: cat, ConsumptionTax: in.ConsumptionTax, WithReconstruction: recon}
	pct := func(v int64) string { return fmt.Sprintf("%d.%02d%%", v/100, v%100) }
	switch cat {
	case General:
		res.CategoryLabel = "報酬・料金（原稿料・デザイン料・講演料・士業の報酬など）"
		if in.Amount <= 1_000_000 {
			res.Withholding = in.Amount * low / 10000
			res.Formula = fmt.Sprintf("%d円 × %s = %d円（1円未満切り捨て）", in.Amount, pct(low), res.Withholding)
		} else {
			base := 1_000_000 * low / 10000
			res.Withholding = (in.Amount-1_000_000)*high/10000 + base
			res.Formula = fmt.Sprintf("(%d円 − 100万円) × %s + %d円 = %d円（1円未満切り捨て）", in.Amount, pct(high), base, res.Withholding)
		}
	case JudicialScrivener:
		res.CategoryLabel = "司法書士・土地家屋調査士・海事代理士への報酬"
		if in.Amount > 10_000 {
			res.Withholding = (in.Amount - 10_000) * low / 10000
		}
		res.Formula = fmt.Sprintf("(%d円 − 1万円) × %s = %d円（1円未満切り捨て）", in.Amount, pct(low), res.Withholding)
	default:
		return Result{}, fmt.Errorf("区分 %q は general か judicial_scrivener です", in.Category)
	}
	res.NetPayment = in.Amount + in.ConsumptionTax - res.Withholding
	return res, nil
}

// GrossForNet は、源泉徴収後の手取り（消費税を除く）が net 円になる報酬額を求める。
// ちょうど net になる額が複数あるときは最も小さい額を返す。ちょうど net になる額が無いときは、手取りが net を超えない最大の額と、超える最小の額を返す。
func GrossForNet(net int64, cat Category, date jpdate.Date) (exact bool, lower, upper int64, err error) {
	if net <= 0 || net > 50_000_000_000 {
		return false, 0, 0, fmt.Errorf("手取り %d円 は 1円〜500億円 の範囲で指定してください", net)
	}
	f := func(g int64) (int64, error) {
		r, err := Calculate(Input{Amount: g, Category: cat, PaymentDate: date})
		if err != nil {
			return 0, err
		}
		return g - r.Withholding, nil
	}
	// 手取りは報酬額に対して単調に増えるので二分探索する。
	lo, hi := net, net*2+20_000
	for lo < hi {
		mid := lo + (hi-lo)/2
		v, err := f(mid)
		if err != nil {
			return false, 0, 0, err
		}
		if v < net {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	v, err := f(lo)
	if err != nil {
		return false, 0, 0, err
	}
	if v == net {
		return true, lo, lo, nil
	}
	return false, lo - 1, lo, nil
}

var jst = time.FixedZone("JST", 9*60*60)
