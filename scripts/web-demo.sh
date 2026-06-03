#!/bin/bash
# ADK Web UI + Shingan middleware デモ起動スクリプト
#
# 使い方:
#   bash scripts/web-demo.sh
#
# 前提:
#   - gcloud auth application-default login 済み
#   - go build 済み (./shingan-web が存在すること)
#
# ブラウザで http://localhost:8080 を開くと ADK Web UI が表示される。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# GCP プロジェクトは環境変数で指定する（実在のプロジェクトIDはハードコードしない）。
# VERTEX_PROJECT が優先、無ければ GOOGLE_CLOUD_PROJECT を使う。
export GOOGLE_CLOUD_PROJECT="${VERTEX_PROJECT:-${GOOGLE_CLOUD_PROJECT:-}}"
if [ -z "$GOOGLE_CLOUD_PROJECT" ]; then
  echo "エラー: GCP プロジェクトが未設定です。" >&2
  echo "  VERTEX_PROJECT または GOOGLE_CLOUD_PROJECT を設定してください。例:" >&2
  echo "  GOOGLE_CLOUD_PROJECT=my-gcp-project bash scripts/web-demo.sh" >&2
  exit 1
fi
export GOOGLE_CLOUD_LOCATION="${GOOGLE_CLOUD_LOCATION:-us-central1}"
export GOOGLE_GENAI_USE_VERTEXAI="${GOOGLE_GENAI_USE_VERTEXAI:-true}"

BINARY="$PROJECT_ROOT/shingan-web"

echo "=== ADK Web UI + Shingan 統合デモ ==="
echo ""
echo "GCPプロジェクト : $GOOGLE_CLOUD_PROJECT"
echo "リージョン       : $GOOGLE_CLOUD_LOCATION"
echo ""

# バイナリが無ければビルドする
if [ ! -f "$BINARY" ]; then
  echo "shingan-web バイナリが見つかりません。ビルドします..."
  cd "$PROJECT_ROOT"
  go build -o shingan-web ./cmd/shingan-web
  echo "ビルド完了: $BINARY"
  echo ""
fi

echo "起動中... ブラウザで http://localhost:8080 を開いてください"
echo ""
echo "========== デモ操作手順 =========="
echo ""
echo "  1. ADK Web UI が開く (http://localhost:8080 → /ui/ にリダイレクト)"
echo ""
echo "  2. infinite_loop_unbounded を選択"
echo "     └ セッション作成 → チャット欄に任意のメッセージを送信"
echo "     └ Shingan Critical エラーが表示され実行拒否 (HTTP 403)"
echo "     └ エラー: { \"error\": \"shingan_guard\", \"findings\": [...] }"
echo ""
echo "  3. infinite_loop_bounded を選択"
echo "     └ セッション作成 → チャット欄に任意のメッセージを送信"
echo "     └ Shingan がパス → Vertex AI Gemini が応答 (カウンター 1, 2, 3, DONE)"
echo ""
echo "  4. simple_hello を選択"
echo "     └ セッション作成 → チャット欄に任意のメッセージを送信"
echo "     └ Shingan がパス → 日本語挨拶が返る (\"こんにちは\" 等)"
echo ""
echo "==================================="
echo ""
echo "Ctrl+C で終了"
echo ""

exec "$BINARY"
