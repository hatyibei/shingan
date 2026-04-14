# Shingan（心眼）

> AI Agent Workflow Static Analyzer

![Go version](https://img.shields.io/badge/Go-1.23-00ADD8?logo=go) ![License](https://img.shields.io/badge/License-MIT-green) ![CI](https://github.com/hatyibei/shingan/actions/workflows/ci.yml/badge.svg)

AIエージェントのワークフローを実行前に構造解析し、無限ループ・到達不能ノード・エラーハンドリング欠落・コスト非効率・冗長LLM呼出を検出する Go製静的解析ツール。

## なぜShinganか

LLMオーケストレーションが普及した現在、ワークフローの「設計時バグ」を検出するカテゴリが空白になっている。FlowLintはn8n専用、LangSmithはランタイム観測に特化しており、いずれも**実行前**の構造検査には対応していない。

エンタープライズ向けエージェント（ブラウザ自動操作・外部API連携）は一度実行すると副作用が不可逆になる。サイクル・到達不能・エラーハンドリング欠落を**デプロイ前**に機械的に検出できれば、コスト損失とインシデントの大半を未然に防げる。

## アーキテクチャ

Onion Architecture — 内側から外側への依存のみ許容。

```
┌─────────────────────────────────────────────┐
│  cmd/          DI配線・エントリポイントのみ          │
│  ┌───────────────────────────────────────┐  │
│  │  infrastructure/   具象実装            │  │
│  │  ┌─────────────────────────────────┐  │  │
│  │  │  application/   ユースケース     │  │  │
│  │  │  ┌───────────────────────────┐  │  │  │
│  │  │  │  domain/  外部依存ゼロ     │  │  │  │
│  │  │  └───────────────────────────┘  │  │  │
│  │  └─────────────────────────────────┘  │  │
│  └───────────────────────────────────────┘  │
└─────────────────────────────────────────────┘
```

| 層 | 責務 | 依存先 |
|---|---|---|
| domain/ | WorkflowGraph・ルール・エンティティ定義 | 標準ライブラリのみ |
| application/ | AnalysisOrchestrator・interface定義 | domain/ |
| infrastructure/ | パーサー・レポーター・Factory実装 | application/, domain/ |
| cmd/ | CLI・DI配線 | infrastructure/ |

**Factory Pattern 3箇所**

- `AnalyzerFactory` — 解析ルール (`domain.AnalysisRule`) の生成・登録
- `ParserFactory` — フォーマット別パーサー (`application.WorkflowParser`) の切替
- `ReporterFactory` — 出力形式別レポーター (`application.ReportFormatter`) の切替

## インストール

```bash
go install github.com/hatyibei/shingan/cmd/shingan@latest
```

またはソースからビルド:

```bash
git clone https://github.com/hatyibei/shingan.git
cd shingan
go build -o shingan ./cmd/shingan
```

## 使い方

JSON入力の例:

```bash
shingan analyze --format json --input workflow.json --output markdown
```

ADK-Go入力の例:

```bash
shingan analyze --format adk-go --input ./agents/ --output markdown
```

終了コード: `0` = Info以下のみ、`1` = Warning検出、`2` = Critical検出

CI統合例（GitHub Actions）:

```yaml
- name: Shingan check
  run: shingan analyze --format adk-go --input ./agents/
```

## ランタイムデモ — Vertex AI Gemini で実行

`shingan-runner` CLIは、静的解析のsafe-guard付きでADK-Go AgentをVertex AI上で実際に実行する。
「静的解析で警告したバグが本当に問題を起こすか」を1分で実証できる。

### 事前準備

```bash
# GCPプロジェクト設定（Vertex AI API有効化済み）
export GOOGLE_CLOUD_PROJECT=<your-project-id>
export GOOGLE_CLOUD_LOCATION=us-central1
export GOOGLE_GENAI_USE_VERTEXAI=true

# Application Default Credentials認証
gcloud auth application-default login

# バイナリビルド
go build -o shingan ./cmd/shingan
go build -o shingan-runner ./cmd/runner
```

### デモフロー（4ステップ）

```bash
bash scripts/demo.sh
```

1. **静的解析でCritical警告**: `infinite_loop_unbounded.go` (MaxIterations未設定) を解析
2. **Runner safe-guardで実行拒否**: Critical finding検出時は実行をブロック
3. **安全版は実行成功**: `infinite_loop_bounded.go` (MaxIterations=3) はクリーン判定で実行
4. **Vertex AI Gemini応答**: `simple_agent.go` で実際のLLM呼び出し

コスト: 1デモ実行あたり<0.1円 (gemini-2.0-flash-001, max_tokens=100)

詳細は [docs/runtime-demo.md](./docs/runtime-demo.md) 参照。

## 本物のADK-Goサンプルでのデモ

`examples/real/` に配置した `google.golang.org/adk v1.1.0` SDK準拠のサンプル3種に対してShinganが検出するFinding:

| Sample | Rule | Severity | 結果 |
|---|---|---|---|
| examples/real/infinite_loop.go | cycle_detection | Critical | loopagent.New + MaxIterations未設定で無限ループを検出 |
| examples/real/unreachable.go | unreachable_node | Warning | orphan_analyzerが orchestratorのSubAgentsに未接続で到達不能を検出 |
| examples/real/missing_handler.go | error_handler_checker | Warning | ※下記注記参照 |

実行:

```bash
shingan analyze --format adk-go --input examples/real/infinite_loop.go --output markdown
# exit code 2 (Critical)

shingan analyze --format adk-go --input examples/real/unreachable.go --output markdown
# exit code 1 (Warning)
```

**公式ADK-Go SDKとの差分・注記:**

- `loopagent.New(loopagent.Config{AgentConfig: agent.Config{SubAgents: ...}})` パターンに対応済み（v1.1.0）
- LlmAgent / SequentialAgent / LoopAgent の `New()` コンストラクタパターンを AST で検出
- `functiontool.New(Config, handler)` で登録したツールのノード検出は未対応（ツールが `tool.Tool` interface 変数として渡される場合、call-site AST解析が必要）→ `missing_handler.go` の error_handler_checker は今後対応予定
- ADK-Go SDK は v1.1.0 で `go 1.25.0` 以上が必要（go.mod のminimum versionに反映済み）

```bash
# demo タグでE2E自動検証
go test -tags=demo -v -run TestDemo_ .
```

## 解析ルール一覧

| Rule ID | 検出対象 | 最高 Severity |
|---|---|---|
| cycle_detection | 非Controlノードのサイクル、LoopAgent管理下のサイクル | Critical |
| loop_guard | LoopAgent (Control型) のMaxIterations未設定 | Critical |
| unreachable_node | エントリから到達不能なLLM/Toolノード | Warning |
| error_handler_checker | 外部I/Oノード後のエラーハンドリング欠落 | Critical |
| cost_estimation | ループ内の高額LLMモデル、単純タスクへの高額モデル適用 | Warning |
| redundant_llm_call | 同一prompt_template×modelの重複呼出 | Warning |

## サポートフォーマット

### 入力

| Format | 状態 | 備考 |
|---|---|---|
| json | GA | Shingan独自のWorkflowGraph JSON |
| adk-go | GA | Google ADK-Go (`google.golang.org/adk`) のAST解析 |
| samurai | Alpha | SamuraiAI想定スキーマ（社内実スキーマ差し替え前提） |
| n8n | Planned (v0.2) | n8n JSON export |
| langgraph | Planned (v1.0) | Python AST経由 |

### 出力

| Format | ContentType | 用途 |
|---|---|---|
| json | application/json | API応答、プログラム連携 |
| markdown | text/markdown | CLI、レポート |
| sarif | application/sarif+json | GitHub Code Scanning統合 |

## ロードマップ

- **v0.1（2026-04）**: ADK-Go + JSON + SamuraiAI想定スキーマ対応、6ルール、CLI + goa API、SARIF出力、Vertex AIランタイムデモ ✓
- **v0.2**: n8nパーサー、SamuraiAI公式スキーマ対応、CI Plugin
- **v0.3**: PIIリークルール、信頼度スコア、設計-実行時観測統合
- **v1.0**: LangGraph/Dify対応、マルチフレームワーク安定版

## 開発

```bash
go test ./...
go vet ./...
go build -o shingan ./cmd/shingan
```

## ドキュメント

- [アーキテクチャ詳細](./docs/architecture.md)
- [ランタイムデモ手順](./docs/runtime-demo.md)
- [SARIF出力とGitHub Code Scanning統合](./docs/sarif-output.md)
- [SamuraiAIアダプター設計](./docs/samurai-adapter.md)
- [cycle_detectionの技術ノート](./docs/cycle-detection-note.md)
- [全ADR](./shingan-adr.md)

## ライセンス

MIT
