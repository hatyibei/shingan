# Shingan Runtime Demo — Vertex AI Gemini 実行手順

> **面接用ランタイムデモ**: Shinganが静的解析で検出したバグを、実際にVertex AI (Gemini) で走らせて「本当に問題になる」ことを実証する。

## デモシナリオ概要

| ステップ | 内容 | 期待結果 |
|---|---|---|
| Step 1 | `infinite_loop_unbounded.go` を Shingan で解析 | Critical (exit code 2) |
| Step 2 | `shingan-runner --sample infinite_loop_unbounded` | safe-guard で実行拒否 |
| Step 3 | `infinite_loop_bounded.go` を解析 + 実行 | 解析クリーン → 3イテレーションで正常終了 |
| Step 4 | `simple_agent.go` を解析 + 実行 | Gemini が日本語で挨拶 |

---

## 事前準備

### 1. GCP 認証 (ADC)

```bash
gcloud auth application-default login
```

ブラウザが開いたらGoogleアカウントでログインし、権限を承認します。  
完了すると `~/.config/gcloud/application_default_credentials.json` が生成されます。

**確認:**
```bash
ls ~/.config/gcloud/application_default_credentials.json  # 存在すること
```

### 2. GCPプロジェクト設定

```bash
gcloud config set project axial-mercury-486503-j5
```

### 3. Vertex AI API 有効化

```bash
gcloud services enable aiplatform.googleapis.com --project=axial-mercury-486503-j5
```

**確認済み**: 2026-04-14時点でプロジェクト `axial-mercury-486503-j5` では `aiplatform.googleapis.com` が `ENABLED` 状態。

### 4. 環境変数（オプション）

`cmd/runner/samples.go` では Vertex AI 設定をコード内に直接記述しているため、環境変数は不要です。  
ただし標準的なADK-Go利用では以下を設定することが多いです:

```bash
export GOOGLE_CLOUD_PROJECT=axial-mercury-486503-j5
export GOOGLE_CLOUD_LOCATION=us-central1
export GOOGLE_GENAI_USE_VERTEXAI=true
```

### 5. バイナリビルド

```bash
cd /home/hatyibei/Claude/shingan

# Shingan CLI
go build -o shingan ./cmd/shingan

# Runner
go build -o shingan-runner ./cmd/runner
```

---

## 実行手順

### デモスクリプト（全フロー）

```bash
bash scripts/demo.sh
```

オフライン確認 (Vertex AI呼び出しなし):
```bash
bash scripts/demo.sh --dry-run
```

### 個別コマンド

#### Step 1: 静的解析で Critical 警告

```bash
./shingan analyze \
  --format adk-go \
  --input examples/runtime/infinite_loop_unbounded.go \
  --output markdown
```

期待出力:
```
## Critical
| cycle_detection | ... | Control node "unbounded_loop" has a cycle but max_iterations is not set: risk of infinite loop |
```
終了コード: `2`

#### Step 2: safe-guard — 実行拒否

```bash
./shingan-runner --sample infinite_loop_unbounded --dry-run
```

期待出力:
```
[1/3] Running Shingan static analysis ...
    ✗ [Critical] cycle_detection — ...
[2/3] Safe-guard: Critical finding detected → EXECUTION REFUSED
    ✗ Execution blocked by Shingan safe-guard
```

#### Step 3: 安全版 LoopAgent 実行

```bash
./shingan-runner --sample infinite_loop_bounded
```

期待出力:
```
[1/3] Running Shingan static analysis ...
    ✓ No findings — clean agent
[2/3] Safe-guard: no Critical findings → execution allowed
[3/3] Executing agent via Vertex AI Gemini (gemini-2.0-flash-001)...
[event 1 / author=bounded_loop] 1
[event 2 / author=bounded_loop] 2
[event 3 / author=bounded_loop] 3 DONE
✓ Agent execution complete.
```

#### Step 4: シンプルなLLM Agent 実行

```bash
./shingan-runner --sample simple
```

期待出力:
```
[event 1 / author=hello_agent] こんにちは！
✓ Agent execution complete.
```

---

## コスト試算

| 項目 | 内容 |
|---|---|
| モデル | `gemini-2.0-flash-001` (Vertex AI, us-central1) |
| 入力トークン | ~50 tokens/リクエスト |
| 出力トークン | ~30 tokens/リクエスト (max 200) |
| デモ全体のAPI呼び出し | 約5回 (Step 3: 3回 + Step 4: 1回 + 余裕) |
| 推定コスト | **$0.001 未満** (1円未満) |

参考: gemini-2.0-flash-001 の価格 (2025年時点)
- Input: $0.075/1M tokens  
- Output: $0.30/1M tokens

---

## トラブルシューティング

### `PERMISSION_DENIED` エラー

```
ADC が設定されていないか、プロジェクトへのアクセス権限がありません
```

対処:
```bash
gcloud auth application-default login
gcloud config set project axial-mercury-486503-j5
```

### `aiplatform.googleapis.com is not enabled`

```bash
gcloud services enable aiplatform.googleapis.com --project=axial-mercury-486503-j5
```

### `shingan: command not found`

プロジェクトルートからビルドしてカレントディレクトリで実行:
```bash
go build -o shingan ./cmd/shingan
go build -o shingan-runner ./cmd/runner
./shingan analyze ...
./shingan-runner ...
```

---

## 技術仕様

### ADK-Go Runner API

```go
// 1. セッションサービス作成
sessionSvc := session.InMemoryService()

// 2. Runner 作成
r, err := runner.New(runner.Config{
    AppName:           "shingan-demo",
    Agent:             rootAgent,
    SessionService:    sessionSvc,
    AutoCreateSession: true,
})

// 3. 実行 (iter.Seq2 パターン)
for event, err := range r.Run(ctx, "user-id", "session-id", userMsg, agent.RunConfig{}) {
    // event.Content.Parts[i].Text でLLM出力を取得
}
```

### Vertex AI 認証

`gemini.NewModel` に `genai.ClientConfig{Backend: genai.BackendVertexAI}` を指定し、  
`GOOGLE_APPLICATION_CREDENTIALS` または ADC (`~/.config/gcloud/application_default_credentials.json`) で認証します。

---

## ファイル構成

```
cmd/runner/
  main.go         CLI (cobra) — --sample / --max-iter / --dry-run
  runner.go       解析 → safe-guard → 実行 ロジック
  samples.go      Agent builder 関数 (Vertex AI 接続)
  main_test.go    dry-run + safe-guard 動作確認テスト

examples/runtime/
  simple_agent.go              LlmAgent（解析ターゲット）
  infinite_loop_bounded.go     LoopAgent MaxIter=3（解析ターゲット）
  infinite_loop_unbounded.go   LoopAgent MaxIter未設定（解析ターゲット）
  README.md

scripts/
  demo.sh         全フロー実行スクリプト（--dry-run オプション付き）
```
