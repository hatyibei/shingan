#!/usr/bin/env bash
# Shingan Live Demo Script — 面接用
# 使い方: bash scripts/demo.sh [--dry-run]
#
# --dry-run: Vertex AI への実際の呼び出しをスキップする（CI/オフライン確認用）
#
# 前提条件:
#   - go build -o shingan ./cmd/shingan
#   - go build -o shingan-runner ./cmd/runner
#   - gcloud auth application-default login 済み
#   - GOOGLE_CLOUD_PROJECT=axial-mercury-486503-j5 (デフォルト設定済み)

set -euo pipefail

DRY_RUN_FLAG=""
if [[ "${1:-}" == "--dry-run" ]]; then
  DRY_RUN_FLAG="--dry-run"
  echo "[demo.sh] --dry-run モード: Vertex AI 呼び出しをスキップします"
fi

SHINGAN="${SHINGAN:-./shingan}"
RUNNER="${RUNNER:-./shingan-runner}"

# バイナリが存在するか確認
if [[ ! -f "$SHINGAN" ]]; then
  echo "Error: $SHINGAN が見つかりません。先に 'go build -o shingan ./cmd/shingan' を実行してください。"
  exit 1
fi
if [[ ! -f "$RUNNER" ]]; then
  echo "Error: $RUNNER が見つかりません。先に 'go build -o shingan-runner ./cmd/runner' を実行してください。"
  exit 1
fi

echo ""
echo "============================================================"
echo "  Shingan Live Demo — AI Agent Workflow Static Analyzer"
echo "  面接デモシナリオ: 静的解析 → safe-guard → Vertex AI実行"
echo "============================================================"
echo ""

# ─── Step 1: 静的解析で警告 ───────────────────────────────────────
echo "## Step 1: Shinganが静的解析でCritical警告を検出"
echo "   ファイル: examples/runtime/infinite_loop_unbounded.go"
echo "   (MaxIterations未設定のLoopAgentを含む — 意図的なバグ)"
echo ""
"$SHINGAN" analyze \
  --format adk-go \
  --input examples/runtime/infinite_loop_unbounded.go \
  --output markdown || true
echo ""
echo "→ 終了コード 2 (Critical) 期待"
echo ""

# ─── Step 2: safe-guard で実行拒否 ────────────────────────────────
echo "------------------------------------------------------------"
echo "## Step 2: Shingan safe-guard — Critical検出で実行拒否"
echo "   shingan-runner は内部でShingan解析を実行し、"
echo "   Critical Findingがあれば起動前に拒否します。"
echo ""
"$RUNNER" --sample infinite_loop_unbounded $DRY_RUN_FLAG || true
echo ""
echo "→ 実行拒否メッセージ期待 (エラーではなく安全な拒否)"
echo ""

# ─── Step 3: 安全版（MaxIter=3）は実行成功 ─────────────────────────
echo "------------------------------------------------------------"
echo "## Step 3: 安全版 (MaxIterations=3) — Shinganがクリーン判定"
echo "   ファイル: examples/runtime/infinite_loop_bounded.go"
echo "   (MaxIterations=3 が設定済み — Shingan finding なし)"
echo ""
"$SHINGAN" analyze \
  --format adk-go \
  --input examples/runtime/infinite_loop_bounded.go \
  --output markdown
echo ""
echo "→ 終了コード 0 (finding なし) 期待"
echo ""
echo "   shingan-runner で実行..."
"$RUNNER" --sample infinite_loop_bounded $DRY_RUN_FLAG
echo ""

# ─── Step 4: シンプルなLLM Agent 実行デモ ──────────────────────────
echo "------------------------------------------------------------"
echo "## Step 4: シンプルなLLM Agent — Vertex AI Gemini で挨拶"
echo "   ファイル: examples/runtime/simple_agent.go"
echo "   モデル: gemini-2.0-flash-001 (Vertex AI, us-central1)"
echo ""
"$RUNNER" --sample simple $DRY_RUN_FLAG
echo ""

echo "============================================================"
echo "  デモ完了"
if [[ -n "$DRY_RUN_FLAG" ]]; then
  echo "  (--dry-run モード: 実際のVertex AI呼び出しは行われていません)"
  echo "  ライブ実行するには: bash scripts/demo.sh"
fi
echo "============================================================"
