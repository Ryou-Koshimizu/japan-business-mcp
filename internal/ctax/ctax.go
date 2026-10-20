// Package ctax は消費税の計算（税率ごとに合計して1回だけ端数処理）と、
// 適格請求書（インボイス）の記載事項の確認を行う。
//
// 適格請求書では「1つの請求書につき、税率ごとに1回」端数処理する（消費税法施行令第70条の10）。
// 品目ごとに端数処理して足し上げると、合計の税額が1円単位でずれることがあるため、
// 両方を計算して差を見せる。
package ctax

import (
	"fmt"
	"math/big"
	"sort"
	"strings"
)

// Rounding は1円未満の端数の処理。
type Rounding string

const (
	Floor Rounding = "floor" // 切り捨て
	Round Rounding = "round" // 四捨五入
	Ceil  Rounding = "ceil"  // 切り上げ
)

// ParseRounding は英語・日本語のどちらでも受け付ける。空は切り捨て。
func ParseRounding(s string) (Rounding, error) {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "", "floor", "down", "truncate", "切り捨て", "切捨て", "切捨":
		return Floor, nil
	case "round", "half_up", "四捨五入":
		return Round, nil
	case "ceil", "up", "切り上げ", "切上げ", "切上":
		return Ceil, nil
	}
	return "", fmt.Errorf("端数処理 %q は floor（切り捨て）・round（四捨五入）・ceil（切り上げ）のどれかです", s)
}

// Label は端数処理の日本語名。
func (r Rounding) Label() string {
	switch r {
	case Round:
		return "四捨五入"
	case Ceil:
		return "切り上げ"
	}
	return "切り捨て"
}

// Mode は金額が税抜か税込か。
type Mode string

const (
	Exclusive Mode = "exclusive" // 税抜
	Inclusive Mode = "inclusive" // 税込
)

// ParseMode は英語・日本語のどちらでも受け付ける。空は税抜。
func ParseMode(s string) (Mode, error) {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "", "exclusive", "excl", "税抜", "税抜き", "外税":
		return Exclusive, nil
	case "inclusive", "incl", "税込", "税込み", "内税":
		return Inclusive, nil
	}
	return "", fmt.Errorf("金額の種類 %q は exclusive（税抜）か inclusive（税込）です", s)
}

// 金額の上限（1兆円）。これを超える請求は想定しない。
const MaxAmount = 1_000_000_000_000

// Line は請求の1行。Amount（その行の金額）か、UnitPrice×Quantity のどちらかで渡す。
type Line struct {
	Description string `json:"description,omitempty"`
	Amount      int64  `json:"amount,omitempty"`
	UnitPrice   int64  `json:"unit_price,omitempty"`
	Quantity    int64  `json:"quantity,omitempty"`
	Rate        int    `json:"rate"`
}

// LineAmount はその行の金額。
func (l Line) LineAmount() (int64, error) {
	if l.Amount != 0 && (l.UnitPrice != 0 || l.Quantity != 0) {
		return 0, fmt.Errorf("%q: amount と unit_price×quantity はどちらか一方で指定してください", l.Description)
	}
	a := l.Amount
	if l.UnitPrice != 0 || l.Quantity != 0 {
		if l.Quantity < 0 || l.Quantity > 1_000_000_000 {
			return 0, fmt.Errorf("%q: 数量 %d は 0〜10億 の範囲で指定してください", l.Description, l.Quantity)
		}
		if l.UnitPrice < -MaxAmount || l.UnitPrice > MaxAmount {
			return 0, fmt.Errorf("%q: 単価が大きすぎます", l.Description)
		}
		p := new(big.Int).Mul(big.NewInt(l.UnitPrice), big.NewInt(l.Quantity))
		if p.CmpAbs(big.NewInt(MaxAmount)) > 0 {
			return 0, fmt.Errorf("%q: 金額が1兆円を超えます", l.Description)
		}
		a = p.Int64()
	}
	if a < -MaxAmount || a > MaxAmount {
		return 0, fmt.Errorf("%q: 金額が1兆円を超えます", l.Description)
	}
	return a, nil
}

// ValidRate は扱う税率（10%・8%・0%＝非課税や対象外）。
func ValidRate(r int) bool { return r == 10 || r == 8 || r == 0 }

// RateLabel は税率の日本語名。
func RateLabel(r int) string {
	switch r {
	case 10:
		return "標準税率 10%"
	case 8:
		return "軽減税率 8%"
	}
	return "対象外・非課税 0%"
}

// TaxOn は金額 a に対する税額を、指定の端数処理で計算する。
// 税抜なら a×rate/100、税込なら a×rate/(100+rate)。整数演算だけで行う。
func TaxOn(a int64, rate int, mode Mode, r Rounding) int64 {
	if rate == 0 || a == 0 {
		return 0
	}
	num := new(big.Int).Mul(big.NewInt(a), big.NewInt(int64(rate)))
	den := big.NewInt(100)
	if mode == Inclusive {
		den = big.NewInt(int64(100 + rate))
	}
	return divRound(num, den, r)
}

