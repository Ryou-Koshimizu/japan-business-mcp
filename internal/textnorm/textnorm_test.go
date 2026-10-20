package textnorm

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeTable(t *testing.T) {
	cases := []struct {
		name string
		in   string
		opt  Options
		want string
	}{
		{"全角英数字", "ＡＢＣ１２３", Options{}, "ABC123"},
		{"半角カナ濁点", "ｶﾞｷﾞｸﾞｹﾞｺﾞ ﾊﾟ", Options{}, "ガギグゲゴ パ"},
		{"全角記号", "（株）＆＠", Options{}, "(株)&@"},
		{"丸付き・合字", "①㈱", Options{}, "1(株)"},
		{"電話番号のゆれ", "０３ー１２３４ー５６７８", Options{}, "03-1234-5678"},
		{"マイナス記号", "03−1234−5678", Options{}, "03-1234-5678"},
		{"長音はそのまま", "コーヒー・データー", Options{}, "コーヒー・データー"},
		{"空白のまとめ", "  山田　　太郎  ", Options{}, "山田 太郎"},
		{"カタカナへ", "とうきょう　タワー", Options{Kana: Katakana}, "トウキョウ タワー"},
		{"ひらがなへ", "トウキョウ", Options{Kana: Hiragana}, "とうきょう"},
		{"住所 丁目番地号", "東京都千代田区千代田１丁目１番１号", Options{Mode: Address}, "東京都千代田区千代田1-1-1"},
		{"住所 漢数字", "大阪府大阪市北区梅田三丁目一番三号 ノースゲートビル", Options{Mode: Address}, "大阪府大阪市北区梅田3-1-3ノースゲートビル"},
		{"住所 番地の", "鳥取県岩美郡岩美町大字浦富６７５番地の１", Options{Mode: Address}, "鳥取県岩美郡岩美町大字浦富675-1"},
		{"住所 一番町は地名", "東京都千代田区一番町１０番地", Options{Mode: Address}, "東京都千代田区一番町10"},
		{"住所 号室・号線", "横浜市中区本町二丁目１０号 ３０１号室", Options{Mode: Address}, "横浜市中区本町2-10 301号室"},
		{"住所 郵便番号", "〒１００ー０００１　東京都千代田区千代田１−１", Options{Mode: Address}, "〒100-0001 東京都千代田区千代田1-1"},
		{"住所 郵便番号ハイフン無し", "〒1000001東京都", Options{Mode: Address}, "〒100-0001 東京都"},
		{"住所 二十三番地", "京都市中京区二十三番地", Options{Mode: Address}, "京都市中京区23"},
		{"住所 丁目のみ", "東京都新宿区四谷三丁目", Options{Mode: Address}, "東京都新宿区四谷3丁目"},
		{"住所 英字建物", "港区六本木６丁目１０番１号 Roppongi Hills", Options{Mode: Address}, "港区六本木6-10-1 Roppongi Hills"},
		{"国道は変えない", "国道一号線", Options{Mode: Address}, "国道一号線"},
		{"番地のあとに括弧", "上道町３０００番地（境港市役所内）", Options{Mode: Address}, "上道町3000(境港市役所内)"},
		{"番地のあとに英字の部屋番号", "倉吉市昭和町２丁目１１７番地Ａ－２２", Options{Mode: Address}, "倉吉市昭和町2-117 A-22"},
		{"号のあとに部屋番号", "米子市皆生温泉２丁目８番２号１０２号", Options{Mode: Address}, "米子市皆生温泉2-8-2 102"},
		{"空白を詰めてできた数字の間の長音", "0ー 0000000", Options{Mode: Address}, "0-0000000"},
		{"番館は変えない", "本町５番館", Options{Mode: Address}, "本町5番館"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Normalize(c.in, c.opt)
			if got.Output != c.want {
				t.Fatalf("\n in:   %q\n got:  %q\n want: %q\n applied: %v", c.in, got.Output, c.want, got.Applied)
			}
			if got.Changed != (c.in != c.want) {
				t.Fatalf("Changed=%v", got.Changed)
			}
		})
	}
}

func TestKanjiToInt(t *testing.T) {
	cases := map[string]int{"一": 1, "十": 10, "十一": 11, "二十": 20, "二十三": 23, "百": 100, "百五": 105, "三百二十一": 321, "千": 1000, "千二百": 1200, "二〇二六": 2026, "〇": 0}
	for in, want := range cases {
		if got, ok := KanjiToInt(in); !ok || got != want {
			t.Errorf("%s: got %d,%v want %d", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "二三十", "十百", "〇十", "一二三四五", "あ"} {
		if v, ok := KanjiToInt(bad); ok {
			t.Errorf("%q は読めないはず（%d）", bad, v)
		}
	}
}

func FuzzNormalizeIdempotent(f *testing.F) {
	for _, s := range []string{
		"東京都千代田区千代田１丁目１番１号", "ｶﾞｷﾞ", "１ー２ー３", "〒1000001東京", "国道一号線", "が",
		"一番町", "二十三番地の四", "1番2号3号室", "\xff", " \t\n", "ＡＢＣ　ｄｅｆ", "1 2 3", "百千番",
	} {
		f.Add(s, true, uint8(0))
		f.Add(s, false, uint8(1))
	}
	f.Fuzz(func(t *testing.T, s string, addr bool, kana uint8) {
		if len(s) > 2000 {
			return
		}
		o := Options{Mode: General, Kana: []Kana{KeepKana, Katakana, Hiragana}[int(kana)%3]}
		if addr {
			o.Mode = Address
		}
		r1 := Normalize(s, o)
		if !utf8.ValidString(r1.Output) {
			t.Fatalf("出力が UTF-8 でない: %q", r1.Output)
		}
		r2 := Normalize(r1.Output, o)
		if r2.Output != r1.Output {
			t.Fatalf("冪等でない\n in:  %q\n 1回: %q\n 2回: %q\n applied: %v", s, r1.Output, r2.Output, r2.Applied)
		}
		if o.Mode == General && strings.Contains(r1.Output, "  ") {
			t.Fatalf("空白が2つ続いている: %q", r1.Output)
		}
	})
}
