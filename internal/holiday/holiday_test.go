package holiday

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"

	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/jpdate"
)

const csvSHA256 = "cec37a743c96995cdb9cb52b685c9003634682a9b0e1a640a6b9b96881fe964a"

// loadCAO は内閣府の CSV を読み、日付→名称の表にする。
func loadCAO(t testing.TB) (map[jpdate.Date]string, int, int) {
	t.Helper()
	raw, err := os.ReadFile("testdata/syukujitsu.csv")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != csvSHA256 {
		t.Fatalf("testdata/syukujitsu.csv の SHA-256 が SOURCE.md と違う: %s", got)
	}
	r := csv.NewReader(transform.NewReader(bytes.NewReader(raw), japanese.ShiftJIS.NewDecoder()))
	m := map[jpdate.Date]string{}
	minY, maxY := 9999, 0
	header := true
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header {
			header = false
			continue
		}
		d, err := jpdate.Parse(rec[0])
		if err != nil {
			t.Fatalf("CSV の日付 %q: %v", rec[0], err)
		}
		m[d] = rec[1]
		minY = min(minY, d.Year())
		maxY = max(maxY, d.Year())
	}
	return m, minY, maxY
}

// caoLabel は、こちらの名称を内閣府 CSV の表記に寄せる。
// 内閣府 CSV は振替休日と国民の休日をどちらも「休日」と書き、
// 2019年の特別法の日を「休日（祝日扱い）」、皇室の結婚の儀を「結婚の儀」と短く書く。
func caoLabel(h Holiday) string {
	switch {
	case h.Kind == Substitute || h.Kind == Citizens:
		return "休日"
	case h.Kind == Special && h.Date.Year() == 2019:
		return "休日（祝日扱い）"
	case h.Kind == Special && strings.HasSuffix(h.Name, "結婚の儀"):
		return "結婚の儀"
	case h.Kind == Special && strings.HasSuffix(h.Name, "大喪の礼"):
		return "大喪の礼"
	case h.Date.Equal(jpdate.MustNew(2019, time.October, 14)):
		// 2019年は体育の日。2020年にスポーツの日へ改称することが決まっていたため、内閣府は併記している。
		return "体育の日（スポーツの日）"
	}
	return h.Name
}

func TestMatchesCabinetOfficeCSV(t *testing.T) {
	cao, minY, maxY := loadCAO(t)
	if len(cao) != 1067 || minY != 1955 || maxY != 2027 {
		t.Fatalf("CSV の件数・範囲が想定と違う: %d件 %d〜%d", len(cao), minY, maxY)
	}
	got := map[jpdate.Date]Holiday{}
	for y := minY; y <= maxY; y++ {
		hs, err := Year(y)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range hs {
			got[h.Date] = h
		}
	}
	mismatch := 0
	for d, name := range cao {
		h, ok := got[d]
		if !ok {
			t.Errorf("%s %s が計算に無い", d, name)
			mismatch++
			continue
		}
		if l := caoLabel(h); l != name {
			t.Errorf("%s の名称: 計算=%q（表記合わせ後 %q） 内閣府=%q", d, h.Name, l, name)
			mismatch++
		}
	}
	for d, h := range got {
		if _, ok := cao[d]; !ok {
			t.Errorf("%s %s は内閣府 CSV に無い", d, h.Name)
			mismatch++
		}
	}
	if mismatch == 0 {
		t.Logf("内閣府 CSV の %d日（%d〜%d年）と1日ずつ一致", len(cao), minY, maxY)
	}
}

