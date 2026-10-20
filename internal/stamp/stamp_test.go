package stamp

import "testing"

func TestCalculate(t *testing.T) {
	cases := []struct {
		name string
		in   Input
		want int64
	}{
		{"5万円未満", Input{Amount: 49_999, SalesProceeds: true}, 0},
		{"5万円", Input{Amount: 50_000, SalesProceeds: true}, 200},
		{"100万円", Input{Amount: 1_000_000, SalesProceeds: true}, 200},
		{"100万1円", Input{Amount: 1_000_001, SalesProceeds: true}, 400},
		{"200万円", Input{Amount: 2_000_000, SalesProceeds: true}, 400},
		{"300万円", Input{Amount: 3_000_000, SalesProceeds: true}, 600},
		{"500万円", Input{Amount: 5_000_000, SalesProceeds: true}, 1_000},
		{"1千万円", Input{Amount: 10_000_000, SalesProceeds: true}, 2_000},
		{"2千万円", Input{Amount: 20_000_000, SalesProceeds: true}, 4_000},
		{"3千万円", Input{Amount: 30_000_000, SalesProceeds: true}, 6_000},
		{"5千万円", Input{Amount: 50_000_000, SalesProceeds: true}, 10_000},
		{"1億円", Input{Amount: 100_000_000, SalesProceeds: true}, 20_000},
		{"2億円", Input{Amount: 200_000_000, SalesProceeds: true}, 40_000},
		{"3億円", Input{Amount: 300_000_000, SalesProceeds: true}, 60_000},
		{"5億円", Input{Amount: 500_000_000, SalesProceeds: true}, 100_000},
		{"10億円", Input{Amount: 1_000_000_000, SalesProceeds: true}, 150_000},
		{"10億円超", Input{Amount: 1_000_000_001, SalesProceeds: true}, 200_000},
		{"税額を区分（税抜5万円）", Input{Amount: 55_000, TaxStated: true, ConsumptionTax: 5_000, SalesProceeds: true}, 200},
		{"税額を区分（税抜5万円未満）", Input{Amount: 54_999, TaxStated: true, ConsumptionTax: 5_000, SalesProceeds: true}, 0},
		{"税額を区分しない", Input{Amount: 54_999, SalesProceeds: true}, 200},
		{"税込110万・区分", Input{Amount: 1_100_000, TaxStated: true, ConsumptionTax: 100_000, SalesProceeds: true}, 200},
		{"電子の領収書", Input{Amount: 10_000_000, SalesProceeds: true, Electronic: true}, 0},
		{"営業に関しない", Input{Amount: 10_000_000, SalesProceeds: true, NonBusiness: true}, 0},
		{"売上代金以外", Input{Amount: 10_000_000}, 200},
		{"売上代金以外 5万円未満", Input{Amount: 40_000}, 0},
		{"金額の記載なし", Input{AmountUnstated: true, SalesProceeds: true}, 200},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := Calculate(c.in)
			if err != nil {
				t.Fatal(err)
			}
			if r.Stamp != c.want {
				t.Fatalf("got %d want %d (%s / %s)", r.Stamp, c.want, r.Bracket, r.Reason)
			}
			if r.Reason == "" {
				t.Fatal("理由が空")
			}
		})
	}
	if _, err := Calculate(Input{Amount: -1}); err == nil {
		t.Fatal("負の金額はエラー")
	}
	if _, err := Calculate(Input{Amount: 100, ConsumptionTax: 200}); err == nil {
		t.Fatal("税額が金額より大きいのはエラー")
	}
}
