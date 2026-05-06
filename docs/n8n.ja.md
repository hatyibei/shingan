> 🌐 Language: [English](./n8n.md) | **日本語**

# n8n サポート

> Phase 1 / JSON-DSL トラック — [n8n](https://n8n.io) ワークフローエクスポート用の Shingan パーサ。

## 概要

Shingan は n8n の `.json` ワークフローエクスポート（n8n エディタの **Workflow → Download** ボタン、もしくは CLI の `n8n export:workflow --id=<n>` で取得できるファイル）を静的解析できる。LangGraph (Python) や ADK-Go と違い、n8n は JSON DSL でワークフローを定義するため、グラフ抽出にランタイムは不要。

パーサは **純 Go**: Python シムも Node ブリッジも不要。CI 統合がもっとも軽い。

```
┌──────────────────────┐    json.Unmarshal    ┌──────────────────────────┐
│ shingan (Go process) │ ──────────────────► │  workflow.json           │
│   N8nParser          │                      │  (n8n export)            │
└──────────────────────┘                      └──────────────────────────┘
```

## インストール

追加依存なし — `shingan` をインストールすればそのまま動く:

```bash
shingan analyze --format n8n --input workflow.json
```

## 使い方

単一ファイル:

```bash
shingan analyze --format n8n --input workflow.json --output markdown
```

ディレクトリ解析は v0.7 では未対応 (n8n エクスポートは 1 ファイル 1 ワークフロー)。個別に渡すかシェルループで:

```bash
for f in n8n-exports/*.json; do
  shingan analyze --format n8n --input "$f" --output sarif --output-file "${f%.json}.sarif"
done
```

CI での baseline 付き呼び出し:

```bash
shingan analyze \
  --format n8n \
  --input n8n-exports/customer_support.json \
  --baseline .shingan/baseline.json \
  --since main
```

## NodeType マッピング

n8n の type 文字列 (`n8n-nodes-base.openAi`, `n8n-nodes-base.if` など) を Shingan 正準 NodeType enum にマッピングし、フレームワーク非依存ルールがそのまま動くようにする。

| n8n type 部分一致 | Shingan NodeType | `Config["category"]` |
|---|---|---|
| `openai`, `chatgpt`, `anthropic`, `claude`, `gemini`, `vertex`, `bedrock`, `ollama`, `mistral`, `cohere`, `huggingface` | LLM | — |
| `n8n-nodes-langchain` | LLM | — |
| `ai-agent`, `ai_agent`, `.agent`, `agent.`, `/agent` | LLM | — |
| `llm` (catch-all) | LLM | — |
| `.code`, `executecommand`, `function`, `pythonfunction` | Tool | `code_execution` |
| `.if`, `.switch`, `filter`, `router` | Condition | — |
| `webhook`, `trigger` | Tool | `trigger` |
| `httprequest`, `http`, `.api`, `rest` | Tool | `api` |
| (それ以外) | Tool | `api` (default) |

マッチャーは **大文字小文字無視 + 部分一致** なので、新しい n8n type 名 (`OpenAi2`, `chatGptAdvanced`) もコード変更なしで動く。

## エッジマッピング

n8n の `connections.<source>.main` は **2 次元配列**:

- 外側 index = 出力ポート (0 = pass / true、`if` ノードでは 1 = fail / false)
- 内側 index = 同じポートからの並列宛先

Shingan のエッジ条件:

| 元ノードの NodeType | ポート 0 → Edge.Condition | ポート 1 → Edge.Condition | ポート n>1 → Edge.Condition |
|---|---|---|---|
| Condition (`if`, `switch`) | `"true"` | `"false"` | `"branch_<n>"` |
| それ以外 | `""` (無条件) | `"branch_1"` | `"branch_<n>"` |

これで `cycle_detection` / `unreachable_node` / `error_handler_checker` は ADK-Go の `LoopAgent` や LangGraph の `add_conditional_edges` と同じやり方でワークフローを推論できる。

## エントリノードの決め方

n8n エクスポートは entry を宣言しない。Shingan は次の優先順で選ぶ:

1. `Config["category"] == "trigger"` (`webhook` / `*Trigger` / `manualTrigger`) の最初のノード
2. 入力エッジを持たない最初のノード
3. 配列順で最初のノード

## 無効化されたノード

n8n の `"disabled": true` フラグが立ったノードは、付随するエッジごと黙って捨てる。n8n ランタイムの挙動 (実行されない) と一致させており、デッドコードに対する findings を避ける。

## 対応している機能

| 機能 | 対応 | 備考 |
|---|---|---|
| `nodes[]` 配列 | OK | Parameters は `Config` にそのまま入れる |
| `connections.<source>.main` | OK | 多ポート + 並列宛先どちらも処理 |
| `connections.<source>.ai_*` (langchain sub-tools / memory / output parsers) | Skip | 装飾扱い、エッジ生成しない |
| `disabled: true` | OK | ノードと付随エッジを除外 |
| Trigger 系: `webhook`, `*Trigger`, `manualTrigger` | OK | エントリ昇格 |
| `if` / `switch` Condition ノード | OK | branch ラベル `true` / `false` / `branch_<n>` |
| サブワークフロー (`executeWorkflow`) | v0.7 範囲外 | 通常 Tool 扱い、呼び先はインライン化しない |
| Pinned data (`pinData`) | 無視 | 静的解析は構造だけ見る、テストデータは見ない |

## Confidence と ConfidenceReason

n8n は完全静的 — LangGraph の `conditional_edges` コールバックのような動的グラフ生成がない — ので、パーサは高 confidence findings を出す:

| エッジ / ノード種別 | Confidence | ConfidenceReason |
|---|---|---|
| `connections.<source>.main` エッジ | 1.0 | `exact_static_match` |
| 部分一致から推定した NodeType | 0.8 | `name_heuristic` |
| デフォルト Tool fallback (未知 type) | 0.6 | `name_heuristic` |

## サンプル

5 つの参照サンプルが `testdata/n8n/` にある:

| ファイル | パターン | 実測 findings |
|---|---|---|
| `simple_chain.json` | Webhook → ChatGPT → HTTP Request | Warning 2 件 — `error_handler_checker` (Webhook trigger / 下流に Tool を持つ ChatGPT LLM) |
| `branching.json` | Webhook → ChatGPT → IF → (Slack / Email) | Warning 1 件 — Webhook の `error_handler_checker` (IF の分岐先はクリーン終端) |
| `loop.json` | Schedule → Fetch → Split In Batches → Process Item ↺ | Critical 2 件 (`cycle_detection`: Split In Batches、`retry_storm`: Process Item の `retries=5 × parallelism=20`) + Warning 4 件 (各直列ノードの `error_handler_checker`) |
| `multi_step.json` | Webhook → Vector Search → Embed → Generator → Output | Warning 4 件 — trigger・API ツール・LLM 2 段すべてに `error_handler_checker` |
| `ai_agent.json` | langchain `aiAgent` から code-execution Tool に到達 | Critical 1 件 (`eval_missing`: Agent → Code Tool) + Warning 2 件 (Webhook と Agent の `error_handler_checker`)。langchain sub-tool 配線 (`ai_languageModel` / `ai_memory` / `ai_tool`) は装飾扱いで無視 |

実行例:

```bash
shingan analyze --format n8n --input testdata/n8n/simple_chain.json --output markdown
```

## 出力例 (`simple_chain.json`)

```bash
$ shingan analyze --format n8n --input testdata/n8n/simple_chain.json --output markdown
# Shingan Analysis Report

## Summary

| Total | Critical | Warning | Info |
|-------|----------|---------|------|
| 2     | 0        | 2       | 0    |

## Warning

| Rule                  | Node    | Confidence | Message                                                                                            |
|-----------------------|---------|------------|----------------------------------------------------------------------------------------------------|
| error_handler_checker | Webhook | 80%        | Tool node "Webhook" (category="trigger") has no conditional outgoing edges: error handling is missing |
| error_handler_checker | ChatGPT | 80%        | LLM node "ChatGPT" uses tool(s) but has no conditional outgoing edges: error handling for tool failures is missing |
```

(exit code: `2` — Warnings のみ、Critical なし)

## 設計参照

- ADR-002: Onion + Factory parser 拡張性
- ADR-003: WorkflowGraph IR (正準 NodeType enum)
- ADR-008: 二次元 ConfidenceReason

実装ファイル:

- `infrastructure/parser/n8n.go` — `WorkflowParser` 実装 (純 Go)
- `infrastructure/parser/n8n_test.go` — テーブル駆動テスト + エッジケース
- `infrastructure/factory/parser.go` — Factory 登録 `case "n8n"`
- `cmd/shingan/analyze.go` — `--format=n8n` フラグ
- `domain/testutil/generate.go` — プロパティテスト用 `GenerateN8nGraph`
- `cmd/shingan-gen/main.go` — サンプル生成用 `--pattern=n8n-simple`

## トラブルシューティング

| 症状 | 原因 | 対処 |
|---|---|---|
| `n8n parser: unmarshal: …` | 有効な n8n エクスポート JSON ではない | n8n エディタから再エクスポート (Workflow → Download) |
| グラフが空 | 全ノードが `disabled: true` | 少なくとも 1 ノードを有効化、または `--input` フィルタを外す |
| `if` ノードのエッジが消えている | `.main` でなく `.ai_*` を読んでいる | JSON に `main` エッジがあるか確認 (langchain 専用ノードは装飾扱い) |

## バージョン互換性

- n8n **v1.x** (現行 LTS) のエクスポート: テスト済み
- n8n **v0.x** (旧版) のエクスポート: スキーマは類似。NodeType の部分一致は動くが、古い `connections.<src>` 形は fixture でカバーしていない
- サブワークフローのインライン化 (`executeWorkflow`): Phase 2 の follow-up