// 法律の条文と特例法から導ける日付。
func TestKnownDatesFromStatutes(t *testing.T) {
	cases := []struct {
		date string
		name string
		kind Kind
	}{
		{"1973-04-30", "振替休日", Substitute}, // 振替休日の規定（1973-04-12 施行）の最初の適用。4/29 が日曜
		{"1988-05-04", "国民の休日", Citizens},  // 国民の休日の規定（1985-12-27 施行）の最初の適用
		{"2007-04-30", "振替休日", Substitute}, // 昭和の日（新設）が日曜
		{"2008-05-06", "振替休日", Substitute}, // 2007年改正：5/4（日）の振替は 5/5 を飛ばして 5/6
		{"2009-09-22", "国民の休日", Citizens},  // シルバーウィーク
		{"2015-09-22", "国民の休日", Citizens},  //
		{"2016-08-11", "山の日", National},    // 山の日の新設
		{"2019-04-30", "国民の休日", Citizens},  // 天皇の即位の日（祝日とみなす）に挟まれた日
		{"2019-05-01", "天皇の即位の日", Special}, //
		{"2019-05-02", "国民の休日", Citizens},  //
		{"2019-10-22", "即位礼正殿の儀", Special}, //
		{"2020-02-23", "天皇誕生日", National},  // 日曜
		{"2020-02-24", "振替休日", Substitute}, //
		{"2020-07-23", "海の日", National},    // オリンピック特措法
		{"2020-07-24", "スポーツの日", National}, //
		{"2020-08-10", "山の日", National},    //
		{"2021-07-22", "海の日", National},    //
		{"2021-07-23", "スポーツの日", National}, //
		{"2021-08-08", "山の日", National},    //
		{"2021-08-09", "振替休日", Substitute}, //
		{"2026-05-06", "振替休日", Substitute}, // 5/3（日）憲法記念日 → 5/4・5/5 が祝日なので 5/6
		{"2026-09-22", "国民の休日", Citizens},  // 敬老の日 9/21 と 秋分の日 9/23 に挟まれた日
		{"2026-03-20", "春分の日", National},
		{"2026-09-23", "秋分の日", National},
	}
	for _, c := range cases {
		t.Run(c.date, func(t *testing.T) {
			d, err := jpdate.Parse(c.date)
			if err != nil {
				t.Fatal(err)
			}
			h, ok, err := Lookup(d)
			if err != nil || !ok {
				t.Fatalf("%s が祝日・休日になっていない（err=%v）", c.date, err)
			}
			if h.Name != c.name || h.Kind != c.kind {
				t.Fatalf("%s: got %s/%s want %s/%s", c.date, h.Name, h.Kind, c.name, c.kind)
			}
		})
	}
}

// 祝日では「ない」ことが法の改正経過から言える日。
func TestNotHolidays(t *testing.T) {
	for _, s := range []string{
		"2019-12-23", // 天皇誕生日は 2019年は無い（平成の天皇誕生日は2018年まで、令和は2020年から）
		"2019-02-23",
		"2020-10-12", // 2020年のスポーツの日は 7/24 に移動
		"2021-10-11", // 2021年も 7/23 に移動
		"2020-07-20", // 2020年の海の日は 7/23 に移動
		"1972-04-30", // 振替休日の規定の施行前（4/29 が日曜でも振替無し）
		"1984-05-04", // 国民の休日の規定の施行前
		"2026-01-02", // 法律上は平日
	} {
		d, _ := jpdate.Parse(s)
		if h, ok, _ := Lookup(d); ok {
			t.Errorf("%s は祝日ではないはず（計算: %s）", s, h.Name)
		}
	}
}

// 2028年以降（官報で未公表の年）の、規則としての性質。
func TestPropertiesFutureYears(t *testing.T) {
	for y := 2028; y <= MaxYear; y++ {
		hs, err := Year(y)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[jpdate.Date]bool{}
		names := map[string]int{}
		for _, h := range hs {
			if seen[h.Date] {
				t.Fatalf("%d: %s が重複", y, h.Date)
			}
			seen[h.Date] = true
			names[h.Name]++
			if h.Kind == Substitute && h.Date.Weekday() == time.Sunday {
				t.Errorf("%d: 振替休日 %s が日曜", y, h.Date)
			}
		}
		for _, n := range []string{"元日", "成人の日", "建国記念の日", "天皇誕生日", "春分の日", "昭和の日", "憲法記念日", "みどりの日", "こどもの日", "海の日", "山の日", "敬老の日", "秋分の日", "スポーツの日", "文化の日", "勤労感謝の日"} {
			if names[n] != 1 {
				t.Errorf("%d: %s が %d 回", y, n, names[n])
			}
		}
		for _, h := range hs {
			switch h.Name {
			case "成人の日", "海の日", "敬老の日", "スポーツの日":
				if h.Date.Weekday() != time.Monday {
					t.Errorf("%d: %s %s が月曜でない", y, h.Name, h.Date)
				}
			case "春分の日":
				if h.Date.Month() != time.March || h.Date.Day() < 19 || h.Date.Day() > 22 {
					t.Errorf("%d: 春分の日 %s", y, h.Date)
				}
			case "秋分の日":
				if h.Date.Month() != time.September || h.Date.Day() < 21 || h.Date.Day() > 24 {
					t.Errorf("%d: 秋分の日 %s", y, h.Date)
				}
			}
		}
		if !IsEstimate(y) {
			t.Errorf("%d は見込みの年のはず", y)
		}
	}
}

func TestOutOfRange(t *testing.T) {
	for _, y := range []int{1948, 2151, 0, -1} {
		if _, err := Year(y); err == nil {
			t.Errorf("%d年はエラーになるはず", y)
		}
	}
}

func TestYearReturnsCopy(t *testing.T) {
	a, _ := Year(2026)
	a[0].Name = "書き換え"
	b, _ := Year(2026)
	if b[0].Name != "元日" {
		t.Fatal("キャッシュが書き換えられた")
	}
}

func BenchmarkYear(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = compute(2026)
	}
}
