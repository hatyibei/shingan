> 🌐 Language: [English](./architecture.md) | **日本語**

# Shingan アーキテクチャ詳細

```
作成日:   2026-04-14
更新日:   2026-06-03
現行バージョン: v0.9
```

---

## 1. 層構造と依存方向

Shinganは Onion Architecture を採用する。依存は常に外側から内側へのみ向かい、逆方向の依存は禁止。

```
┌──────────────────────────────────────────────────────────────────┐
│  cmd/                                                            │
│    shingan/ api/ runner/ shingan-web/ shingan-lsp/ shingan-mcp/  │
│    shingan-gen/  — cobra コマンド、Factory 呼出、DI配線            │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │  infrastructure/                                           │  │
│  │    parser/      — 11 フレームワークパーサー（JSON・ADK-Go・   │  │
│  │                   LangGraph・CrewAI・n8n・… ）              │  │
│  │    reporter/    — Markdown・JSON・SARIF レポーター実装         │  │
│  │    factory/     — AnalyzerFactory・ParserFactory 実装        │  │
│  │    api/ baseline/ cache/  — サービス・ベースライン・キャッシュ   │  │
│  │  ┌──────────────────────────────────────────────────────┐  │  │
│  │  │  application/                                        │  │  │
│  │  │    orchestrator.go  — AnalysisOrchestrator           │  │  │
│  │  │    parser.go・reporter.go — 利用側 interface          │  │  │
│  │  │    policy.go・rule_catalog.go — .shingan.yaml・カタログ │
│  │  ┌────────────────────────────────────────────────┐  │  │  │
│  │  │  domain/                                       │  │  │  │
│  │  │    graph.go    — WorkflowGraph・Node・Edge       │  │  │  │
│  │  │    rule.go     — AnalysisRule + tier interface  │  │  │  │
│  │  │    finding.go  — Finding・Severity              │  │  │  │
│  │  │    rules/      — 22 個の組込ルール実装            │  │  │  │
│  │  │              （registry.go AllBuiltins()）       │  │  │  │
│  │  │    testutil/ — builder.go（テスト用グラフ構築）     │  │  │  │
│  │  └────────────────────────────────────────────────┘  │  │  │
│  │  └──────────────────────────────────────────────────────┘  │  │
│  └────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

> `domain` パッケージ本体がコア型（`graph.go`・`rule.go`・`finding.go`・
> `visitor.go`・`baseline.go`）を持ち、ルール**実装**は `domain/rules` サブ
> パッケージに置く（`domain/analyzer` というパッケージは存在しない）。

### 依存ルール（厳守）

| 層 | import 可 | import 不可 |
|---|---|---|
| domain/ | 標準ライブラリのみ | application/, infrastructure/, cmd/ |
| application/ | domain/ | infrastructure/, cmd/ |
| infrastructure/ | application/, domain/ | cmd/ |
| cmd/ | infrastructure/, application/, domain/ | — |

---

## 2. 各層の責務

### domain/

- `WorkflowGraph` — ノード・エッジのグラフ表現（`graph.go`）
- `Node` — ノード種別（LLM / Tool / Task / Loop / Branch 等）、メタデータ
- `Edge` — 有向エッジ、条件ラベル
- `AnalysisRule` — 旧来の解析ルール interface（`Analyze(graph) []Finding`）。
  新規ルールは `LocalRule` / `PathRule` / `GlobalRule` の tier interface
  （ADR-006/007）を実装し、単一走査の `GraphWalker` でディスパッチされる
- `Finding` — 検出結果（RuleName・Severity・Message・NodeID・Confidence）
- `Severity` — Info / Warning / Critical の列挙
- `rules/` — **22 個の組込ルール実装**。各ルールが自身の `init()` で
  レジストリへ自己登録し、`rules.AllBuiltins()` が全件を返す

domain 層は外部ライブラリを一切持ち込まない（標準ライブラリのみ）。これにより単体テストがモックなしで書ける。

### application/

- `WorkflowParser` interface — `Parse(input) (*WorkflowGraph, error)`
- `ReportFormatter` interface — `Format(findings) string`
- `AnalysisOrchestrator` — goroutine 並行でルールを実行し結果を集約

interface は**利用側**（application/）に定義する。実装側（infrastructure/）には定義しない（Dependency Inversion の原則）。

### infrastructure/

- `parser/` — **11 フレームワークパーサー**。各フレームワークを共通の
  `WorkflowGraph` IR へマッピングする：`json`（独自スキーマ）、`adkgo`
  （`go/parser` による Go AST）、`samurai`、`langgraph`、`n8n`、`crewai`、
  さらに v0.9 で追加した 5 つ（`langgraph-js`・`mastra`・`pydantic-graph`・
  `llamaindex`・`autogen`）。Python/TS 系パーサーはシムを常駐サブプロセス上で
  JSON-RPC 経由で動かし、n8n は純 Go。
- `reporter/markdown` / `reporter/json` / `reporter/sarif` — 出力形式実装
- `factory/` — AnalyzerFactory・ParserFactory・ReporterFactory の具象実装
- `api/`・`baseline/`・`cache/` — HTTP サービス、`--save-baseline`、解析キャッシュ

### cmd/

- cobra コマンド定義（`analyze` サブコマンド）
- Factory を呼び出して依存を注入
- 終了コードの決定（最高 Severity → 0/1/2）

---

## 3. Factory Pattern 詳細

### AnalyzerFactory

Factory はルールのマップを持たない。`rules.AllBuiltins()` に委譲し、各ルールが
自身の `init()` で自己登録した全件を返す。よってルール追加時に factory を触る
必要はない（ADR-010、internal-first Plugin SDK）。

```
AnalyzerFactory
  ├── Create(ruleType string) (domain.AnalysisRule, error)
  │     └── rules.AllBuiltins() を走査し Name() で照合
  └── CreateAll() []domain.AnalysisRule
        └── rules.AllBuiltins()   // 組込 22 個すべて
