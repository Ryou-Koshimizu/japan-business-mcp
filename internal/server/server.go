// Package server は、日本の業務の道具を MCP のサーバーとしてまとめる。
// 標準入出力（stdio）と Streamable HTTP のどちらからも、同じ *mcp.Server を使う。
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/holiday"
	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/jpdate"
	"github.com/Ryou-Koshimizu/japan-business-mcp/internal/wareki"
)

// Name はサーバーの名前（MCP の serverInfo.name）。
const Name = "japan-business-mcp"

// Config はサーバーの設定。
type Config struct {
	// Version は serverInfo.version に入る。
	Version string
	// DefaultCalendar は、どの道具でも足される会社の休み（起動時の --calendar で読む）。
	DefaultCalendar CalendarOptions
	// Now は「今日」を決める時計（試験で固定するため）。nil なら time.Now。
	Now func() time.Time
}

type srv struct {
	cfg Config
}

var jst = time.FixedZone("JST", 9*60*60)

func (s *srv) today() jpdate.Date {
	now := time.Now
	if s.cfg.Now != nil {
		now = s.cfg.Now
	}
	return jpdate.FromTime(now().In(jst))
}

const instructions = `日本の実務で使う計算と確認の道具です。外部の API を呼ばず、法令の規則からその場で計算します。
- 祝日・営業日：holidays_list / business_day_check / business_day_add / business_day_count（会社の休みは calendar で足せる）
- 支払期日：payment_due_date（「月末締め翌月末払い」などを日付に）
- インボイス：registration_number_validate（T＋13桁・法人番号の検査用数字）、qualified_invoice_check（記載事項）、consumption_tax_calculate（税率ごとに1回の端数処理）
- 源泉徴収：withholding_tax_calculate（10.21%／20.42%、手取りからの逆算も）
- 印紙：revenue_stamp_receipt（領収書の収入印紙）
- 和暦：wareki_convert　表記ゆれ：text_normalize
日付は YYYY-MM-DD、金額は円の整数で渡してください。`

// dateSchema は jpdate.Date を JSON Schema の文字列（date 形式）として表す。
var schemaOpts = &jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
	reflect.TypeFor[jpdate.Date](): {Type: "string", Format: "date", Description: "日付（YYYY-MM-DD）"},
}}

func addTool[In, Out any](s *mcp.Server, name, title, desc string, h mcp.ToolHandlerFor[In, Out]) {
	in, err := jsonschema.For[In](schemaOpts)
	if err != nil {
		panic(fmt.Sprintf("%s の入力スキーマ: %v", name, err))
	}
	out, err := jsonschema.For[Out](schemaOpts)
	if err != nil {
		panic(fmt.Sprintf("%s の出力スキーマ: %v", name, err))
	}
	f := false
	mcp.AddTool(s, &mcp.Tool{
		Name:         name,
		Title:        title,
		Description:  desc,
		InputSchema:  in,
		OutputSchema: out,
		Annotations: &mcp.ToolAnnotations{
			Title:          title,
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  &f,
		},
	}, h)
}

// ToolNames は登録する道具の名前（README・遊び場・試験で共有）。
var ToolNames = []string{
	"holidays_list", "business_day_check", "business_day_add", "business_day_count",
	"payment_due_date", "registration_number_validate", "qualified_invoice_check",
	"consumption_tax_calculate", "withholding_tax_calculate", "revenue_stamp_receipt",
	"wareki_convert", "text_normalize",
}

