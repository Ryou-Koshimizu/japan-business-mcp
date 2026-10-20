#!/usr/bin/env bash
# 全件試験：リポジトリにあるすべての試験を、飛ばしなしで流して件数を数える。
#   bash scripts/check-all.sh            # 実機試験（公的データの取得）も含める
#   SKIP_REALWORLD=1 bash scripts/check-all.sh
# HTTP の試験・遊び場の配信は JBIZ_PORT（既定 8105）だけを使う。
set -euo pipefail
cd "$(dirname "$0")/.."
PORT="${JBIZ_PORT:-8105}"
OUT=".cache/check"
mkdir -p "$OUT"
step() { printf '\n== %s\n' "$*"; }

step "gofmt"
test -z "$(gofmt -l .)"
step "go vet（通常・WebAssembly）"
go vet ./...
GOOS=js GOARCH=wasm go vet ./cmd/wasm
step "staticcheck"
go run honnef.co/go/tools/cmd/staticcheck@2026.2.2 ./...
step "golangci-lint"
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run ./...

step "go test -race（単体・ゴールデン・MCP 結合：stdio と HTTP）"
JBIZ_TEST_HTTP_ADDR="127.0.0.1:${PORT}" go test -race -count=1 -json ./... > "$OUT/go-test.json" || { grep -E '"Action":"fail"' "$OUT/go-test.json" | head -20; exit 1; }
python3 - "$OUT/go-test.json" <<'PY'
import json, sys
p = s = f = 0
for line in open(sys.argv[1]):
    e = json.loads(line)
    if not e.get("Test"):
        continue
    a = e.get("Action")
    if a == "pass": p += 1
    elif a == "skip": s += 1
    elif a == "fail": f += 1
print(f"Go の試験（サブテスト含む）: 合格 {p}・飛ばし {s}・失敗 {f}")
sys.exit(1 if s or f else 0)
PY

step "ファズ（各 ${FUZZTIME:=30s}）"
make fuzz FUZZTIME="$FUZZTIME"

step "遊び場の実ブラウザ試験（Chromium・WebKit）"
make web
(cd e2e && npm ci --silent && npx playwright install chromium webkit >/dev/null && JBIZ_WEB_PORT="$PORT" npx playwright test)
python3 - <<'PY'
import json
d = json.load(open("test-results/e2e.json"))
st = d["stats"]
print(f"Playwright: 合格 {st['expected']}・飛ばし {st['skipped']}・失敗 {st['unexpected']}・不安定 {st['flaky']}")
raise SystemExit(1 if st["skipped"] or st["unexpected"] or st["flaky"] else 0)
PY

if [ -z "${SKIP_REALWORLD:-}" ]; then
  step "実機試験（配る実行ファイル × stdio・HTTP × 公的データ）"
  make build
  go run ./scripts/realworld -bin bin/japan-business-mcp -port "$PORT"
fi
step "全件試験：すべて緑"
