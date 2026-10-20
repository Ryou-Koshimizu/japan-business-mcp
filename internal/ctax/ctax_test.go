package ctax

import "testing"

func i64(v int64) *int64 { return &v }

func TestTaxOn(t *testing.T) {
	cases := []struct {
		a    int64
		rate int
		mode Mode
		r    Rounding
		want int64
	}{
		{1000, 10, Exclusive, Floor, 100},
		{1005, 10, Exclusive, Floor, 100},
		{1005, 10, Exclusive, Round, 101},
		{1004, 10, Exclusive, Round, 100},
		{1001, 10, Exclusive, Ceil, 101},
		{1100, 10, Inclusive, Floor, 100},
		{1080, 8, Inclusive, Floor, 80},
		{999, 10, Inclusive, Floor, 90},
		{999, 10, Inclusive, Round, 91},
		{999, 10, Inclusive, Ceil, 91},
		{-105, 10, Exclusive, Floor, -10}, // 値引きは絶対値で処理
		{12345, 0, Exclusive, Ceil, 0},
		{MaxAmount, 10, Exclusive, Floor, MaxAmount / 10},
	}
	for _, c := range cases {
		if got := TaxOn(c.a, c.rate, c.mode, c.r); got != c.want {
			t.Errorf("TaxOn(%d,%d,%s,%s)=%d want %d", c.a, c.rate, c.mode, c.r, got, c.want)
		}
	}
}

func TestCalculateOncePerRate(t *testing.T) {
	res, err := Calculate([]Line{
		{Description: "部品A", Amount: 105, Rate: 10},
		{Description: "部品B", Amount: 105, Rate: 10},
		{Description: "お茶", UnitPrice: 130, Quantity: 3, Rate: 8},
		{Description: "切手", Amount: 84, Rate: 0},
	}, Exclusive, Floor)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ByRate) != 3 || res.ByRate[0].Rate != 10 || res.ByRate[1].Rate != 8 || res.ByRate[2].Rate != 0 {
		t.Fatalf("税率の並び: %+v", res.ByRate)
	}
	ten := res.ByRate[0]
	if ten.Subtotal != 210 || ten.Tax != 21 || ten.PerLineTax != 20 {
		t.Fatalf("10%%: %+v", ten)
	}
	eight := res.ByRate[1]
	if eight.Subtotal != 390 || eight.Tax != 31 || eight.TaxIncluded != 421 {
		t.Fatalf("8%%: %+v", eight)
	}
	if res.TotalTax != 52 || res.TotalIncluded != 210+21+390+31+84 || !res.PerLineDiffers {
		t.Fatalf("合計: %+v", res)
	}
}

func TestCalculateInclusive(t *testing.T) {
	res, err := Calculate([]Line{{Amount: 550, Rate: 10}, {Amount: 550, Rate: 10}, {Amount: 324, Rate: 8}}, Inclusive, Floor)
	if err != nil {
		t.Fatal(err)
	}
	if res.ByRate[0].Tax != 100 || res.ByRate[0].TaxExcluded != 1000 || res.ByRate[1].Tax != 24 || res.ByRate[1].TaxExcluded != 300 {
		t.Fatalf("%+v", res.ByRate)
	}
}

func TestCalculateErrors(t *testing.T) {
	bad := [][]Line{
		nil,
		{{Amount: 100, Rate: 5}},
		{{Amount: 100, UnitPrice: 10, Quantity: 2, Rate: 10}},
		{{Amount: MaxAmount + 1, Rate: 10}},
		{{UnitPrice: MaxAmount, Quantity: 2, Rate: 10}},
		{{UnitPrice: 10, Quantity: -1, Rate: 10}},
	}
	for i, ls := range bad {
		if _, err := Calculate(ls, Exclusive, Floor); err == nil {
			t.Errorf("%d: エラーになるはず", i)
		}
	}
}

