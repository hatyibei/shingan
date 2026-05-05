# examples/runtime — Shingan Runtime Demo Agents

このディレクトリには Shingan のランタイムデモ用 ADK-Go エージェント定義が含まれます。

## ファイル一覧

| ファイル | Shingan Finding | 実行可否 |
|---|---|---|
| `simple_agent.go` | なし（クリーン） | 実行可 |
| `infinite_loop_bounded.go` | なし（MaxIterations=3 で安全） | 実行可 |
| `infinite_loop_unbounded.go` | `cycle_detection` Critical | **実行拒否**（runner がガード） |

## 役割

各ファイルは2つの目的を持ちます:

1. **静的解析ターゲット**: `shingan analyze --format adk-go --input` で解析対象として読み込まれる
2. **パターン参照**: `cmd/runner/samples.go` の実際の実行用ビルダー関数と対応する

## 静的解析

```bash
# simple_agent.go — 問題なし
shingan analyze --format adk-go --input examples/runtime/simple_agent.go

# infinite_loop_bounded.go — 問題なし
shingan analyze --format adk-go --input examples/runtime/infinite_loop_bounded.go

# infinite_loop_unbounded.go — Critical 検出 (exit code 2)
shingan analyze --format adk-go --input examples/runtime/infinite_loop_unbounded.go --output markdown
```

## ランタイム実行

```bash
# 解析 + Vertex AI 実行（--dry-run なし）
shingan-runner --sample simple
shingan-runner --sample infinite_loop_bounded

# 解析のみ（実行なし）
shingan-runner --sample infinite_loop_unbounded --dry-run
# → Critical 検出で実行拒否
```

詳細は CHANGELOG.md / docs/architecture.md を参照。
