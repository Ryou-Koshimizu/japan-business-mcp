package corpnum

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateTable(t *testing.T) {
	cases := []struct {
		in      string
		valid   bool
		kind    Kind
		wantErr bool
	}{
		// 国税庁の法人番号（法人番号公表サイトで公表されている番号）
		{"7000012050002", true, CorporateNumber, false},
		{"T7000012050002", true, RegistrationNumber, false},
		{"Ｔ７０００－０１２０－５０００２", true, RegistrationNumber, false},
		{"t 7000 0120 50002", true, RegistrationNumber, false},
		// トヨタ自動車株式会社の法人番号
		{"1180301018771", true, CorporateNumber, false},
		{"T1180301018771", true, RegistrationNumber, false},
		// 1桁だけ変えたもの
		{"7000012050003", false, CorporateNumber, false},
		{"T1180301018772", false, RegistrationNumber, false},
		{"6000012050002", false, CorporateNumber, false},
		// 形式の誤り
		{"T700001205000", false, RegistrationNumber, true},
		{"70000120500021", false, CorporateNumber, true},
		{"T70000120500A2", false, RegistrationNumber, true},
		{"", false, CorporateNumber, true},
		{"TT7000012050002", false, RegistrationNumber, true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			r, err := Validate(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
			if err != nil {
				return
			}
			if r.Valid != c.valid || r.Kind != c.kind {
				t.Fatalf("got valid=%v kind=%s, want %v %s (%s)", r.Valid, r.Kind, c.valid, c.kind, r.Message)
			}
		})
	}
}

// 省令の計算式を、和の式のまま独立に書き直したもので全数ではなく広く突き合わせる。
func TestCheckDigitAgainstFormula(t *testing.T) {
	for i := 0; i < 200000; i++ {
		base := fmt.Sprintf("%012d", uint64(i)*48271%1000000000000)
		got, err := CheckDigit(base)
		if err != nil {
			t.Fatal(err)
		}
		s := 0
		for n := 1; n <= 12; n++ {
			p := int(base[len(base)-n] - '0')
			q := 2 - n%2 // 奇数→1、偶数→2
			s += p * q
		}
		if want := 9 - s%9; got != want {
			t.Fatalf("%s: got %d want %d", base, got, want)
		}
		if got < 1 || got > 9 {
			t.Fatalf("検査用数字は1〜9のはず: %d", got)
		}
	}
}

// 1桁の書き間違いは、0と9の取り違えを除いて必ず見つかる（9で割った余りの性質）。
func TestSingleDigitErrorDetection(t *testing.T) {
	base := "000012050002"
	cd, _ := CheckDigit(base)
	num := fmt.Sprint(cd) + base
	for pos := 1; pos < 13; pos++ {
		for d := byte('0'); d <= '9'; d++ {
			if num[pos] == d {
				continue
			}
			mut := num[:pos] + string(d) + num[pos+1:]
			r, _ := Validate(mut)
			zeroNine := (num[pos] == '0' && d == '9') || (num[pos] == '9' && d == '0')
			if r.Valid && !zeroNine {
				t.Errorf("%s（%d桁目を %c に）を見逃した", mut, pos+1, d)
			}
			if !r.Valid && zeroNine {
				t.Errorf("%s: 0↔9 は検査用数字では区別できないはず", mut)
			}
		}
	}
}

func FuzzValidate(f *testing.F) {
	for _, s := range []string{"7000012050002", "T1180301018771", "Ｔ７０００", "", "T", "TTTT", "0000000000000", "\xff\xfe"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		r, err := Validate(s)
		if err != nil {
			return
		}
		digits := strings.TrimPrefix(r.Normalized, "T")
		if len(digits) != 13 {
			t.Fatalf("エラー無しで13桁でない: %q", r.Normalized)
		}
		cd, err := CheckDigit(digits[1:])
		if err != nil {
			t.Fatal(err)
		}
		if r.Valid != (int(digits[0]-'0') == cd) {
			t.Fatalf("Valid の判定が計算と食い違う: %q", s)
		}
		// 正規化した結果をもう一度通しても同じ結論
		r2, err := Validate(r.Normalized)
		if err != nil || r2.Valid != r.Valid || r2.Normalized != r.Normalized {
			t.Fatalf("正規化が安定しない: %q → %q", s, r.Normalized)
		}
	})
}

func FuzzCheckDigit(f *testing.F) {
	f.Add("000012050002")
	f.Add("180301018771")
	f.Fuzz(func(t *testing.T, base string) {
		cd, err := CheckDigit(base)
		if err != nil {
			return
		}
		r, err := Validate(fmt.Sprint(cd) + base)
		if err != nil || !r.Valid {
			t.Fatalf("計算した検査用数字を付けた番号が通らない: %d%s (%v)", cd, base, err)
		}
	})
}
