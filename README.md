# japan-business-mcp — 日本の業務 MCP サーバー

[![CI](https://github.com/Ryou-Koshimizu/japan-business-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/Ryou-Koshimizu/japan-business-mcp/actions/workflows/ci.yml)
[![CodeQL](https://github.com/Ryou-Koshimizu/japan-business-mcp/actions/workflows/codeql.yml/badge.svg)](https://github.com/Ryou-Koshimizu/japan-business-mcp/actions/workflows/codeql.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

祝日と営業日、支払期日、インボイス（登録番号・記載事項・消費税）、源泉徴収、領収書の印紙、和暦、表記ゆれ。
日本の実務でよく使う計算と確認を、Claude Desktop・Claude Code・Cursor から呼べる **12 の道具**にまとめた MCP サーバーです。

- Go の単一の実行ファイル。外部の API・鍵・通信は要りません（法令の規則からその場で計算）。
- 標準入出力（stdio）と Streamable HTTP の両方に対応。公式 Go SDK（`modelcontextprotocol/go-sdk` v1.8.0）。
- 道具・資料（resources）・依頼文（prompts）をそろえています。
- 同じサーバーを WebAssembly にした**遊び場**があり、ブラウザだけで試せます。

![遊び場：支払期日とインボイスの点検を、ブラウザの中の MCP サーバーで実行](docs/images/playground.gif)

## 道具

| 道具 | できること |
| --- | --- |
| `holidays_list` | 祝日・休日の一覧（振替休日・国民の休日・特別法の休日を含む）。1949〜2150年 |
| `business_day_check` | 営業日か、休みならその理由と前後の営業日 |
| `business_day_add` | n 営業日後・前（飛ばした休みの一覧つき）。「3営業日以内に返信」「5営業日前に発注」 |
| `business_day_count` | 期間の営業日数と、祝日・会社の休み・土日の内訳 |
| `payment_due_date` | 「月末締め翌月末払い」「20日締め翌月10日払い」を日付に。休みなら前営業日・翌営業日へ |
| `registration_number_validate` | 登録番号（T＋13桁）・法人番号の検査用数字。1回で1,000件 |
| `qualified_invoice_check` | 適格請求書・適格簡易請求書の記載事項（税率ごとに1回の端数処理かも確認） |
| `consumption_tax_calculate` | 税率（10%・8%）ごとに合計して1回だけ端数処理した消費税。品目ごとの端数処理との差も |
| `withholding_tax_calculate` | 報酬・料金の源泉徴収税額（10.21%／20.42%、司法書士等）と振込額。手取りからの逆算も |
| `revenue_stamp_receipt` | 領収書（第17号文書）の収入印紙の額。税額を区分すれば税抜で判定、電子の領収書は不要 |
| `wareki_convert` | 和暦⇔西暦（明治〜令和、元年・漢数字・`R8.10.20` などの略記） |
| `text_normalize` | 全角半角・半角カナ・長音とハイフン・ひらがな⇔カタカナ。住所の丁目番地号のハイフン化と郵便番号 |

会社の休み（年末年始・夏季休暇・創立記念日・土曜出勤日など）は、道具の `calendar` 引数か、起動時の `-calendar` ファイルで足せます。

**資料（resources）**：`jbiz://holidays/{year}`（その年の祝日 JSON）、`jbiz://reference/invoice-requirements`（適格請求書の記載事項）、`jbiz://reference/eras`（元号の一覧）、`jbiz://reference/withholding`（源泉徴収の早見）

**依頼文（prompts）**：`invoice_review`（請求書の点検）、`payment_schedule`（支払予定表）、`freelance_invoice`（フリーランスへの支払額）

## 導入

### 1. 実行ファイルを入れる

```sh
go install github.com/Ryou-Koshimizu/japan-business-mcp/cmd/japan-business-mcp@latest
```

[Releases](https://github.com/Ryou-Koshimizu/japan-business-mcp/releases) から macOS・Linux・Windows（amd64・arm64）向けの実行ファイルも入手できます（来歴の証明 attestation つき）。

### 2. アプリに登録する

**Claude Desktop**（`claude_desktop_config.json`）

```json
{
  "mcpServers": {
    "japan-business": {
      "command": "japan-business-mcp"
    }
  }
}
```

**Claude Code**

```sh
claude mcp add japan-business -- japan-business-mcp
```

**Cursor**（`.cursor/mcp.json` または `~/.cursor/mcp.json`）

```json
{
  "mcpServers": {
    "japan-business": {
      "command": "japan-business-mcp",
      "args": ["-calendar", "/path/to/company-calendar.json"]
    }
  }
}
```

`command` に絶対パスを書くと、PATH の設定に左右されません（例：`/Users/you/go/bin/japan-business-mcp`）。

**Streamable HTTP**

```sh
JBIZ_HTTP_TOKEN=change-me japan-business-mcp -http 127.0.0.1:8105
# → http://127.0.0.1:8105/mcp（GET /healthz で生存確認）
```

`JBIZ_HTTP_TOKEN` を設定すると `Authorization: Bearer <token>` の要求だけを受け付けます。他のサイトからの呼び出し（クロスオリジン）と DNS リバインディングは断ります。

### 3. 会社のカレンダー（任意）

```json
{
  "company_holidays": ["12-29..01-03", "08-13..08-16", "2026-10-21"],
  "work_days": ["2026-10-24"]
}
```

`12-29` は毎年、`2026-10-21` はその日だけ、`..` で範囲。`work_days` は休みでも営業する日です。`-calendar` か環境変数 `JBIZ_CALENDAR` で読み込み、どの道具にも効きます。

### 話しかける例

- 「来月の支払予定を、月末締め翌月末払い・休みなら前営業日で出して」
- 「この請求書、インボイスとして足りないところはある？（文面を貼る）」
- 「この取引先リストの登録番号、写し間違いがないか全部見て」
- 「報酬30万円（税抜）のデザイン料、源泉徴収と振込額は？」
- 「令和8年度の営業日数を、年末年始は12/29〜1/3休みで数えて」

## 遊び場

`web/` は静的なページで、道具の計算をすべてブラウザの中（WebAssembly）で行います。画面に流れる JSON-RPC は、ブラウザの中の MCP サーバーが実際に返した電文です。

```sh
make web                                  # web/main.wasm.gz と wasm_exec.js を作る
python3 -m http.server 8105 -d web        # http://127.0.0.1:8105/
```

GitHub Pages（`.github/workflows/pages.yml` を手動で実行）か、`make web` 後の `web/` を Vercel などの静的配信に置けば公開できます。

## 確かめたこと

正解の表を手で書き写すのではなく、公的機関の公開データと突き合わせています。数字はすべて実際に測ったものです。

### 実機試験（`go run ./scripts/realworld`）

配る形の実行ファイルを、公式 SDK のクライアントから **stdio と Streamable HTTP の両方**で呼び、その場で取得した公開データと照合しました（2026-10-20 実施。結果は [docs/realworld/latest.json](docs/realworld/latest.json)）。

| 確かめたこと | 件数 | 誤り |
| --- | ---: | ---: |
| 祝日：内閣府「国民の祝日について」の CSV（1955〜2027年）と1日ずつ | 1,067日 | 0 |
| 営業日の判定：2025〜2027年の全日 | 1,095日 | 0 |
| 誤検出：国税庁の公開データの番号（法人番号は鳥取県の全件、登録番号は個人と人格のない社団等） | 331,285件 | 0 |
| 見逃し：上の番号の1桁を変えたもの（0↔9 の入れ替えを除く） | 331,285件 | 0 |
| 住所の正規化：鳥取県の全法人の所在地（冪等・全角が残らない・数字の並びが変わらない） | 20,258件 | 0 |
| 和暦⇔西暦の往復：1873〜2150年を7日おき | 14,506件 | 0 |

1回の実機試験で、道具の呼び出しは通り道ごとに 71,360 回（stdio 88秒・HTTP 28秒、Apple Silicon の Mac で測定）。

### 全件試験（`bash scripts/check-all.sh`）

| 種類 | 結果 |
| --- | --- |
| Go の試験（表駆動・ゴールデン・ファズの種・MCP 結合（実行ファイルを stdio と HTTP で）・`-race`） | 273件 合格・飛ばし 0 |
| ファズ（検査用数字・正規化の冪等性・和暦・支払期日・月の計算・会社カレンダー） | 7種類 × 30秒、失敗 0 |
| 実ブラウザ（Playwright：Chromium と WebKit） | 72件 合格・飛ばし 0 |
| └ 画面の大きさ 25種類（280×653〜3840×2160、横向きのスマホを含む）× 2 | 横はみ出し 0・ヘッダー 80px・ヒーローの中身は最初の画面に収まる |
| └ 全24例を画面から実行し、返事が Go の試験と同じ正解（ゴールデン）と一致 | 誤答 0 |
| └ axe（WCAG 2.1 A/AA、琥珀・緑 × パソコン・スマホ） | 違反 0 |
| 静的解析 | `go vet`・staticcheck 2026.2.2・golangci-lint 2.15.0 で 0件 |

祝日の規則は、内閣府の CSV（取得したままの Shift_JIS、SHA-256 を記録）と1日ずつ突き合わせる試験を、ふだんの `go test` にも入れています（[internal/holiday/testdata/SOURCE.md](internal/holiday/testdata/SOURCE.md)）。

## 設計判断

- **表ではなく規則で計算する**：祝日は「国民の祝日に関する法律」の改正経過・振替休日・国民の休日・特措法を規則として持ち、2028年以降の春分・秋分は「計算式による見込み」（`is_estimate`）と明示します。
- **計算の部品と MCP の層を分ける**：`internal/holiday` などは MCP を知らず、`internal/server` が説明文・スキーマ・JSON の形だけを受け持ちます。stdio・HTTP・WebAssembly のどれも同じ `server.New()` を使います。
- **出力はすべて構造化**：入出力の JSON Schema を Go の型から作り、`structuredContent` と同じ JSON の text を返します。日付は `format: date` の文字列。
- **金額は整数演算**：消費税・源泉徴収は円の整数と `math/big` で計算し、浮動小数の誤差を入れません。
- **重い入力に強く**：会社の休みは展開後 2,000日まで、走査の上限、道具の中の panic はエラー応答に変えてサーバーを落としません。
- **正解を3か所で共有**：24の呼び出し例と正解（ゴールデン）を、サーバーの試験・実行ファイルの試験・遊び場の画面の試験で同じものを使います。

詳しくは [docs/design.md](docs/design.md)。

## 開発

```sh
make build        # bin/japan-business-mcp
make test         # go test ./...
make race lint    # -race と静的解析
make fuzz         # ファズ（FUZZTIME=30s）
make e2e          # 遊び場の実ブラウザ試験
make realworld    # 実機試験（公的データを取得して照合）
bash scripts/check-all.sh   # 全件試験
```

Go 1.27 以上。遊び場の試験は Node.js 22 と Python 3（静的配信）を使います。

## 出典

- 内閣府「国民の祝日について」（祝日の CSV）
- 国税庁 法人番号公表サイト・適格請求書発行事業者公表サイト（全件データ）
- 法令：国民の祝日に関する法律、消費税法第57条の4、消費税法施行令第70条の10、所得税法第204条、印紙税法 別表第一

## ライセンス

MIT。アイコンは [Lucide](https://lucide.dev)（ISC）と [Simple Icons](https://simpleicons.org)（CC0）。
