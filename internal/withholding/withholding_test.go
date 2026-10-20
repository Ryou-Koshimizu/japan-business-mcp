package withholding

import (
	"testing"
	"time"

	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/jpdate"
)

var d2026 = jpdate.MustNew(2026, time.October, 20)

func TestCalculate(t *testing.T) {
	cases := []struct {
		amount int64
		ctax   int64
		cat    Category
		date   jpdate.Date
		want   int64
		net    int64
	}{
		{100_000, 0, General, d2026, 10_210, 89_790},
		{100_000, 10_000, General, d2026, 10_210, 99_790},
		{33_333, 0, General, d2026, 3_403, 29_930},
		{1_000_000, 0, General, d2026, 102_100, 897_900},
		{1_000_001, 0, General, d2026, 102_100, 897_901}, // 1円 × 20.42% = 0.2042 → 切り捨て
		{1_500_000, 0, General, d2026, 204_200, 1_295_800},
		{10_000_000, 0, General, d2026, 1_939_900, 8_060_100},
		{0, 0, General, d2026, 0, 0},
		{50_000, 0, JudicialScrivener, d2026, 4_084, 45_916},
		{10_000, 0, JudicialScrivener, d2026, 0, 10_000},
		{100_000, 0, General, jpdate.MustNew(2038, time.January, 1), 10_000, 90_000}, // 復興特別所得税の終了後
		{2_000_000, 0, General, jpdate.MustNew(2038, time.January, 1), 300_000, 1_700_000},
		{100_000, 0, General, jpdate.MustNew(2037, time.December, 31), 10_210, 89_790},
		{100_000, 0, General, jpdate.MustNew(2012, time.December, 31), 10_000, 90_000}, // 開始前
	}
	for _, c := range cases {
		r, err := Calculate(Input{Amount: c.amount, ConsumptionTax: c.ctax, Category: c.cat, PaymentDate: c.date})
		if err != nil {
			t.Fatal(err)
		}
		if r.Withholding != c.want || r.NetPayment != c.net {
			t.Errorf("%d(%s,%s): got %d / %d want %d / %d (%s)", c.amount, c.cat, c.date, r.Withholding, r.NetPayment, c.want, c.net, r.Formula)
		}
	}
}

func TestCalculateErrors(t *testing.T) {
	for _, in := range []Input{{Amount: -1}, {Amount: 100, ConsumptionTax: 200}, {Amount: 100, Category: "hostess"}} {
		if _, err := Calculate(in); err == nil {
			t.Errorf("%+v はエラーになるはず", in)
		}
	}
}

func TestGrossForNet(t *testing.T) {
	// 99,999円 と 100,000円 はどちらも手取り 89,790円 になる。小さいほうを返す。
	exact, lo, hi, err := GrossForNet(89_790, General, d2026)
	if err != nil || !exact || lo != 99_999 || hi != 99_999 {
		t.Fatalf("got %v %d %d %v", exact, lo, hi, err)
	}
	// 全ての手取り額で、返した報酬額の手取りが条件を満たすこと
	for net := int64(1); net <= 3_000_000; net += 997 {
		exact, lo, hi, err := GrossForNet(net, General, d2026)
		if err != nil {
			t.Fatal(err)
		}
		nlo, _ := Calculate(Input{Amount: lo, PaymentDate: d2026})
		nhi, _ := Calculate(Input{Amount: hi, PaymentDate: d2026})
		if exact && nlo.NetPayment != net {
			t.Fatalf("%d: exact なのに %d", net, nlo.NetPayment)
		}
		if !exact && (nlo.NetPayment >= net || nhi.NetPayment <= net || hi != lo+1) {
			t.Fatalf("%d: 前後 %d→%d / %d→%d", net, lo, nlo.NetPayment, hi, nhi.NetPayment)
		}
	}
}
