# Shingan デモ用スクリーンショット

ADK Web UI + Shingan middleware の動作を示すスクリーンショット一式。
面接（2026-04-17 Kiva社）のデモ資料として使用する。

## 取得方法

```bash
# 前提: shingan-web が起動済み
GOOGLE_CLOUD_PROJECT=axial-mercury-486503-j5 \
GOOGLE_CLOUD_LOCATION=us-central1 \
GOOGLE_GENAI_USE_VERTEXAI=true \
/home/hatyibei/Claude/shingan/shingan-web > /tmp/shingan-web.log 2>&1 &
sleep 3

# スクリーンショット取得
cd /home/hatyibei/Claude/shingan/scripts/screenshots
npm install  # 初回のみ
node capture.mjs

# 確認
ls -la *.png
```

## スクリーンショット一覧

| ファイル名 | シナリオ | 内容 |
|-----------|----------|------|
| `01-home-select-agent.png` | 1. トップページ | ADK Web UI 初期状態。"Select an agent" ドロップダウン |
| `02-home-3-agents-list.png` | 1. エージェント一覧 | 3エージェント一覧表示（infinite_loop_unbounded / infinite_loop_bounded / simple_hello） |
| `03-unbounded-agent-selected.png` | 2. バグAgent選択 | `infinite_loop_unbounded` を選択した状態。チャット入力待ち |
| `04-shingan-blocks-buggy-agent.png` | 2. Shinganブロック | "hello" 送信後、Shinganが403で拒否。"No invocations found" = Vertex AIに到達せず |
| `05-shingan-error-detail.png` | 2. エラー詳細 | 403 JSON オーバーレイ表示。`loop_guard` / `severity: critical` / `MaxIterations未設定` |
| `06-simple-hello-selected.png` | 3. 安全Agent選択 | `simple_hello` を選択した状態 |
| `07-clean-agent-executes.png` | 3. Gemini応答 | simple_hello に "こんにちは！" を送信 → Gemini が "こんにちは！" と返答 |
| `08-gemini-response-detail.png` | 3. 応答詳細 | 同上、スクロール後の詳細表示 |
| `09-bounded-agent-selected.png` | 4. 有界Loop Agent | `infinite_loop_bounded` を選択した状態 |
| `10-bounded-agent-executes.png` | 4. 有界Loop実行 | "count to 3" を送信 → `1` `2` `3 DONE` と順番に返答（MaxIter=3でループ正常終了） |

## 面接でのスクショ使用順序（推奨）

### 3分デモ フロー

1. **`02-home-3-agents-list.png`** — 「ADK Web UIに3つのエージェントを登録してあります」
2. **`03-unbounded-agent-selected.png`** — 「まず危険なAgent、MaxIterations未設定のLoopAgent」
3. **`04-shingan-blocks-buggy-agent.png`** — 「送信すると...No invocations found。Geminiに届いていない」
4. **`05-shingan-error-detail.png`** — 「Shinganが403でブロックしました。loop_guard、severity: critical、MaxIterations未設定で無限ループ危険」
5. **`07-clean-agent-executes.png`** — 「simple_helloはクリーン判定でVertexAI Geminiが実際に応答」
6. **`10-bounded-agent-executes.png`** — 「infinite_loop_bounded はMaxIter=3、Shinganがパスして1,2,3 DONEと正常実行」

### ストーリーの言い方

- 「ESLintがコンパイルエラー前にリントエラーを出すように、ShinganはGemini実行前にワークフローを解析します」
- 「Shinganがいなければ、このAgentはGemini APIを無制限に呼び続けて数千円の請求になる可能性がある」
- 「Onion Architectureだから、SamuraiAIへの対応はinfrastructure層にアダプター1つ追加するだけ」

## 使ったツール

- **Playwright** (playwright npm v1.59.1 + chromium v147)
- **Node.js** v24.13.1
- 動作確認: `node capture.mjs` 1回 (約3分、Vertex AI応答含む)

## 既知の制約

- `responseReceived` フラグ: Web UIが `.model-message` / `.event-container` クラスを使っていないため、Playwright側の応答検知は動かないが、タイムアウト後のスクリーンショットで実際の応答が映る（画像確認済み）
- Vertex AI コールドスタート: 初回応答に45秒程度かかる場合がある
- shingan-web 再起動: デモ前に `gcloud auth application-default login` が必要（ADC 12h有効期限）
