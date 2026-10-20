package server

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/testcases"
)

var update = flag.Bool("update", false, "ゴールデンファイルを書き直す")

func fixedNow() time.Time { return time.Date(2026, 10, 20, 12, 0, 0, 0, jst) }

func connect(t *testing.T, cfg Config) *mcp.ClientSession {
	t.Helper()
	if cfg.Now == nil {
		cfg.Now = fixedNow
	}
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := New(cfg).Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestToolsListed(t *testing.T) {
	cs := connect(t, Config{})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
		if tl.Description == "" || tl.Title == "" || tl.OutputSchema == nil || tl.Annotations == nil || !tl.Annotations.ReadOnlyHint {
			t.Errorf("%s: 説明・題・出力スキーマ・読み取り専用の印が要る", tl.Name)
		}
	}
	want := slices.Clone(ToolNames)
	slices.Sort(want)
	slices.Sort(names)
	if !slices.Equal(names, want) {
		t.Fatalf("道具の一覧: %v", names)
	}
	covered := map[string]bool{}
	for _, c := range testcases.Cases {
		covered[c.Tool] = true
	}
	for _, n := range ToolNames {
		if !covered[n] {
			t.Errorf("%s のゴールデン試験が無い", n)
		}
	}
}

func TestGolden(t *testing.T) {
	cs := connect(t, Config{})
	for _, c := range testcases.Cases {
		t.Run(c.Name, func(t *testing.T) {
			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: c.Tool, Arguments: c.Args})
			if err != nil {
				t.Fatal(err)
			}
			if res.IsError {
				t.Fatalf("道具がエラーを返した: %v", res.Content[0].(*mcp.TextContent).Text)
			}
			got := testcases.Canonical(t, res.StructuredContent)
			path := testcases.Path(c.Name)
			if *update {
				if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, got, 0o600); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path) // #nosec G304 -- 試験のゴールデンファイル
			if err != nil {
				t.Fatalf("%v（go test ./internal/server -update で作る）", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("ゴールデンと違う: %s\n--- got\n%s", path, got)
			}
			// テキストの内容（構造化出力を読まないクライアント向け）も同じ JSON
			var txt any
			if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &txt); err != nil {
				t.Fatalf("text content が JSON でない: %v", err)
			}
			if !bytes.Equal(testcases.Canonical(t, txt), got) {
				t.Fatal("text content と structuredContent が食い違う")
			}
		})
	}
}

func TestToolErrors(t *testing.T) {
	cs := connect(t, Config{})
	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"holidays_list", map[string]any{"year": 1900}, "対応範囲"},
		{"holidays_list", map[string]any{"from": "2026-01-01"}, "両方"},
		{"holidays_list", map[string]any{"from": "2026-01-01", "to": "2040-01-01"}, "10年以内"},
		{"business_day_check", map[string]any{"date": "2026-02-30"}, "存在しない"},
		{"business_day_check", map[string]any{"date": "2026-10-20", "calendar": map[string]any{"company_holidays": []string{"そのうち"}}}, "読めません"},
		{"business_day_add", map[string]any{"date": "2026-10-20", "days": 99999}, "大きすぎ"},
		{"business_day_count", map[string]any{"from": "2026-10-31", "to": "2026-10-01"}, "より前"},
		{"payment_due_date", map[string]any{"transaction_date": "2026-10-20", "closing_day": 30, "month_offset": 1, "pay_day": 31}, "締め日"},
		{"registration_number_validate", map[string]any{"numbers": []string{}}, "1〜1000件"},
		{"consumption_tax_calculate", map[string]any{"lines": []map[string]any{{"amount": 100, "rate": 5}}}, "税率"},
		{"consumption_tax_calculate", map[string]any{"rounding": "bank", "lines": []map[string]any{{"amount": 100, "rate": 10}}}, "端数処理"},
		{"qualified_invoice_check", map[string]any{"kind": "receipt"}, "standard"},
		{"withholding_tax_calculate", map[string]any{}, "amount"},
		{"withholding_tax_calculate", map[string]any{"amount": 1000, "category": "other"}, "general"},
		{"revenue_stamp_receipt", map[string]any{"amount": -1}, "範囲"},
		{"wareki_convert", map[string]any{"input": "平成31年5月1日"}, "令和元年5月1日"},
		{"text_normalize", map[string]any{"text": "x", "mode": "zip"}, "general"},
		{"text_normalize", map[string]any{"text": strings.Repeat("あ", 40000)}, "10万バイト"},
		{"business_day_add", map[string]any{"days": 1}, "date"}, // 必須の引数が無い（スキーマの検証）
	}
	for _, c := range cases {
		t.Run(c.tool+"_"+c.want, func(t *testing.T) {
			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: c.tool, Arguments: c.args})
			if err != nil {
				t.Fatal(err)
			}
			if !res.IsError {
				t.Fatalf("エラーになるはず: %s", testcases.Canonical(t, res.StructuredContent))
			}
			msg := res.Content[0].(*mcp.TextContent).Text
			if !strings.Contains(msg, c.want) {
				t.Fatalf("エラー文に %q が無い: %s", c.want, msg)
			}
		})
	}
}

