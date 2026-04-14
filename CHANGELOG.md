# Changelog

All notable changes to Shingan are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), versioning follows [SemVer](https://semver.org/).

## [Unreleased]

### Added
- PII Leak Scanner rule (v0.3 preview) for enterprise compliance (RAG→external tool path detection)
- functiontool.New() 経由で登録したToolのAST検出対応（error_handler_checker強化）
- Playwright スクリーンショット自動化スクリプト (`scripts/screenshots/`)

## [0.1.0] - 2026-04-15

### Added
- 6 analysis rules:
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
