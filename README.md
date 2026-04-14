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

## 解析ルール一覧

| Rule ID | 検出対象 | 最高 Severity |
|---|---|---|
| cycle_detection | max_iterations未設定の無限ループ、非ループサイクル | Critical |
| unreachable_node | エントリから到達不能なLLM/Toolノード | Warning |
| error_handler_checker | 外部I/Oノード後のエラーハンドリング欠落 | Critical |
| cost_estimation | ループ内の高額LLMモデル、単純タスクへの高額モデル適用 | Warning |
| redundant_llm_call | 同一prompt_template×modelの重複呼出 | Warning |

## サポートフォーマット

| Format | 状態 | 備考 |
|---|---|---|
| json | GA | Shingan独自のWorkflowGraph JSON |
| adk-go | GA | Google ADK-Go ソースコードのAST解析 |
| n8n | Planned (v0.2) | n8n JSON export |
| langgraph | Planned (v1.0) | Python AST経由 |

## ロードマップ

- **v0.1（現在）**: ADK-Go + JSON対応、5ルール、CLI
- **v0.2**: n8nパーサー、SARIF出力、GitHub Actions統合アクション
- **v0.3**: SamuraiAIアダプター、PIIリークルール
- **v1.0**: LangGraph/Dify対応、マルチフレームワーク安定版

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

## ランタイムデモ — Vertex AI Gemini 実行

Shingan が静的解析で検出したバグを、実際にVertex AI で走らせて**本当に問題になる**ことを実証する live demo。

### デモフロー

```bash
# バイナリビルド
go build -o shingan ./cmd/shingan
go build -o shingan-runner ./cmd/runner

# 全フロー自動実行（認証済み環境なら live 実行、未認証なら --dry-run を追加）
bash scripts/demo.sh
# または dry-run（Vertex AI呼び出しなし）
bash scripts/demo.sh --dry-run
```

| ステップ | コマンド | 期待結果 |
|---|---|---|
| 1. 静的解析でCritical警告 | `./shingan analyze --format adk-go --input examples/runtime/infinite_loop_unbounded.go --output markdown` | `cycle_detection` Critical, exit code 2 |
| 2. safe-guardで実行拒否 | `./shingan-runner --sample infinite_loop_unbounded --dry-run` | Critical検出 → 実行拒否 |
| 3. 安全版（MaxIter=3）実行 | `./shingan-runner --sample infinite_loop_bounded` | 3イテレーション後に正常終了 |
| 4. シンプルなLLM Agent実行 | `./shingan-runner --sample simple` | Geminiが日本語で挨拶 |

### shingan-runner の動作

```
1. examples/runtime/<sample>.go を Shingan で静的解析
2. Critical Finding があれば → 実行拒否（safe-guard）
3. クリーンなら → ADK-Go Runner + Vertex AI Gemini で実行
```

- `--max-iter N`: LoopAgent の MaxIterations を動的に注入（safe-guard override用）
- `--dry-run`: 解析のみ、Vertex AI呼び出しなし

詳細: [docs/runtime-demo.md](./docs/runtime-demo.md)

## アーキテクチャ詳細

- ADR全体 → [shingan-adr.md](./shingan-adr.md)
- 層構造・拡張ポイント → [docs/architecture.md](./docs/architecture.md)

## 開発

```bash
go test ./...
go vet ./...
go build -o shingan ./cmd/shingan
```

## ライセンス

MIT