// New は道具・資料・定型の依頼文をそろえた MCP サーバーを作る。
func New(cfg Config) *mcp.Server {
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	s := &srv{cfg: cfg}
	m := mcp.NewServer(&mcp.Implementation{Name: Name, Title: "日本の業務 MCP", Version: cfg.Version}, &mcp.ServerOptions{
		Instructions: instructions,
	})

	addTool(m, "holidays_list", "祝日の一覧",
		"日本の祝日・休日（振替休日・国民の休日・特別法の休日を含む）を一覧にする。year か from/to で範囲を指定。",
		s.holidaysList)
	addTool(m, "business_day_check", "営業日かどうか",
		"その日が営業日か、休みならその理由（土日・祝日の名前・会社の休み）と前後の営業日を返す。",
		s.businessDayCheck)
	addTool(m, "business_day_add", "n営業日後",
		"起点の日から n 営業日後（負なら前）の日付と、途中で飛ばした休みの一覧を返す。「3営業日以内に返信」「5営業日前に発注」などに使う。",
		s.businessDayAdd)
	addTool(m, "business_day_count", "期間の営業日数",
		"期間（両端を含む）の営業日数と、含まれる祝日・会社の休み・土日の数を返す。",
		s.businessDayCount)
	addTool(m, "payment_due_date", "支払期日",
		"「月末締め翌月末払い」「20日締め翌月10日払い」のような支払条件と取引日から、締め日と支払日を出す。支払日が休みなら前営業日（既定）か翌営業日に寄せる。",
		s.paymentDue)
	addTool(m, "registration_number_validate", "登録番号・法人番号の検査",
		"適格請求書発行事業者の登録番号（T＋13桁）と法人番号（13桁）の検査用数字を確かめる。1回で1000件まで。番号の写し間違いを見つける用途。",
		s.numberValidate)
	addTool(m, "qualified_invoice_check", "インボイスの記載事項チェック",
		"適格請求書（または適格簡易請求書）の記載事項がそろっているかを確かめる：発行者名・登録番号・取引日・取引内容と軽減税率の旨・税率ごとの合計と税額（税率ごとに1回の端数処理か）・宛名。",
		s.invoiceCheck)
	addTool(m, "consumption_tax_calculate", "消費税の計算",
		"明細から、税率（10%・8%）ごとに合計して1回だけ端数処理した消費税額を出す。品目ごとに端数処理した場合との差も示す。税抜・税込どちらの金額でも可。",
		s.consumptionTax)
	addTool(m, "withholding_tax_calculate", "源泉徴収税額",
		"報酬・料金の源泉徴収税額（100万円以下 10.21%、超える部分 20.42%。司法書士等は1万円を引いて10.21%）と支払額を出す。net_target で手取りから報酬額を逆算できる。",
		s.withholdingTax)
	addTool(m, "revenue_stamp_receipt", "領収書の印紙税",
		"領収書（第17号文書）に貼る収入印紙の額を出す。消費税額を区分して書くと税抜の金額で判定する。電子データで渡す領収書は不要。",
		s.revenueStamp)
	addTool(m, "wareki_convert", "和暦⇔西暦",
		"和暦（明治〜令和、元年表記・漢数字・R8.10.20 のような略記も可）と西暦を相互に変換する。改元の年は両方の元号を返す。",
		s.warekiConvert)
	addTool(m, "text_normalize", "表記ゆれの正規化",
		"全角英数字→半角、半角カナ→全角、数字の間の長音やダッシュ→ハイフン、空白のまとめ、ひらがな⇔カタカナ。mode=address で住所の丁目・番地・号をハイフンにし郵便番号を整える。",
		s.normalize)

	addResources(m)
	addPrompts(m)
	return m
}

func addResources(m *mcp.Server) {
	m.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "holidays",
		Title:       "その年の祝日（JSON）",
		URITemplate: "jbiz://holidays/{year}",
		MIMEType:    "application/json",
		Description: "指定した年（1949〜2150）の祝日・休日の一覧",
	}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI
		ys := strings.TrimPrefix(uri, "jbiz://holidays/")
		y, err := strconv.Atoi(ys)
		if err != nil {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		hs, err := holiday.Year(y)
		if err != nil {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		b, _ := json.MarshalIndent(hs, "", "  ")
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "application/json", Text: string(b)}}}, nil
	})

	text := func(uri, name, title, desc, body string) {
		m.AddResource(&mcp.Resource{URI: uri, Name: name, Title: title, Description: desc, MIMEType: "text/markdown"},
			func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "text/markdown", Text: body}}}, nil
			})
	}
	text("jbiz://reference/invoice-requirements", "invoice-requirements", "適格請求書の記載事項",
		"適格請求書・適格簡易請求書に書くこと（消費税法第57条の4）", invoiceRequirements)
	text("jbiz://reference/eras", "eras", "元号の一覧", "明治〜令和の始まりと終わりの日", erasTable())
	text("jbiz://reference/withholding", "withholding", "報酬の源泉徴収の早見", "税率と計算式", withholdingRef)
}

