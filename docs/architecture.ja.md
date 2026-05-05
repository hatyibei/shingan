> 🌐 Language: [English](./architecture.md) | **日本語**

# Shingan アーキテクチャ詳細

```
作成日:   2026-04-14
対象バージョン: v0.1
```

---

## 1. 層構造と依存方向

Shinganは Onion Architecture を採用する。依存は常に外側から内側へのみ向かい、逆方向の依存は禁止。

```
┌──────────────────────────────────────────────────────────────────┐
│  cmd/                                                            │
│    shingan/main.go  — cobra コマンド定義、Factory 呼出、DI配線      │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │  infrastructure/                                           │  │
│  │    parser/      — JSON・ADK-Go パーサー実装                  │  │
│  │    reporter/    — Text・Markdown・JSON レポーター実装          │  │
│  │    factory/     — AnalyzerFactory・ParserFactory 実装        │  │
│  │  ┌──────────────────────────────────────────────────────┐  │  │
│  │  │  application/                                        │  │  │
│  │  │    orchestrator.go  — AnalysisOrchestrator           │  │  │
│  │  │    interfaces.go    — WorkflowParser・ReportFormatter  │  │  │
│  │  │  ┌────────────────────────────────────────────────┐  │  │  │
│  │  │  │  domain/                                       │  │  │  │
│  │  │  │    model/    — WorkflowGraph・Node・Edge         │  │  │  │
│  │  │  │    rule/     — AnalysisRule interface・Finding   │  │  │  │
│  │  │  │    analyzer/ — cycle・unreachable・error handler  │  │  │  │
│  │  │  │              — cost・redundant 各ルール実装        │  │  │  │
│  │  │  │    testutil/ — builder.go（テスト用グラフ構築）     │  │  │  │
│  │  │  └────────────────────────────────────────────────┘  │  │  │
│  │  └──────────────────────────────────────────────────────┘  │  │
│  └────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

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

- `model.WorkflowGraph` — ノード・エッジのグラフ表現
- `model.Node` — ノード種別（LLM / Tool / Loop / Branch 等）、メタデータ
- `model.Edge` — 有向エッジ、条件ラベル
- `rule.AnalysisRule` — 解析ルール interface（`Analyze(graph) []Finding`）
- `rule.Finding` — 検出結果（RuleID・Severity・Message・NodeID）
- `rule.Severity` — Info / Warning / Critical の列挙
- `analyzer/` — 5つの解析ルール実装（外部依存なし、純粋関数的）

domain 層は外部ライブラリを一切持ち込まない。これにより単体テストがモックなしで書ける。

### application/

- `WorkflowParser` interface — `Parse(input) (*WorkflowGraph, error)`
- `ReportFormatter` interface — `Format(findings) string`
- `AnalysisOrchestrator` — goroutine 並行でルールを実行し結果を集約

interface は**利用側**（application/）に定義する。実装側（infrastructure/）には定義しない（Dependency Inversion の原則）。

### infrastructure/

- `parser/json` — Shingan独自 JSON スキーマのデシリアライズ
- `parser/adkgo` — Go AST 解析（`go/parser`・`go/ast` 使用）でエージェント定義を抽出
- `reporter/text` / `reporter/markdown` / `reporter/json` — 出力形式実装
- `factory/` — AnalyzerFactory・ParserFactory・ReporterFactory の具象実装

### cmd/

- cobra コマンド定義（`analyze` サブコマンド）
- Factory を呼び出して依存を注入
- 終了コードの決定（最高 Severity → 0/1/2）

---

## 3. Factory Pattern 詳細

### AnalyzerFactory

```
AnalyzerFactory
  └── Build(rules []string) []domain.AnalysisRule
        ├── "cycle_detection"      → CycleDetector{}
        ├── "unreachable_node"     → UnreachableNodeDetector{}
        ├── "error_handler_checker"→ ErrorHandlerChecker{}
        ├── "cost_estimation"      → CostEstimator{}
        └── "redundant_llm_call"   → RedundantLLMDetector{}
```

新ルール追加時は `domain/analyzer/` にファイルを追加し、AnalyzerFactory のマップに1行登録するだけ。

### ParserFactory

```
ParserFactory
  └── Build(format string) application.WorkflowParser
        ├── "json"   → JSONParser{}
        └── "adk-go" → ADKGoParser{}
```

新フォーマット追加時は `infrastructure/parser/` に実装を追加し、ParserFactory に登録。

### ReporterFactory

```
ReporterFactory
  └── Build(output string) application.ReportFormatter
        ├── "text"     → TextReporter{}
        ├── "markdown" → MarkdownReporter{}
        └── "json"     → JSONReporter{}
```

---

## 4. 並行処理設計

`AnalysisOrchestrator.Run()` は全解析ルールを goroutine で並列実行する。

```
Run(graph *WorkflowGraph, rules []AnalysisRule) []Finding
  │
  ├── goroutine: rules[0].Analyze(graph) → ch
  ├── goroutine: rules[1].Analyze(graph) → ch
  ├── goroutine: rules[2].Analyze(graph) → ch
  ├── goroutine: rules[3].Analyze(graph) → ch
  └── goroutine: rules[4].Analyze(graph) → ch
                  ↓
          sync.WaitGroup で全完了待ち
                  ↓
          []Finding を集約して返却
```

**設計上の前提:**
- `graph` は読み取り専用（Analyze は graph を変更しない）
- `Finding` の書き込みは goroutine ごとに独立した slice → channel 経由で集約
- データ競合なし（`go test -race` でグリーンを維持）

---

## 5. 拡張ポイント

### 新しい解析ルールを追加する

1. `domain/analyzer/` に `<rule_name>.go` を作成し `AnalysisRule` interface を実装
2. `domain/analyzer/<rule_name>_test.go` を作成（testutil/builder.go でグラフを構築）
3. `infrastructure/factory/analyzer_factory.go` のマップに1行追加
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
