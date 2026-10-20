// Package textnorm は日本語の表記ゆれを正規化する。
//
//   - 全角の英数字・記号を半角に、半角カナを全角に（Unicode の NFKC）
//   - 数字・英字に挟まれた長音・ダッシュ・マイナス類を「-」に
//   - 空白のまとめ（連続を1つに、前後を削る）
//   - ひらがな⇔カタカナ（指定したとき）
//   - 住所：丁目・番地・番・号をハイフンに（「一丁目」など漢数字も）、郵便番号を「〒123-4567」に
//
// どの処理も、2回かけても結果が変わらない（冪等）ことをファズテストで確かめている。
package textnorm

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Mode は正規化の種類。
type Mode string

const (
	General Mode = "general"
	Address Mode = "address"
)

// Kana はかなの統一方法。
type Kana string

const (
	KeepKana Kana = "keep"
	Katakana Kana = "katakana"
	Hiragana Kana = "hiragana"
)

// Options は正規化の指定。
type Options struct {
	Mode Mode
	Kana Kana
}

// Result は正規化の結果。
type Result struct {
	Input   string   `json:"input"`
	Output  string   `json:"output"`
	Changed bool     `json:"changed"`
	Applied []string `json:"applied"`
}

// MaxInput は1回に扱う文字数の上限（バイト）。
const MaxInput = 100_000

// Normalize は文字列を正規化する。
func Normalize(s string, o Options) Result {
	res := Result{Input: s, Applied: []string{}}
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
		res.Applied = append(res.Applied, "壊れた文字を置き換え")
	}
	applied := map[string]bool{}
	step := func(label string, f func(string) string) {
		n := f(s)
		if n != s {
			if !applied[label] {
				applied[label] = true
				res.Applied = append(res.Applied, label)
			}
			s = n
		}
	}
	// 規則どうしが影響し合う（空白を詰めると「0ー0」が新しくできる など）ので、
	// 1回かけて変わらなくなるまで、すべての規則をくり返す（冪等にするため。数回で止まる）。
	for i := 0; i < 8; i++ {
		before := s
		step("全角英数字を半角・半角カナを全角に（NFKC）", norm.NFKC.String)
		step("数字の間の長音・ダッシュをハイフンに", unifyHyphens)
		switch o.Kana {
		case Katakana:
			step("ひらがなをカタカナに", toKatakana)
		case Hiragana:
			step("カタカナをひらがなに", toHiragana)
		}
		if o.Mode == Address {
			step("住所の空白を詰める", squeezeAddressSpaces)
			step("漢数字の丁目・番地・号を算用数字に", addressKanjiNumbers)
			step("丁目・番地・号をハイフンに", addressHyphens)
			step("郵便番号を 〒123-4567 の形に", postalCode)
		} else {
			step("空白をまとめる", collapseSpaces)
		}
		if s == before {
			break
		}
	}
	res.Output = s
	res.Changed = res.Input != res.Output
	return res
}

func isASCIIAlnum(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

// hyphenLike は、数字の間ではハイフンのつもりで打たれることの多い文字。
var hyphenLike = map[rune]bool{
	'‐': true, '‑': true, '‒': true, '–': true, '—': true, '―': true, '−': true,
	'ー': true, '─': true, '━': true, '﹣': true, '⁃': true, '˗': true,
}

func unifyHyphens(s string) string {
	rs := []rune(s)
	changed := false
	for i := 1; i+1 < len(rs); i++ {
		if hyphenLike[rs[i]] && isASCIIAlnum(rs[i-1]) && isASCIIAlnum(rs[i+1]) {
			rs[i] = '-'
			changed = true
		}
	}
	if !changed {
		return s
	}
	return string(rs)
}

func collapseSpaces(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// squeezeAddressSpaces は住所の空白を消す。英数字どうしの間（建物名など）だけ1つ残す。
func squeezeAddressSpaces(s string) string {
	rs := []rune(collapseSpaces(s))
	var b strings.Builder
	for i, r := range rs {
		if r == ' ' && i > 0 && i+1 < len(rs) && isASCIIAlnum(rs[i-1]) && isASCIIAlnum(rs[i+1]) {
			// 「1-2-3 ABC」の数字と英字の間は区切りとして残す
			b.WriteRune(r)
			continue
		}
		if r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func toKatakana(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'ぁ' && r <= 'ゖ') || r == 'ゝ' || r == 'ゞ' {
			return r + 0x60
		}
		return r
	}, s)
}

func toHiragana(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'ァ' && r <= 'ヶ') || r == 'ヽ' || r == 'ヾ' {
			return r - 0x60
		}
		return r
	}, s)
}

const kanjiDigits = "〇一二三四五六七八九十百千"

var reAddrKanji = regexp.MustCompile(`([` + kanjiDigits + `]+)(丁目|番地|番|号)`)

