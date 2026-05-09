> 🌐 Language: [English](./README.md) | **日本語**

# Shingan（心眼）

> AI Agent Workflow Static Analyzer

![Go version](https://img.shields.io/badge/Go-1.23-00ADD8?logo=go) ![License](https://img.shields.io/badge/License-MIT-green) ![CI](https://github.com/hatyibei/shingan/actions/workflows/ci.yml/badge.svg)

AIエージェントのワークフローを実行前に構造解析し、無限ループ・到達不能ノード・エラーハンドリング欠落・コスト非効率・冗長LLM呼出を検出する Go製静的解析ツール。

## なぜShinganか

LLM オーケストレーションが普及した現在、ワークフローの「設計時バグ」を検出するカテゴリが空白になっている。FlowLint は n8n 専用、LangSmith はランタイム観測に特化しており、いずれも **実行前** の構造検査には対応していない。

AI agent は一度実行すると副作用が不可逆になる (外部 API 呼出、ブラウザ操作、コード実行)。無限ループ・到達不能ノード・エラーハンドリング欠落・PII漏洩経路・prompt injection sink を **デプロイ前** に機械的に検出できれば、コスト爆発とインシデントの大半を未然に防げる。

LangGraph / ADK-Go / CrewAI / n8n / 自前 JSON DSL — どのフレームワークでも、ワークフロー構造の本質は同じ「nodes と edges の有向グラフ」。Shingan は IR (中間表現) を中心に据えた Onion Architecture で、フレームワーク非依存に設計時バグを 20+ ルールで検出する。

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

### npm (推奨、ゼロセットアップ)

```bash
# 一回だけ実行
npx shingan-lint analyze --format=langgraph ./agents/

# プロジェクトに固定
pnpm add -D shingan-lint
pnpm exec shingan analyze --since main

# グローバルに
npm install -g shingan-lint
shingan analyze --input ./testdata/buggy.json
```

[`shingan-lint`](https://www.npmjs.com/package/shingan-lint) は薄い Node ラッパで、`postinstall` で plat-specific Go バイナリを GitHub Release から取得 + SHA256 検証 + `~/.cache/shingan-lint/v<ver>/` にキャッシュする。Linux / macOS / Windows × amd64 / arm64 を全部サポート。

### Go install (Go 開発者向け)

```bash
go install github.com/hatyibei/shingan/cmd/shingan@latest
```

### ソースからビルド

```bash
git clone https://github.com/hatyibei/shingan.git
cd shingan
go build -o shingan ./cmd/shingan
```

### Docker

```bash
docker pull ghcr.io/hatyibei/shingan:latest
docker run --rm -v "$(pwd)":/work ghcr.io/hatyibei/shingan analyze --input /work/buggy.json
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

LangGraph (Python) 入力の例:

```bash
# 事前: pip install langgraph
shingan analyze --format langgraph --input agent.py --output markdown
shingan analyze --format langgraph --input ./agents/ --output sarif --output-file findings.sarif
```

終了コード: `0` = Info以下のみ、`1` = Warning検出、`2` = Critical検出

CI統合例（GitHub Actions）:

```yaml
- name: Shingan check
  run: shingan analyze --format adk-go --input ./agents/
```

## 本物のADK-Goサンプルでのデモ

`examples/real/` に配置した `google.golang.org/adk v1.1.0` SDK準拠のサンプル3種に対してShinganが検出するFinding:

| Sample | Rule | Severity | 結果 |
|---|---|---|---|
| examples/real/infinite_loop.go | cycle_detection | Critical | loopagent.New + MaxIterations未設定で無限ループを検出 |
| examples/real/unreachable.go | unreachable_node | Warning | orphan_analyzerが orchestratorのSubAgentsに未接続で到達不能を検出 |
| examples/real/missing_handler.go | error_handler_checker | Warning | plannerがbrowser_searchツールを使うが条件分岐なし → 対応済み |

実行:

```bash
shingan analyze --format adk-go --input examples/real/infinite_loop.go --output markdown
# exit code 2 (Critical)

shingan analyze --format adk-go --input examples/real/unreachable.go --output markdown
# exit code 1 (Warning)

shingan analyze --format adk-go --input examples/real/missing_handler.go --output markdown
# exit code 2 (Critical: loop_guard + Warning: error_handler_checker)
```

**公式ADK-Go SDKとの差分・注記:**

- `loopagent.New(loopagent.Config{AgentConfig: agent.Config{SubAgents: ...}})` パターンに対応済み（v1.1.0）
- LlmAgent / SequentialAgent / LoopAgent の `New()` コンストラクタパターンを AST で検出
- `functiontool.New(Config{Name: "..."}, handler)` で登録したツールのノード検出に対応済み（`Config.Name` フィールド経由でツール名を取得、ident参照を解決）
- `functiontool.New[TArgs, TResults](...)` のジェネリクス型引数を `go/types` セカンドパスで解析し、TArgs の struct フィールド名から Tool カテゴリを推定（v0.2.0 で対応済み、`ParseFile` API 経由）。`missing_handler.go` の `browser_search` ツールが `functiontool.New` 経由で正しく検出される
- LLMノードがToolノードへのエッジを持つがエラーハンドリング分岐がない場合も `error_handler_checker` が Warning を発火（ADK-Go の LLM→Tool エッジパターンに対応）
- ADK-Go SDK は v1.1.0 で `go 1.25.0` 以上が必要（go.mod のminimum versionに反映済み）

```bash
# demo タグでE2E自動検証
go test -tags=demo -v -run TestDemo_ .
```

## 解析ルール一覧

| Rule ID | 検出対象 | 最高 Severity | Confidence |
|---|---|---|---|
| cycle_detection | 非Loopノードのサイクル、LoopAgent管理下のサイクル | Critical | 1.0 (確定) |
| loop_guard | LoopAgent (Loop型) のMaxIterations未設定 | Critical | 1.0 (確定) |
| unreachable_node | エントリから到達不能なLLM/Toolノード | Warning | 1.0 (確定) |
| error_handler_checker | 外部I/Oノード後のエラーハンドリング欠落 | Critical | 0.8 (ヒューリスティック) |
| cost_estimation | ループ内の高額LLMモデル、単純タスクへの高額モデル適用 | Warning | 0.7 (価格変動あり) |
| redundant_llm_call | 同一prompt_template×modelの重複呼出 | Warning | 0.9 (完全一致) |
| pii_leak_scanner | RAG/PII源ノードから外部シンクへのHuman gate なしパス | Warning | 0.6 (RAG) / 0.3 (名前ヒント) |
| secret_exposure_scanner | Node.Config にハードコードされた APIキー・シークレット | Critical | 0.95 (Critical/Warning) / 0.5 (Info) |
| max_parallel_branches | 単一ノードの fan-out (outgoing edges数) が上限超過 | Critical | 1.0 (Critical) / 0.9 (Warning) / 0.7 (Info) |
| deprecated_model | Shutdown / 近日 deprecated 予定の LLM モデル名 (OpenAI / Anthropic / Google) | Critical | 1.0 (shutdown) / 0.9 (deprecated soon) |
| prompt_injection_sink | user_input → LLM の system prompt template への到達 (substitution あり=Critical / なし=Warning / non-system template=Info) | Critical | 0.9 (Critical) / 0.7 (Warning) / 0.5 (Info) |
| eval_missing | LLM ノード → コード実行系 Tool (eval/exec/code_interpreter/python_runner/shell) への到達 (validation gate なし=Critical / Condition のみ=Warning / Human gate=skip) | Critical | 0.9 (Critical) / 0.6 (Warning) |
| dynamic_node_construction | Node.Config (`body`/`fn`/`handler`/`callback`/`code`/`factory`/`builder`) 内の `eval(`/`exec(`/`Function(`/`compile(`/`__import__(`/`getattr(`/`setattr(` 直書き | Critical | 0.95 (Critical) / 0.85 (Warning) / 0.6 (Info) |

## サポートフォーマット

### 入力

| Format | 状態 | 備考 |
|---|---|---|
| langgraph | **Phase 1 primary** (ADR-011) | Python `langgraph.graph.StateGraph` を long-lived Python subprocess + JSON-RPC で抽出。`pip install langgraph` 別途必要 ([詳細](./docs/langgraph.md)) |
| adk-go | GA / maintained | Google ADK-Go (`google.golang.org/adk`) のAST解析 |
| json | GA | Shingan独自のWorkflowGraph JSON |
| samurai | Alpha | 汎用 GUI ワークフローエディタ向け JSON スキーマアダプタ (拡張サンプル) |
| n8n | **Beta** | n8n ワークフロー JSON エクスポート、純 Go (Python / Node bridge 不要) ([詳細](./docs/n8n.md)) |
| crewai | **Beta** | CrewAI Crew/Agent/Task 定義を Python long-lived subprocess + JSON-RPC で抽出。`pip install "crewai>=0.50.0"` 必要 ([詳細](./docs/crewai.md)) |

### IDE / Editor 統合

| 統合 | 状態 | 備考 |
|---|---|---|
| CLI (`shingan analyze`) | GA | コア体験、`--since`/`--baseline` 対応 |
| GitHub Action | GA | `action.yml`、SARIF出力で Code Scanning 連携 |
| MCP Server (`shingan-mcp`) | GA | Claude Desktop / Cursor / LangGraph Studio から呼出 |
| **LSP Server (`shingan-lsp`)** | **Beta** | VS Code / Cursor / Neovim / Helix / Zed / JetBrains。SHA256 LRU 差分キャッシュ + degraded mode (ADR-009)。詳細: [docs/lsp.md](./docs/lsp.md) |
| VS Code 拡張 (`vscode-shingan`) | Beta | `extensions/vscode-shingan/`、`shingan-lsp` を spawn |

### 出力

| Format | ContentType | 用途 |
|---|---|---|
| json | application/json | API応答、プログラム連携 |
| markdown | text/markdown | CLI、レポート |
| sarif | application/sarif+json | GitHub Code Scanning統合 |

## ロードマップ

- **v0.1〜v0.5** (2026-04): JSON / ADK-Go / Samurai parser、Confidence × Severity 二次元、SARIF / GitHub Action、9 ルール ✓
- **v0.6** (2026-05): ESLint方式 visitor + 3層分離 (ADR-006/007)、shingan-lsp、shingan-mcp、LangGraph parser、20 ルール、`shingan-lint` npm 配布、tag→Release→npm-publish 自動化 ✓
- **v0.7** (May 2026): n8n parser (純 Go、JSON DSL)、bilingual EN/JA docs ✓
- **v0.8** (May 2026): CrewAI parser (Python shim、LangGraph PythonWorker 再利用)、6 frameworks 対応 ✓
- **v0.9+**: Mastra parser (TypeScript bridge)、ルール 30+、Plugin SDK 公開準備、公式サイト + 動画
- **v1.0**: 5+ framework × 25+ rules、Plugin SDK GA、Marketplace 公開

## 開発

```bash
go test ./...
go vet ./...
go build -o shingan ./cmd/shingan
make lint        # check_confidence_reason + go vet
```

新ルール追加時は [docs/rule-authoring.md](./docs/rule-authoring.md) を参照。

## ドキュメント

- [アーキテクチャ詳細](./docs/architecture.md)
- [ルール作成ガイド (内部)](./docs/rule-authoring.md)
- **フレームワーク parser**: [LangGraph](./docs/langgraph.md) · [CrewAI](./docs/crewai.md) · [n8n](./docs/n8n.md)
- **ケーススタディ (実 OSS dogfood)**: [crewAI-examples](./docs/case-studies/crewAI-examples.md) · [n8n community workflows](./docs/case-studies/n8n-community-workflows.md) · [gpt-researcher](./docs/case-studies/gpt-researcher.md) — [index](./docs/case-studies/README.md)
- [LSP server (`shingan-lsp`) — VS Code / Neovim / Helix / Zed setup](./docs/lsp.md)
- [MCP server (`shingan-mcp`) — Claude Desktop / Cursor / LangGraph Studio setup](./docs/mcp-server.md)
- [SARIF 出力 + GitHub Code Scanning 統合](./docs/sarif-output.md)
- [diff モード + baseline (`--since` / `--baseline`)](./docs/diff-mode.md)
- [Confidence scoring](./docs/confidence-scoring.md)
- [cycle_detection の技術ノート](./docs/cycle-detection-note.md)
- [全 ADR (001〜013)](./shingan-adr.md)

### Contributing → New rules

新しい builtin rule を実装する内部 contributor は **[docs/rule-authoring.md](./docs/rule-authoring.md)** を参照してください。 Local / Path / Global の 3 層 (ADR-007) のテンプレート、ConfidenceReason 選択ガイド (ADR-008)、`check_confidence_reason.sh` linter、TDD パターン、既存 10 ルールの設計記録を網羅しています。 ADR-010 の方針により Plugin SDK は v1.0 まで internal-only — 外部 contributor は fork → upstream PR の経路で参加してください。

## ライセンス

MIT
