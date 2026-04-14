# Changelog

All notable changes to Shingan are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), versioning follows [SemVer](https://semver.org/).

## [Unreleased]

## [0.3.0] - 2026-04-15

### Added
- `secret_exposure_scanner` rule — 8つのシークレットパターンを検出 (AWS/OpenAI/Anthropic/GitHub/Slack/JWT/Generic)
  - Critical: AWS Access Key (`AKIA...`), PEM秘密鍵, OpenAI/Anthropic APIキー
  - Warning: GitHub Token (`gh[pousr]_...`), Slack Bot Token (`xox[bpars]-...`)
  - Info: JWT (`eyJ...`), Generic パターン (password=XXX, token=XXX)
  - 除外ロジック: `${VAR}`, `{{placeholder}}`, `process.env.X`, `os.Getenv()` は誤検知なし
  - 対象: `Node.Config` の string / map / slice 値を再帰的にスキャン
- `testdata/secrets/exposed.json` — AWS/OpenAI/Anthropic キーをハードコードしたサンプル (Critical×3)
- `testdata/secrets/safe.json` — 環境変数参照のみのサンプル (0件)
- `domain/testutil/generate.go`: `GenerateSecretExposureGraph(seed)` — Critical 発火パターン生成関数追加
- `cmd/shingan-gen`: `--pattern secret-exposure` オプション追加
- `testdata/generated/secret-exposure-seed42.json` — シード42の生成済みサンプル
- `docs/secret-detection.md` — 検出パターン一覧、Severity判定、除外ロジック解説、v0.4 entropy scanner 予定

## [0.2.0] - 2026-04-15

### Added
- `cmd/shingan-gen` CLI — 7パターンのワークフロー生成 (random, clean, buggy, infinite-loop, unreachable, pii-leak, cycle)
  - `--pattern`, `--size`, `--seed`, `--output` フラグ対応
  - `shingan analyze --format json` と完全互換のJSON出力（nodes配列形式）
  - シード固定による再現性保証
- `domain/testutil/generate.go` — 6つのパターン生成関数を追加
  - `GenerateCleanGraph(n, seed)` — 全7ルールをパスする正常グラフ
  - `GenerateInfiniteLoopGraph(seed)` — loop_guard + cycle_detection 発火
  - `GenerateUnreachableGraph(n, seed)` — unreachable_node 発火
  - `GeneratePIILeakGraph(seed)` — pii_leak_scanner 発火
  - `GenerateCycleGraph(n, seed)` — cycle_detection (非Loopノード) 発火
  - `GenerateBuggyGraph(seed)` — 全7ルール同時発火
- `testdata/generated/` — 各パターンの生成済みサンプルJSON (7ファイル、合計約64KB)
- `docs/sample-generator.md` — shingan-gen 使い方ガイド、パターン解説、教育目的活用法
- `Makefile`: `gen-cli`、`sample-%` ターゲット追加
- `infrastructure/parser/adkgo.go` に `go/types` ベースのセカンドパス追加。`functiontool.New[TArgs, TResults]` のジェネリクス型引数を `types.Info.Instances` で取得し、TArgs の struct 名・フィールド名から Tool カテゴリを推定。`ParseFile(path string)` API 経由で利用可能。ロード失敗時は自動で AST-only にフォールバック
- `ADKGoParser` に `WithoutTypes()` オプション追加。型情報パスを無効化してAST-onlyパスを強制（テスト高速化、ネットワーク非接続環境向け）
- `NodeTypeLoop` (iota=5) — LoopAgent相当。`max_iterations` 必須。ADK-Go の `LoopAgent` はこの型に解析される。
- `NodeTypeCondition` (iota=6) — if/switch/条件分岐相当。`max_iterations` 不要。
- `testutil.Builder.AddLoopNode(id, maxIter)` / `AddConditionNode(id, expression)` ヘルパーメソッド
- `ErrorHandlerChecker` に2ホップ追跡 — Tool直後が Condition ノードで条件付きエッジを持つ場合、エラーハンドリングあり判定
- `ErrorHandlerChecker` に `Config["reliable"]=true` フラグサポート — 決定的アルゴリズム（sort等）を除外
- `loopguard.go` に `isLoopNode()` ヘルパー関数（NodeTypeLoop + deprecated NodeTypeControl を判定）

### Changed
- `PIILeakScanner` を O(sources · (V+E)) から O(V+E) に最適化。逆方向隣接リスト事前構築 + Sink起点逆BFS により、n=1000 で 275ms → 26ms（10倍以上の改善）。既存テスト (11ケース) 互換性を維持しつつ大規模ケース2件を追加。
- `NodeTypeControl` を deprecated に変更。JSON `"control"` 文字列は後方互換のため `NodeTypeLoop` として扱う（iota値 2 は維持）。
- `LoopGuardChecker` の対象を `NodeTypeLoop` / deprecated `NodeTypeControl` のみに変更。`NodeTypeCondition` は対象外。
- `CycleDetector` の Loop 判定を `isLoopNode()` に統一。`NodeTypeCondition` 単体のサイクルは Critical（グラフ定義誤り）のまま。
- `ADKGoParser`: `LoopAgent` → `NodeTypeLoop`（従来は NodeTypeControl）。Sequential/Parallel は NodeTypeControl のまま。
- `SamuraiParser`: `"loop"` → `NodeTypeLoop`、`"condition"` → `NodeTypeCondition`（従来は両方 NodeTypeControl）。
- `reachability.go` の `nodeTypeName()` に Loop/Condition ケース追加。
- `testdata/meta/shingan_pipeline.json`: `parse_error_branch`, `format_error_branch` を `condition` 型に変更。`sort_by_severity` に `reliable=true` を追加。

### Fixed
- `loop_guard` が条件分岐ノード (`parse_error_branch`, `format_error_branch`) を誤検知 (Critical×2)
- `error_handler_checker` が Condition ノードを介したエラーハンドリングを見逃す誤検知 (Info×2)
- `error_handler_checker` が決定的ツール (`sort_by_severity`) を誤検知 (Info×1)
- 上記5件すべてが `testdata/meta/shingan_pipeline.json` で0件になることを自己検証確認 (`docs/self-dogfood.md`)

## [0.1.0] - 2026-04-15

### Added
- 7 analysis rules:
  - `pii_leak_scanner` (v0.3 preview) — RAG→外部送信パスでHuman gateなし
- functiontool.New() 経由で登録したToolのAST検出対応（error_handler_checker強化）
- Playwright スクリーンショット自動化スクリプト (`scripts/screenshots/`、10枚)
- Marp 面接プレゼンスライド15枚 (HTML生成済、`slides/pitch.md`)
- GitHub Action (`action.yml`) — `uses: hatyibei/shingan@v0.1.0`
- Multi-stage Dockerfile (distroless, 4バイナリ)
- Performance benchmarks (`domain/rules/bench_test.go`, `application/bench_test.go`, `infrastructure/parser/bench_test.go`) + `docs/benchmarks.md`
- Self-dogfood verification (`docs/self-dogfood.md`) — 既知誤検知5件の文書化
- 6 base analysis rules:
  - `cycle_detection` — 非Controlノードのサイクル、LoopAgent管理下のサイクル
  - `loop_guard` — Control型ノードのMaxIterations未設定検出（独立ルール）
  - `unreachable_node` — エントリから到達不能なLLM/Tool
  - `error_handler_checker` — 外部I/Oノード後のエラーハンドリング欠落
  - `cost_estimation` — ループ内高額LLMモデル、単純タスクへの高額モデル適用
  - `redundant_llm_call` — 同一prompt_template×modelの重複呼出
- 3 input formats:
  - `json` — Shingan独自のWorkflowGraph JSON
  - `adk-go` — Google ADK-Go (`google.golang.org/adk v1.1.0`) のAST解析
  - `samurai` (Alpha) — SamuraiAI想定スキーマのParser skeleton
- 3 output formats:
  - `json` (API応答向け)
  - `markdown` (CLI・レポート向け)
  - `sarif` (GitHub Code Scanning統合)
- 4 entry points:
  - `cmd/shingan` — CLI (cobra)
  - `cmd/api` — goa v3 Design-first HTTP API + 自動生成OpenAPI
  - `cmd/runner` — Vertex AI Gemini でADK-Go Agentを安全実行
  - `cmd/shingan-web` — ADK Web UI + Shingan pre-execution middleware
- Onion Architecture + Factory Pattern 3箇所 (Analyzer/Parser/Reporter)
- goroutine並行ルール実行 (`sync.WaitGroup` + `chan []Finding`)
- CI: GitHub Actions (Go 1.25, lint/test/build, coverage artifact)
- Issue/PR templates, CONTRIBUTING.md
- ADR 5件 (shingan-adr.md) + docs/ (architecture, runtime-demo, sarif-output, samurai-adapter, cycle-detection-note, adk-webui-integration)

### Notable architectural decisions
- Go + goa + Onion Architecture + Factory Pattern を採用（Kivaスタック準拠）
- Design-first API契約で OpenAPI/実装のドリフトを構造的にゼロに
- 解析ルールはstatelessでWorkflowGraph読み取り専用 → goroutine並行実行が自然に成立
