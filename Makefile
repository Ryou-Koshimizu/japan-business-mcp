# よく使う作業。全件試験は `make check`。
GO ?= go
VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test race lint fuzz web e2e realworld check clean

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/japan-business-mcp ./cmd/japan-business-mcp

test:
	$(GO) test ./...

race:
	$(GO) test -race -count=1 ./...

lint:
	$(GO) vet ./...
	staticcheck ./...
	golangci-lint run ./...
	GOOS=js GOARCH=wasm $(GO) vet ./cmd/wasm

# ファズは各30秒（CI では短く回す）
FUZZTIME ?= 30s
fuzz:
	$(GO) test ./internal/corpnum -run '^$$' -fuzz '^FuzzValidate$$' -fuzztime $(FUZZTIME)
	$(GO) test ./internal/corpnum -run '^$$' -fuzz '^FuzzCheckDigit$$' -fuzztime $(FUZZTIME)
	$(GO) test ./internal/textnorm -run '^$$' -fuzz '^FuzzNormalizeIdempotent$$' -fuzztime $(FUZZTIME)
	$(GO) test ./internal/wareki -run '^$$' -fuzz '^FuzzConvert$$' -fuzztime $(FUZZTIME)
	$(GO) test ./internal/payterm -run '^$$' -fuzz '^FuzzDue$$' -fuzztime $(FUZZTIME)
	$(GO) test ./internal/jpdate -run '^$$' -fuzz '^FuzzAddMonthsClamp$$' -fuzztime $(FUZZTIME)
	$(GO) test ./internal/server -run '^$$' -fuzz '^FuzzCalendarOptions$$' -fuzztime $(FUZZTIME)

# 遊び場（web/）に WebAssembly を置く
web:
	GOOS=js GOARCH=wasm $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o web/main.wasm ./cmd/wasm
	gzip -9 -n -f web/main.wasm
	cp "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" web/wasm_exec.js

# 遊び場の実ブラウザ試験（Chromium と WebKit、25の画面の大きさ、axe）
e2e: web
	cd e2e && npm ci && npx playwright test

# 実機試験：配る形の実行ファイルを、stdio と HTTP の本物の MCP クライアントから叩き、公的な実データと突き合わせる
realworld: build
	$(GO) run ./scripts/realworld -bin bin/japan-business-mcp

# 全件試験（飛ばしなし）
check: lint race fuzz e2e

clean:
	rm -rf bin dist web/main.wasm web/main.wasm.gz web/wasm_exec.js
