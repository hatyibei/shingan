# Self-Dogfood — ShinganをShinganで解析

> 2026-04-15 初回実施 / 2026-04-15 v0.2修正完了

## 目的

「Shinganは自分のワークフロー (AnalysisOrchestrator のパイプライン) を WorkflowGraph として表現したとき、どう解析するか」を確認する。メタ検証。

## 入力

`testdata/meta/shingan_pipeline.json` に、Shinganの解析パイプラインを WorkflowGraph として定義:

```
parse_input (Tool)
 └─ parse_error_branch (Condition: if parse fails)   ← v0.2: control → condition
     ├─ [err==nil] orchestrator (Loop: fan-out 7 rules, max_iterations=1)
     │   ├─ rule_cycle, rule_loop_guard, ..., rule_pii (各LLM)
     │   └─ aggregate (Loop, max_iterations=1) → sort_by_severity (Tool, reliable=true) → format_output (Tool)
     │       └─ format_error_branch (Condition: if format fails)   ← v0.2: control → condition
     │           ├─ [err==nil] output (Output)
     │           └─ [err!=nil] error_out (Output)
     └─ [err!=nil] error_out (Output)
```

## 解析結果

### v0.1 (初回) — 誤検知5件

```bash
./shingan analyze --format json --input testdata/meta/shingan_pipeline.json --output markdown
# Total: 5 | Critical: 2 | Warning: 0 | Info: 3
```

#### Critical: 2件 — **自己誤検知**

| Rule | Node | 誤検知の理由 |
|---|---|---|
| loop_guard | parse_error_branch | Control型だが、if-else分岐であってループではない。max_iterations 無くて当然 |
| loop_guard | format_error_branch | 同上 |

#### Info: 3件 — **誤検知**

| Rule | Node | 誤検知の理由 |
|---|---|---|
| error_handler_checker | parse_input | 直後の `parse_error_branch` で `err != nil` 条件分岐を行っているが、ルールは「Tool→次ノード」単体しか見ていない |
| error_handler_checker | format_output | 同上、直後に `format_error_branch` あり |
| error_handler_checker | sort_by_severity | sort.SliceStable は失敗しない決定的アルゴリズム |

### v0.2 (修正後) — 誤検知0件 ✓

```bash
./shingan analyze --format json --input testdata/meta/shingan_pipeline.json --output json
# Total: 0 | Critical: 0 | Warning: 0 | Info: 0
```

**全5件の誤検知を解消**。

## 根本原因と修正

### [Issue 1: 解決済み] Control型を `NodeTypeLoop` と `NodeTypeCondition` に分割

`NodeTypeControl` (iota=2) を deprecated として残しつつ、新型を追加:

```go
NodeTypeLoop      // NEW (iota=5): LoopAgent相当（max_iterations必須）
NodeTypeCondition // NEW (iota=6): if/switch相当（max_iterations不要）
```

後方互換: JSON `"control"` 文字列は `NodeTypeLoop` として扱う。

`LoopGuardChecker` は `NodeTypeLoop` と deprecated `NodeTypeControl` のみを対象にする。
`NodeTypeCondition` は完全に対象外。

**効果**: Critical 2件解消 (parse_error_branch, format_error_branch が Condition になり loop_guard 対象外)

### [Issue 2: 解決済み] `ErrorHandlerChecker` の2ホップ先追跡

Tool ノード直後が `NodeTypeCondition` で、その先に条件付きエッジがあれば「エラーハンドリングあり」と判定。

**効果**: Info 2件解消 (parse_input, format_output)

### [Issue 3: 解決済み] 信頼性フラグ

`Config["reliable"] == true` のツール（純粋関数、決定的アルゴリズム）は `error_handler_checker` の対象外。

**効果**: Info 1件解消 (sort_by_severity)

## 学び

**Shingan自身のパイプラインを入力して5件のFindingが出たが、うち5件すべてが誤検知**だった。
v0.2 で NodeType 拡張 + 2ホップ追跡 + reliable フラグ で全件解消。

誤検知率の定量管理の重要性を示す一次証拠として、面接でも語れる。
OSSとして磨き込む上では「自己検証(self-dogfood)→誤検知特定→v.nextで修正」のサイクルが重要。
