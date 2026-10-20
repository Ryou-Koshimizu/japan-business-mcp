// Package corpnum は、法人番号（13桁）と適格請求書発行事業者の登録番号（T＋13桁）の
// 検査用数字を検証する。
//
// 計算方法は「法人番号の指定等に関する省令」第2条：
//
//	検査用数字 = 9 − ( Σ(Pn × Qn) を 9 で割った余り )
//	Pn: 基礎番号（下12桁）の最下位を1桁目としたときの n 桁目の数字
//	Qn: n が奇数のとき 1、偶数のとき 2
//
// 登録番号は「T」に13桁の数字を続けたもので、法人は法人番号をそのまま使う。
// 個人事業者などの13桁も同じ検査用数字の付け方で発番される。
package corpnum

import (
	"fmt"
	"strings"
	"unicode"
)

// Kind は入力の種類。
type Kind string

const (
	CorporateNumber    Kind = "corporate_number"    // 法人番号
	RegistrationNumber Kind = "registration_number" // 適格請求書発行事業者の登録番号
)

// Result は検証結果。
type Result struct {
	Input           string `json:"input"`
	Normalized      string `json:"normalized"`
	Kind            Kind   `json:"kind"`
	Valid           bool   `json:"valid"`
	CheckDigit      int    `json:"check_digit"`
	ExpectedCheck   int    `json:"expected_check_digit"`
	CorporateNumber string `json:"corporate_number,omitempty"`
	Message         string `json:"message"`
}

// CheckDigit は12桁の基礎番号から検査用数字（1〜9）を計算する。
func CheckDigit(base string) (int, error) {
	if len(base) != 12 {
		return 0, fmt.Errorf("基礎番号は12桁です（%d桁でした）", len(base))
	}
	sum := 0
	for n := 1; n <= 12; n++ {
		c := base[12-n]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("基礎番号に数字以外 %q が含まれています", c)
		}
		p := int(c - '0')
		q := 1
		if n%2 == 0 {
			q = 2
		}
		sum += p * q
	}
	return 9 - sum%9, nil
}

// normalize は全角・ハイフン・空白を取り除いた形にする。
func normalize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '０' && r <= '９':
			b.WriteRune(r - '０' + '0')
		case r == 'Ｔ' || r == 'ｔ' || r == 't':
			b.WriteRune('T')
		case r == '-' || r == '－' || r == 'ー' || r == '‐' || r == '−' || r == '–' || r == '—' || unicode.IsSpace(r):
			// 区切りは読み飛ばす
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Validate は法人番号または登録番号を検証する。「T」で始まれば登録番号として扱う。
// 形式の誤り（桁数・文字）は error、形式は合っていて検査用数字が合わないものは Valid=false で返す。
func Validate(input string) (Result, error) {
	n := normalize(input)
	res := Result{Input: input, Normalized: n, Kind: CorporateNumber}
	digits := n
	if strings.HasPrefix(n, "T") {
		res.Kind = RegistrationNumber
		digits = n[1:]
	}
	if len(digits) != 13 {
		return res, fmt.Errorf("%s は数字13桁です（%q は %d 文字）", label(res.Kind), input, len([]rune(digits)))
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return res, fmt.Errorf("%s に数字以外の文字 %q が含まれています", label(res.Kind), r)
		}
	}
	want, _ := CheckDigit(digits[1:])
	got := int(digits[0] - '0')
	res.CheckDigit, res.ExpectedCheck = got, want
	res.Valid = got == want
	if res.Kind == RegistrationNumber {
		res.CorporateNumber = digits
	}
	if res.Valid {
		res.Message = fmt.Sprintf("%s の検査用数字（先頭の %d）は正しい値です", label(res.Kind), got)
	} else {
		res.Message = fmt.Sprintf("%s の検査用数字が合いません（先頭が %d、計算上は %d）。写し間違いの可能性があります", label(res.Kind), got, want)
	}
	return res, nil
}

func label(k Kind) string {
	if k == RegistrationNumber {
		return "登録番号（T＋13桁）"
	}
	return "法人番号"
}