```

新ルール追加時は `domain/rules/` にファイルを置き、その `init()` で登録するだけ。

### ParserFactory

```
ParserFactory
  └── Build(format string) application.WorkflowParser
        ├── "json" "adk-go" "samurai" "langgraph" "n8n" "crewai"
        └── "langgraph-js" "pydantic-graph" "llamaindex" "autogen" "mastra"
```

新フォーマット追加時は `infrastructure/parser/` に実装を追加し、ParserFactory に登録。

### ReporterFactory

```
ReporterFactory
  └── Build(output string) application.ReportFormatter
        ├── "markdown" → MarkdownReporter{}
        ├── "json"     → JSONReporter{}
        └── "sarif"    → SARIFReporter{}
```

---

## 4. 並行処理設計

`AnalysisOrchestrator.Run()` は全解析ルールを goroutine で並列実行する。

```
Run(graph *WorkflowGraph, rules []AnalysisRule) []Finding
  │
  ├── goroutine: rules[0].Analyze(graph) → ch
  ├── goroutine: rules[1].Analyze(graph) → ch
  ├── … 1 ルール 1 goroutine（既定で組込 22 個）…
  └── goroutine: rules[n-1].Analyze(graph) → ch
                  ↓
          sync.WaitGroup で全完了待ち
                  ↓
          []Finding を集約して返却
```

> tier interface（`LocalRule` / `PathRule` / `GlobalRule`）で実装された
> 単一ノード・単一パス系ルールは、各々がグラフを再走査せず、共有の
> `GraphWalker` の単一走査でディスパッチされる（ADR-006/007）。上記の
> `AnalysisRule.Analyze` 形は旧来／互換ビュー。

**設計上の前提:**
- `graph` は読み取り専用（Analyze は graph を変更しない）
- `Finding` の書き込みは goroutine ごとに独立した slice → channel 経由で集約
- データ競合なし（`go test -race` でグリーンを維持）

---

## 5. 拡張ポイント

### 新しい解析ルールを追加する

1. `domain/rules/` に `<rule_name>.go` を作成し、tier interface
   （`LocalRule` / `PathRule` / `GlobalRule`）— もしくは旧来の `AnalysisRule` —
   を実装し、`init()` で自己登録する
2. `domain/rules/<rule_name>_test.go` を作成（testutil/builder.go でグラフを構築）
3. factory の編集は不要 — `rules.AllBuiltins()` が自動的に拾う
4. `go test ./... && go vet ./...` がグリーンであることを確認

### 新しいパーサーを追加する

1. `infrastructure/parser/<format>/parser.go` を作成し `application.WorkflowParser` を実装
2. `infrastructure/factory/parser_factory.go` に分岐を追加
3. `testdata/<format>/` にサンプルファイルを追加してテスト

### 新しいReporterを追加する

1. `infrastructure/reporter/<format>/reporter.go` を作成し `application.ReportFormatter` を実装
2. `infrastructure/factory/reporter_factory.go` に分岐を追加

---

## 6. ADR索引

詳細な設計判断の経緯は `shingan-adr.md` を参照。

| ADR | タイトル |
|---|---|
| ADR-001 | プロダクト選定 — なぜ「AIエージェントワークフローの静的解析」か |
| ADR-002 | 解析対象フレームワークの選定 |
| ADR-003 | アーキテクチャ設計（Onion Architecture + Factory Pattern） |
| ADR-004 | インフラストラクチャ設計（パーサー・レポーター・CLI） |
| ADR-005 | 実装スコープとスケジュール |
| Appendix A | 用語集 |
| Appendix B | SamuraiAI ↔ ADK-Go ノードマッピング |
| Appendix C | 解析ルール詳細仕様 |