// addressKanjiNumbers は丁目・番地・番・号の直前の漢数字を算用数字にする。
// 「一番町」「国道一号線」のような地名・道路名は、後ろの文字を見て変えない。
func addressKanjiNumbers(s string) string {
	idx := reAddrKanji.FindAllStringSubmatchIndex(s, -1)
	if idx == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for _, m := range idx {
		num, unit := s[m[2]:m[3]], s[m[4]:m[5]]
		next := s[m[1]:]
		if !unitApplies(unit, next) {
			continue
		}
		v, ok := KanjiToInt(num)
		if !ok {
			continue
		}
		b.WriteString(s[last:m[2]])
		b.WriteString(strconv.Itoa(v))
		b.WriteString(unit)
		last = m[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// unitApplies は、その「丁目・番地・番・号」が住所の区切りとして使われているかを後ろの文字で判断する。
func unitApplies(unit, next string) bool {
	r, _ := utf8.DecodeRuneInString(next)
	switch unit {
	case "丁目":
		return true
	case "番地":
		return true
	case "番":
		// 「1番2号」「1番」「1番の2」はよい。「一番町」「5番館」は地名・建物名なので変えない。
		return next == "" || r == 'の' || r == 'ノ' || unicode.IsDigit(r) || strings.ContainsRune(kanjiDigits, r) || r == ' ' || r == '-'
	case "号":
		// 「号室」「号線」「号館」「号棟」は変えない。
		return !strings.ContainsRune("室線館棟車機", r)
	}
	return false
}

var (
	reChome  = regexp.MustCompile(`(\d+)丁目\s*(\d)`)
	reBanchi = regexp.MustCompile(`(\d+)番地?(?:の|ノ)?\s*(\d)`)
	reBanEnd = regexp.MustCompile(`(\d+)番地?$`)
	// 「3000番地（市役所内）」「12番地 ○○ビル」：番地のあとに数字が続かないときは「番地」を外す
	reBanchiTail = regexp.MustCompile(`(\d+)番地([^0-9のノ]|$)`)
	// 「117番地Ａ－22」のように英字の部屋番号が続くときは、つながらないよう空白で区切る
	reUnitAlpha = regexp.MustCompile(`(\d+)(?:番地|号)([A-Za-z])`)
	reGo        = regexp.MustCompile(`(\d+)号([^室線館棟車機0-9]|$)`)
	reGoNum     = regexp.MustCompile(`(\d+)号(\d)`)
	rePostal    = regexp.MustCompile(`^(?:〒\s*(\d{3})-?(\d{4})|(\d{3})-(\d{4}))(\D.*)?$`)
)

func addressHyphens(s string) string {
	for {
		n := reChome.ReplaceAllString(s, "$1-$2")
		n = reBanchi.ReplaceAllString(n, "$1-$2")
		n = reBanEnd.ReplaceAllString(n, "$1")
		n = reUnitAlpha.ReplaceAllString(n, "$1 $2")
		n = reBanchiTail.ReplaceAllString(n, "$1$2")
		n = reGo.ReplaceAllString(n, "$1$2")
		// 「10号 301号室」の「号」を消すと数字がつながるので、空白で区切る
		n = reGoNum.ReplaceAllString(n, "$1 $2")
		if n == s {
			return s
		}
		s = n
	}
}

// postalCode は先頭の郵便番号（「〒」付き、または「123-4567」の形）を「〒123-4567」にし、後ろに空白を1つ置く。
func postalCode(s string) string {
	m := rePostal.FindStringSubmatch(s)
	if m == nil {
		return s
	}
	a, b := m[1], m[2]
	if a == "" {
		a, b = m[3], m[4]
	}
	out := "〒" + a + "-" + b
	if rest := strings.TrimLeft(m[5], " "); rest != "" {
		out += " " + rest
	}
	return out
}

// KanjiToInt は 0〜9999 の漢数字を整数にする。「二十三」「百五」「千二百」のような十・百・千を使う書き方と、
// 「二〇二六」のような位取りの書き方の両方を読む。
func KanjiToInt(k string) (int, bool) {
	digit := map[rune]int{'〇': 0, '一': 1, '二': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	unit := map[rune]int{'十': 10, '百': 100, '千': 1000}
	rs := []rune(k)
	if len(rs) == 0 {
		return 0, false
	}
	hasUnit := false
	for _, r := range rs {
		if _, ok := unit[r]; ok {
			hasUnit = true
		} else if _, ok := digit[r]; !ok {
			return 0, false
		}
	}
	if !hasUnit {
		if len(rs) > 4 {
			return 0, false
		}
		v := 0
		for _, r := range rs {
			v = v*10 + digit[r]
		}
		return v, true
	}
	total, cur, lastUnit := 0, -1, 10000
	for _, r := range rs {
		if d, ok := digit[r]; ok {
			if cur != -1 || d == 0 {
				return 0, false // 「二三十」「〇十」のような書き方は読まない
			}
			cur = d
			continue
		}
		u := unit[r]
		if u >= lastUnit {
			return 0, false // 「十百」のように位が大きくなる並びは読まない
		}
		if cur == -1 {
			cur = 1
		}
		total += cur * u
		cur, lastUnit = -1, u
	}
	if cur != -1 {
		total += cur
	}
	return total, true
}
