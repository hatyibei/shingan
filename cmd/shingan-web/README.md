# shingan-web

ADK Web UI + Shingan pre-execution guard — middleware-injection demo launcher.

GUI ワークフローエディタに Shingan の静的解析を **実行前ガード** として組み込む参考実装。Google ADK Web UI (`localhost:8080/ui/`) を起動し、Run API (`/api/run`, `/api/run_sse`) に middleware を注入する。同じ pattern は SamuraiAI / Dify / 自社 GUI 等にも応用可。

## 概要

- **Critical な問題を持つ Agent** → 実行ブロック (HTTP 403) + JSON エラー
- **クリーンな Agent** → ADK Web UI から通常通り Vertex AI Gemini 実行

## 起動

```bash
cd /path/to/shingan
go build -o shingan-web ./cmd/shingan-web

GOOGLE_CLOUD_PROJECT=axial-mercury-486503-j5 \
GOOGLE_CLOUD_LOCATION=us-central1 \
GOOGLE_GENAI_USE_VERTEXAI=true \
./shingan-web
```

または `scripts/web-demo.sh` を使う:

```bash
bash scripts/web-demo.sh
```

ブラウザで `http://localhost:8080` を開く (自動で `/ui/` にリダイレクト)。

## デモ手順

1. `infinite_loop_unbounded` を選択 → チャットでメッセージ送信
   → **Shingan guard が 403 でブロック** (MaxIterations 未設定の Critical)
2. `infinite_loop_bounded` を選択 → 同じメッセージ送信
   → Vertex AI Gemini が応答 (MaxIterations=3, 安全)
3. `simple_hello` を選択 → メッセージ送信
   → 「こんにちは」等の日本語挨拶が返る

## 登録 Agent

| Agent 名 | Shingan 結果 | ソースファイル |
|---|---|---|
| `infinite_loop_unbounded` | Critical (loop_guard) → **BLOCKED** | `examples/runtime/infinite_loop_unbounded.go` |
| `infinite_loop_bounded` | Clean → PASSED | `examples/runtime/infinite_loop_bounded.go` |
| `simple_hello` | Clean → PASSED | `examples/runtime/simple_agent.go` |

## アーキテクチャ

```
Browser → ADK Web UI (/ui/)
       → /api/run POST  →  shinganGuardMiddleware  →  ADK REST API
                               ↓ (Critical?)
                            403 JSON (shingan_guard)
```

Middleware の注入点: `web.BuildBaseRouter()` + `router.Use(shinganGuardMiddleware(...))` を
`api.SetupSubrouters()` より **前に** 呼ぶ。これで ADK route 登録前にミドルウェアスタックに入る。

## テスト

```bash
go test ./cmd/shingan-web/...
```