func TestResources(t *testing.T) {
	cs := connect(t, Config{})
	ctx := context.Background()
	rl, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rl.Resources) != 3 {
		t.Fatalf("資料の数 %d", len(rl.Resources))
	}
	for _, r := range rl.Resources {
		rr, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: r.URI})
		if err != nil || len(rr.Contents) != 1 || !strings.HasPrefix(rr.Contents[0].Text, "# ") {
			t.Fatalf("%s: %v", r.URI, err)
		}
	}
	tl, err := cs.ListResourceTemplates(ctx, nil)
	if err != nil || len(tl.ResourceTemplates) != 1 {
		t.Fatalf("テンプレート: %v", err)
	}
	rr, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "jbiz://holidays/2026"})
	if err != nil {
		t.Fatal(err)
	}
	var hs []map[string]any
	if err := json.Unmarshal([]byte(rr.Contents[0].Text), &hs); err != nil || len(hs) != 18 {
		t.Fatalf("2026年の祝日・休日は18日: %d %v", len(hs), err)
	}
	if _, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "jbiz://holidays/1800"}); err == nil {
		t.Fatal("範囲外の年はエラー")
	}
}

func TestPrompts(t *testing.T) {
	cs := connect(t, Config{})
	ctx := context.Background()
	pl, err := cs.ListPrompts(ctx, nil)
	if err != nil || len(pl.Prompts) != 3 {
		t.Fatalf("%v %v", pl, err)
	}
	args := map[string]map[string]string{
		"invoice_review":    {"invoice_text": "請求書 T7000012050002"},
		"payment_schedule":  {"transactions": "A社 2026-10-20 100000", "terms": "月末締め翌月末払い"},
		"freelance_invoice": {"fee": "300000"},
	}
	for _, p := range pl.Prompts {
		r, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: p.Name, Arguments: args[p.Name]})
		if err != nil {
			t.Fatalf("%s: %v", p.Name, err)
		}
		txt := r.Messages[0].Content.(*mcp.TextContent).Text
		for _, v := range args[p.Name] {
			if !strings.Contains(txt, v) {
				t.Errorf("%s: 引数 %q が文面に入っていない", p.Name, v)
			}
		}
	}
}

func TestDefaultCalendarFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cal.json")
	if err := os.WriteFile(path, []byte(`{"company_holidays":["12-29..01-03","2026-10-21"],"work_days":["2026-10-24"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadCalendarFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cs := connect(t, Config{DefaultCalendar: c})
	for date, want := range map[string]bool{"2026-10-21": false, "2026-10-24": true, "2027-01-02": false, "2026-10-22": true} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "business_day_check", Arguments: map[string]any{"date": date}})
		if err != nil || res.IsError {
			t.Fatal(err, res)
		}
		var out map[string]any
		b, _ := json.Marshal(res.StructuredContent)
		_ = json.Unmarshal(b, &out)
		if out["is_business_day"] != want {
			t.Errorf("%s: %v", date, out)
		}
	}
	if err := os.WriteFile(path, []byte(`{"holidays":["12-29"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCalendarFile(path); err == nil {
		t.Fatal("知らない項目はエラー")
	}
	if _, err := LoadCalendarFile(filepath.Join(dir, "none.json")); err == nil {
		t.Fatal("無いファイルはエラー")
	}
}

func TestTodayDefault(t *testing.T) {
	cs := connect(t, Config{})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "holidays_list", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(b), `"from":"2026-01-01"`) {
		t.Fatalf("今日の年（2026）の一覧になるはず: %s", b)
	}
}