func TestParse(t *testing.T) {
	for in, want := range map[string]Rounding{"": Floor, "切り捨て": Floor, "四捨五入": Round, "ceil": Ceil, "切り上げ": Ceil} {
		if got, err := ParseRounding(in); err != nil || got != want {
			t.Errorf("%q → %s %v", in, got, err)
		}
	}
	if _, err := ParseRounding("銀行丸め"); err == nil {
		t.Error("未知の端数処理はエラー")
	}
	for in, want := range map[string]Mode{"": Exclusive, "税込": Inclusive, "内税": Inclusive, "外税": Exclusive} {
		if got, err := ParseMode(in); err != nil || got != want {
			t.Errorf("%q → %s %v", in, got, err)
		}
	}
}

func goodInvoice() Invoice {
	return Invoice{
		IssuerName:         "株式会社サンプル商事",
		RegistrationNumber: "T7000012050002",
		TransactionDate:    "2026-10-20",
		RecipientName:      "株式会社取引先",
		Items: []InvoiceItem{
			{Description: "事務用品", Amount: 105, Rate: 10},
			{Description: "事務用品", Amount: 105, Rate: 10},
			{Description: "お茶（軽減税率対象）", Amount: 390, Rate: 8, ReducedMark: true},
		},
		RateTotals: []InvoiceRateTotal{{Rate: 10, Total: i64(210), Tax: i64(21)}, {Rate: 8, Total: i64(390), Tax: i64(31)}},
	}
}

func TestCheckInvoiceOK(t *testing.T) {
	res, err := CheckInvoice(goodInvoice())
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Problems != 0 {
		t.Fatalf("%+v", res)
	}
}

func TestCheckInvoiceProblems(t *testing.T) {
	cases := []struct {
		name  string
		mod   func(*Invoice)
		item  string
		count int
	}{
		{"登録番号なし", func(i *Invoice) { i.RegistrationNumber = "" }, "登録番号", 1},
		{"登録番号の検査数字", func(i *Invoice) { i.RegistrationNumber = "T7000012050003" }, "登録番号", 1},
		{"T抜け", func(i *Invoice) { i.RegistrationNumber = "7000012050002" }, "登録番号", 1},
		{"宛名なし", func(i *Invoice) { i.RecipientName = "" }, "交付を受ける事業者の氏名又は名称", 1},
		{"軽減の旨なし", func(i *Invoice) { i.Items[2].ReducedMark = false }, "軽減税率の対象品目である旨", 1},
		{"品目ごとの端数処理", func(i *Invoice) { i.RateTotals[0].Tax = i64(20) }, "10% 対象の消費税額", 1},
		{"合計の不一致", func(i *Invoice) { i.RateTotals[1].Total = i64(400) }, "8% 対象の合計", 1},
		{"日付なし", func(i *Invoice) { i.TransactionDate = "" }, "取引年月日", 1},
		{"名称なし", func(i *Invoice) { i.IssuerName = " " }, "発行事業者の氏名又は名称", 1},
		{"税率ごとの記載なし", func(i *Invoice) { i.RateTotals = nil }, "8% 対象の合計と税額", 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inv := goodInvoice()
			c.mod(&inv)
			res, err := CheckInvoice(inv)
			if err != nil {
				t.Fatal(err)
			}
			if res.OK || res.Problems != c.count {
				t.Fatalf("problems=%d want %d: %+v", res.Problems, c.count, res.Findings)
			}
			found := false
			for _, f := range res.Findings {
				if f.Item == c.item && (f.Status == "missing" || f.Status == "error") {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s の指摘が無い: %+v", c.item, res.Findings)
			}
		})
	}
}

func TestCheckSimplified(t *testing.T) {
	inv := goodInvoice()
	inv.Kind = Simplified
	inv.RecipientName = ""
	inv.RateTotals[0].Tax = nil // 適用税率があれば税額は省略できる
	res, err := CheckInvoice(inv)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("%+v", res.Findings)
	}
	inv.TransactionDate = "2026年10月分"
	if res, _ := CheckInvoice(inv); !res.OK {
		t.Fatalf("期間の書き方: %+v", res.Findings)
	}
	inv.Kind = "receipt"
	if _, err := CheckInvoice(inv); err == nil {
		t.Fatal("未知の種類はエラー")
	}
}
