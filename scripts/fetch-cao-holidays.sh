#!/usr/bin/env bash
# 内閣府の祝日 CSV を取り直す。取得後に internal/holiday/testdata/SOURCE.md の日付と SHA-256 を更新し、
# go test ./internal/holiday/ で規則の計算と突き合わせる。
set -euo pipefail
cd "$(dirname "$0")/.."
curl -fsS -m 30 -o internal/holiday/testdata/syukujitsu.csv \
  https://www8.cao.go.jp/chosei/shukujitsu/syukujitsu.csv
shasum -a 256 internal/holiday/testdata/syukujitsu.csv
