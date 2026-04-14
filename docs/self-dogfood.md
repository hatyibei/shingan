# Self-Dogfood — ShinganをShinganで解析

> 2026-04-15 実施

## 目的

「Shinganは自分のワークフロー (AnalysisOrchestrator のパイプライン) を WorkflowGraph として表現したとき、どう解析するか」を確認する。メタ検証。

## 入力

`testdata/meta/shingan_pipeline.json` に、Shinganの解析パイプラインを WorkflowGraph として定義:

```
parse_input (Tool)
 └─ parse_error_branch (Control: if parse fails)
     ├─ [err==nil] orchestrator (Control: fan-out 7 rules)
     │   ├─ rule_cycle, rule_loop_guard, ..., rule_pii (各LLM)
     │   └─ aggregate (Control) → sort_by_severity (Tool) → format_output (Tool)
     │       └─ format_error_branch (Control: if format fails)
     │           ├─ [err==nil] output (Output)
     │           └─ [err!=nil] error_out (Output)
     └─ [err!=nil] error_out (Output)
```

## 解析結果

```bash
./shingan analyze --format json --input testdata/meta/shingan_pipeline.json --output markdown
# Total: 5 | Critical: 2 | Warning: 0 | Info: 3
```

### Critical: 2件 — **自己誤検知を発見**

| Rule | Node | 誤検知の理由 |
|---|---|---|
| loop_guard | parse_error_branch | Control型だが、if-else分岐であってループではない。max_iterations 無くて当然 |
| loop_guard | format_error_branch | 同上 |

**誤検知の根本原因**: 現在 `NodeTypeControl` は「ループ」と「条件分岐」を区別していない。
`LoopGuardChecker` は「Control型 → max_iterations 必須」と一律判定している。

### Info: 3件 — **これも誤検知**

| Rule | Node | 誤検知の理由 |
|---|---|---|
| error_handler_checker | parse_input | 直後の `parse_error_branch` で `err != nil` 条件分岐を行っているが、ルールは「Tool→次ノード」単体しか見ていない |
| error_handler_checker | format_output | 同上、直後に `format_error_branch` あり |
| error_handler_checker | sort_by_severity | 直後が `format_output` (Tool)。sortは失敗しないので本来は指摘不要 |

**誤検知の根本原因**:
- `ErrorHandlerChecker` は「Toolノードの outbound エッジのうち Condition付きがあるか」を見ているが、**2ホップ先まで追跡していない**
- また、「失敗しないTool (決定的アルゴリズム)」を `Config["reliable"] = true` で除外する仕組みが未実装

## 学び・v0.2 改修方針

### [Issue 1] Control型を `NodeTypeLoop` と `NodeTypeCondition` に分割

```go
// domain/graph.go
type NodeType int
const (
    NodeTypeLLM NodeType = iota
    NodeTypeTool
    NodeTypeLoop      // NEW: LoopAgent相当（max_iterations必須）
    NodeTypeCondition // NEW: if/switch相当（max_iterations不要）
    NodeTypeHuman
    NodeTypeOutput
)
```

`LoopGuardChecker` は `NodeTypeLoop` のみを対象にする。

### [Issue 2] `ErrorHandlerChecker` の2ホップ先追跡

Tool ノード直後が Condition (分岐) で、その先に失敗時ブランチがあれば「エラーハンドリングあり」と判定する。

### [Issue 3] 信頼性フラグ（v0.3 信頼度スコアと連動）

`Config["reliable"] == true` のツール（純粋関数、決定的アルゴリズム）は error_handler_checker の対象外とする。

## 結論

**Shingan自身のパイプラインを入力して5件のFindingが出たが、うち5件すべてが誤検知**。
誤検知は既知のモデル抽象の粗さによるもので、v0.2で NodeType 拡張 + 2ホップ追跡 で解決可能。

この作業自体が「ルールの網羅性と同時に誤検知率管理の重要性」を示す一次証拠になっている。
面接でも「OSSとして磨き込む上で、誤検知率を定量化して継続的に下げるプロセスが必要」と語れる。
