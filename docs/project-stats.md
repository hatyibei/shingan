# Shingan v0.2.0 — 実装規模スナップショット

> 計測日: 2026-04-15 (v0.2.0)
> 計測コマンド: `git ls-files | xargs wc -l`

## コード量

| 区分 | v0.1.0 | v0.2.0 | 増減 |
|---|---|---|---|
| Go (全体) | 11,178 | **14,339** | +3,161 |
| Go (テスト含む) | 4,774 | 6,537 | +1,763 |
| Go (本体) | 6,404 | 7,802 | +1,398 |
| Markdown | 2,674 | 4,744 | +2,070 |
| Tracked files | 120 | **183** | +63 |

## テスト

| 区分 | v0.1.0 | v0.2.0 |
|---|---|---|
| Test/Benchmark 関数 | 206 | **298** |
| テストパッケージ | 8 | 10 |
| `go test -race` | 全green | 全green |
| Benchmark 計測 | 21 | 21+ (PII最適化の実測再取得) |

## エントリポイント (5バイナリ)

| バイナリ | 用途 |
|---|---|
| `shingan` | CLI 静的解析 |
| `shingan-gen` | **NEW** サンプルワークフロー生成 (7パターン) |
| `shingan-api` | goa v3 HTTP API + OpenAPI |
| `shingan-runner` | Vertex AI Gemini safe-guard実行 |
| `shingan-web` | ADK Web UI + Shingan middleware |

## 解析ルール (v0.1時点 7個)

| Rule ID | ファイル |
|---|---|
| cycle_detection | domain/rules/cycle.go |
| loop_guard | domain/rules/loopguard.go |
| unreachable_node | domain/rules/reachability.go |
| error_handler_checker | domain/rules/errorhandler.go |
| cost_estimation | domain/rules/cost.go |
| redundant_llm_call | domain/rules/redundant.go |
| pii_leak_scanner | domain/rules/pii_leak.go |

## 入力/出力フォーマット

- **入力**: json / adk-go (AST解析) / samurai (skeleton)
- **出力**: json / markdown / sarif

## アーキテクチャ層

```
cmd/             (4ディレクトリ)
└─ infrastructure/  (parser/ reporter/ factory/ api/)
   └─ application/  (orchestrator, parser, reporter interfaces)
      └─ domain/    (graph, finding, rule + 7 rules, testutil)
```

Onion Architecture違反: **0** (domain/ 外部依存ゼロ維持)

## Factory Pattern 3箇所

| Factory | 対応フォーマット数 |
|---|---|
| AnalyzerFactory | 7 rules |
| ParserFactory | 3 (json, adk-go, samurai) |
| ReporterFactory | 3 (json, markdown, sarif) |

## ADR / ドキュメント

- `shingan-adr.md` — ADR-001〜005 + Appendix A/B/C
- `docs/` — 9+ 個（architecture, runtime-demo, sarif-output, samurai-adapter, cycle-detection-note, adk-webui-integration, pii-detection, interview-cheatsheet, preparation-checklist, reverse-questions, project-stats）

## 依存関係

- `google.golang.org/adk v1.1.0` — ADK-Go公式SDK、AST解析 + Web UI
- `goa.design/goa/v3` — Design-first API
- `github.com/spf13/cobra` — CLI
- `github.com/gorilla/mux` — ADK-Go内部で使用、middleware注入

## 開発期間

- ADR設計: 1日 (2026-04-14)
- 実装: 1.5日 (2026-04-14〜15)
- v0.1.0: **計 2.5日**

## CI

- GitHub Actions (lint / test + coverage / build 4binaries)
- Go 1.25
- 全コミット green