const invoiceRequirements = `# 適格請求書の記載事項（消費税法第57条の4第1項）

1. 適格請求書発行事業者の氏名又は名称及び登録番号（T＋13桁）
2. 課税資産の譲渡等を行った年月日
3. 課税資産の譲渡等に係る資産又は役務の内容（軽減税率の対象品目である旨）
4. 税率ごとに区分して合計した対価の額（税抜き又は税込み）及び適用税率
5. 税率ごとに区分した消費税額等
6. 書類の交付を受ける事業者の氏名又は名称

## 適格簡易請求書（同条第2項：小売業・飲食店業・タクシー業など）
- 6（宛名）は不要
- 4 の適用税率と 5 の消費税額等は、どちらか一方でよい

## 端数処理
- 1つの適格請求書につき、税率ごとに1回（消費税法施行令第70条の10）
- 切り捨て・四捨五入・切り上げのどれでもよい
`

const withholdingRef = `# 報酬・料金の源泉徴収（所得税＋復興特別所得税）

| 支払金額 | 税額 |
| --- | --- |
| 100万円以下 | 支払金額 × 10.21% |
| 100万円超 | (支払金額 − 100万円) × 20.42% + 102,100円 |
| 司法書士・土地家屋調査士・海事代理士 | (支払金額 − 1万円) × 10.21% |

- 1円未満の端数は切り捨て
- 請求書で消費税額が区分されていれば、税抜の金額を対象にできる
- 復興特別所得税は 2037年12月31日 までの支払いに加わる（2038年以降は 10% ／ 20%）
`

func erasTable() string {
	var b strings.Builder
	b.WriteString("# 元号の一覧\n\n| 元号 | 略記 | 始まり | 終わり |\n| --- | --- | --- | --- |\n")
	for _, e := range wareki.Eras {
		end := "（現在）"
		if !e.End.IsZero() {
			end = e.End.String()
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", e.Name, e.Alpha, e.Start, end)
	}
	b.WriteString("\n日付の変換は太陽暦を採用した 1873-01-01（明治6年1月1日）から。1年目は「元年」と書く。\n")
	return b.String()
}

func addPrompts(m *mcp.Server) {
	m.AddPrompt(&mcp.Prompt{
		Name:        "invoice_review",
		Title:       "請求書の点検",
		Description: "受け取った請求書の文面を、インボイスの記載事項・登録番号・税額の計算の順に点検する",
		Arguments:   []*mcp.PromptArgument{{Name: "invoice_text", Title: "請求書の文面", Description: "請求書から書き写した文字（OCR の結果でも可）", Required: true}},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		txt := req.Params.Arguments["invoice_text"]
		return &mcp.GetPromptResult{
			Description: "請求書の点検",
			Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: "次の請求書を点検してください。\n" +
				"1. text_normalize で全角・半角のゆれをそろえる\n" +
				"2. registration_number_validate で登録番号の検査用数字を確かめる\n" +
				"3. 明細を税率ごとに分けて consumption_tax_calculate で税額を計算し、請求書の税額と比べる\n" +
				"4. qualified_invoice_check で記載事項の不足を確かめる\n" +
				"最後に、問題点と直し方を箇条書きで短くまとめてください。\n\n---\n" + txt}}},
		}, nil
	})
	m.AddPrompt(&mcp.Prompt{
		Name:        "payment_schedule",
		Title:       "支払予定表",
		Description: "複数の取引と支払条件から、支払日の一覧を作る",
		Arguments: []*mcp.PromptArgument{
			{Name: "transactions", Title: "取引の一覧", Description: "取引先・取引日・金額を1行に1件", Required: true},
			{Name: "terms", Title: "支払条件", Description: "例：月末締め翌月末払い（休みなら前営業日）", Required: true},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		a := req.Params.Arguments
		return &mcp.GetPromptResult{
			Description: "支払予定表",
			Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: "支払条件「" + a["terms"] + "」で、次の取引の支払日を payment_due_date で1件ずつ求め、" +
				"支払日の早い順に「支払日・曜日・取引先・金額」の表にしてください。同じ日の支払いは合計も出してください。\n\n" + a["transactions"]}}},
		}, nil
	})
	m.AddPrompt(&mcp.Prompt{
		Name:        "freelance_invoice",
		Title:       "フリーランスへの支払額",
		Description: "報酬額から、消費税・源泉徴収税額・振込額を計算して説明する",
		Arguments:   []*mcp.PromptArgument{{Name: "fee", Title: "報酬額（税抜）", Description: "例：300000", Required: true}},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: "フリーランスへの支払額",
			Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: "税抜の報酬 " + req.Params.Arguments["fee"] + " 円について、" +
				"consumption_tax_calculate で消費税（10%）を、withholding_tax_calculate で源泉徴収税額（消費税は区分して記載する前提）を求め、" +
				"請求書に書く金額と振込額を表にしてください。"}}},
		}, nil
	})
}