// divRound は num/den を指定の方法で整数にする（負の数は絶対値で処理して符号を戻す。値引き行のため）。
func divRound(num, den *big.Int, r Rounding) int64 {
	neg := num.Sign() < 0
	n := new(big.Int).Abs(num)
	q, m := new(big.Int).QuoRem(n, den, new(big.Int))
	switch r {
	case Ceil:
		if m.Sign() != 0 {
			q.Add(q, big.NewInt(1))
		}
	case Round:
		if new(big.Int).Mul(m, big.NewInt(2)).Cmp(den) >= 0 {
			q.Add(q, big.NewInt(1))
		}
	}
	if neg {
		q.Neg(q)
	}
	return q.Int64()
}

// RateSummary は税率ごとの集計。
type RateSummary struct {
	Rate        int    `json:"rate"`
	RateLabel   string `json:"rate_label"`
	Lines       int    `json:"lines"`
	Subtotal    int64  `json:"subtotal_as_entered"`
	TaxExcluded int64  `json:"amount_excluding_tax"`
	Tax         int64  `json:"tax"`
	TaxIncluded int64  `json:"amount_including_tax"`
	PerLineTax  int64  `json:"tax_if_rounded_per_line"`
}

// Result は計算結果。
type Result struct {
	Mode            Mode          `json:"mode"`
	Rounding        Rounding      `json:"rounding"`
	RoundingLabel   string        `json:"rounding_label"`
	ByRate          []RateSummary `json:"by_rate"`
	TotalExcluded   int64         `json:"total_excluding_tax"`
	TotalTax        int64         `json:"total_tax"`
	TotalIncluded   int64         `json:"total_including_tax"`
	PerLineTotalTax int64         `json:"total_tax_if_rounded_per_line"`
	PerLineDiffers  bool          `json:"per_line_rounding_differs"`
	Explanation     string        `json:"explanation"`
}

// Calculate は税率ごとに合計し、1回だけ端数処理して税額を出す。
func Calculate(lines []Line, mode Mode, r Rounding) (Result, error) {
	res := Result{Mode: mode, Rounding: r, RoundingLabel: r.Label(), ByRate: []RateSummary{}}
	if len(lines) == 0 {
		return res, fmt.Errorf("明細が1行もありません")
	}
	if len(lines) > 10000 {
		return res, fmt.Errorf("明細は1万行までです")
	}
	sums := map[int]*RateSummary{}
	for i, l := range lines {
		if !ValidRate(l.Rate) {
			return res, fmt.Errorf("%d行目: 税率 %d%% は扱えません（10・8・0 のどれか）", i+1, l.Rate)
		}
		a, err := l.LineAmount()
		if err != nil {
			return res, fmt.Errorf("%d行目: %w", i+1, err)
		}
		s := sums[l.Rate]
		if s == nil {
			s = &RateSummary{Rate: l.Rate, RateLabel: RateLabel(l.Rate)}
			sums[l.Rate] = s
		}
		s.Lines++
		s.Subtotal += a
		s.PerLineTax += TaxOn(a, l.Rate, mode, r)
	}
	rates := make([]int, 0, len(sums))
	for k := range sums {
		rates = append(rates, k)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(rates)))
	for _, k := range rates {
		s := sums[k]
		if s.Subtotal > MaxAmount*10 || s.Subtotal < -MaxAmount*10 {
			return res, fmt.Errorf("税率 %d%% の合計が大きすぎます", k)
		}
		s.Tax = TaxOn(s.Subtotal, k, mode, r)
		if mode == Inclusive {
			s.TaxIncluded = s.Subtotal
			s.TaxExcluded = s.Subtotal - s.Tax
		} else {
			s.TaxExcluded = s.Subtotal
			s.TaxIncluded = s.Subtotal + s.Tax
		}
		res.ByRate = append(res.ByRate, *s)
		res.TotalExcluded += s.TaxExcluded
		res.TotalTax += s.Tax
		res.TotalIncluded += s.TaxIncluded
		res.PerLineTotalTax += s.PerLineTax
	}
	res.PerLineDiffers = res.PerLineTotalTax != res.TotalTax
	var b strings.Builder
	fmt.Fprintf(&b, "税率ごとに%sの金額を合計し、税額の端数処理（%s）を1回だけ行いました。", map[Mode]string{Exclusive: "税抜", Inclusive: "税込"}[mode], r.Label())
	if res.PerLineDiffers {
		diff := res.PerLineTotalTax - res.TotalTax
		way := "多く"
		if diff < 0 {
			way, diff = "少なく", -diff
		}
		fmt.Fprintf(&b, "品目ごとに端数処理して足すと税額は %d円 で、%d円%sなります（適格請求書では税率ごとに1回の端数処理）。", res.PerLineTotalTax, diff, way)
	}
	res.Explanation = b.String()
	return res, nil
}

// MatchingRoundings は、税率ごとの対価の額 subtotal に対して税額 tax を返す端数処理の一覧。
func MatchingRoundings(subtotal int64, rate int, mode Mode, tax int64) []Rounding {
	var out []Rounding
	for _, r := range []Rounding{Floor, Round, Ceil} {
		if TaxOn(subtotal, rate, mode, r) == tax {
			out = append(out, r)
		}
	}
	return out
}
