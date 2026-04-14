# cycle_detection の "non-Control node" メッセージについて

## 更新 (2026-04-14)

v0.2 実装済み: 以下のオプションをすべて実施した。

- **Option A (文言改善)**: サイクル検出時、DFSパスを遡って親Control（LoopAgent）を探し、見つかればメッセージを `"cycle detected inside Control node \"<parent>\" via sub-agent \"<node>\""` に変更。
- **Option B (Severity分離)**: 親Control内かつmax_iterations未設定 → `Warning`、max_iterations >= 100 → `Info`、親Controlなし → `Critical` (グラフ定義誤り)。
- **Option C (独立ルール LoopGuardChecker)**: `domain/rules/loopguard.go` に `LoopGuardChecker` を新規追加。Controlノードの `max_iterations` 欠落を独立してCritical検出する。AnalyzerFactoryに `"loop_guard"` として登録（計6ルール）。

実行結果例 (`infinite_loop_unbounded.go`):
```
loop_guard   | Critical | unbounded_loop | LoopAgent "unbounded_loop" has no MaxIterations configured — potential infinite loop
cycle_detection | Warning | classifier | cycle detected inside Control node "unbounded_loop" via sub-agent "classifier"
```

## 現状の挙動

`testdata/real/infinite_loop.go` や `examples/runtime/infinite_loop_unbounded.go` のような **LoopAgentにMaxIterations未設定** のサンプルをShinganで解析すると、以下のFindingが出る:

```
cycle_detection Critical
  node: classifier
  message: cycle detected at non-Control node "classifier" (type=llm): graph definition error
```

メッセージは「非Controlノードでサイクル検出」と言っているが、実際にはLoopAgent（= Controlノード）の内部で起きているループ。一見ミスリーディングに見える。

## 技術的背景

### ADK-Go AST パーサーのノード展開

ShinganのADK-Goパーサー (`infrastructure/parser/adkgo.go`) は、`loopagent.New(Config{ AgentConfig: agent.Config{ SubAgents: [A, B] } })` のような構造を以下のように展開する:

- LoopAgent本体 → 1ノード (`NodeTypeControl`, `Config["max_iterations"]`)
- SubAgents `[A, B]` → 各々ノード (`NodeTypeLLM`等)
- LoopAgentからA, Bへのエッジ
- **A → B → A のループバックエッジ**（SubAgentsの末尾から先頭へ）

この時点でサイクルは「A → B → A」で、A/Bは`NodeTypeLLM`なので `非Controlノード上のサイクル` として検出される。

### CycleDetectorのSeverity判定ロジック (`domain/rules/cycle.go`)

```go
if cycleNode.Type != domain.NodeTypeControl {
    // Critical: グラフ定義誤り
}
// Control型ノードの場合は max_iterations を確認
```

ここで判定しているのは **サイクル到達時の先頭ノード** の型。LoopAgent本体ではなくSubAgentsで最初に back-edge のターゲットになるノード。結果として Critical と判定される。

## これは仕様か、バグか?

**仕様として意図的**。以下2つの意図がある:

1. **可視性**: ユーザーはLoopAgentの内部構造（どのサブAgentが繰り返されるか）を理解して修正する必要がある。LoopAgent本体にFindingを出すより、ループに含まれる実ノード（classifier）を指した方が修正ポイントが明確。

2. **LoopAgent外のサイクルとの区別**: もしユーザーが意図せずSequentialAgentの中で circular reference を作った場合も同じルールで検出したい。「Control直下かどうか」より「サイクル内のノードが管理された繰り返しか」を見る方が網羅的。

ただし**メッセージ文言は改善余地あり**。現状の "graph definition error" は、LoopAgent管理下のケースでは誤解を招く。

## 改善方針（v0.2候補）

### Option A: 文言改善のみ

サイクルの起点ノードの親（またはサイクルに至るまでの制御ノード）を遡って、LoopAgentが存在する場合はメッセージを差し替える:

```
before: cycle detected at non-Control node "classifier": graph definition error
after:  cycle detected inside LoopAgent "retry_loop" via "classifier". MaxIterations not set → potentially infinite loop.
```

### Option B: Severity分離

- LoopAgent管理下のサイクル + MaxIterations未設定 → `Critical` (現状維持)
- LoopAgent管理下のサイクル + MaxIterations設定済みでも >= 100 → `Warning`
- LoopAgent管理**外**のサイクル → `Critical` with "graph definition error" (現状の真の対象)

### Option C: 2つのFindingに分割

同じ問題に対して:
1. `cycle_detection` (Critical) — ループが存在する事実
2. `loop_without_max_iterations` (Critical) — MaxIterations未設定という独立ルール

CycleDetector を分割し、新ルール `LoopGuardChecker` を作る。責務が明確になる。

## 面接で問われた時の答え方

**Q: このメッセージ、誤検知ではないですか?**

A: 検出自体は正しいんですが、メッセージ文言はもっと分かりやすくできます。現状「non-Control node」と出るのは、AST上でループを構成しているのがLoopAgentの子ノード (classifier) だから。ユーザーから見ると「LoopAgent内のループに警告が出ている」状態で、直したいのはLoopAgentの設定 (MaxIterations) です。

v0.2でメッセージを親コンテキスト込みに改善予定で、「LoopAgent "retry_loop" の MaxIterations が未設定」と出すようにします。独立ルールとして分離することも検討しています。

**Q: 面接までに直さないんですか?**

A: 直せますが、今回のPoCでは「静的解析のフレームワーク確立」が主目的で、個別ルールの文言最適化は入社後のロードマップに入れています。ただしSeverity判定は現状で正しく（MaxIter未設定→Critical）、面接官が見るべき「そもそもこの問題を検出できているか」は満たせています。

## 関連Issue（実作業が発生する場合）

- [x] v0.2: cycle_detection メッセージに親コンテキスト（LoopAgent名）を含める **実装済み**
- [x] v0.2: `LoopGuardChecker` ルールの新規追加検討 **実装済み**
- [ ] v0.2: ADK-Go parser でLoopAgent → SubAgents の「管理関係」メタデータを Edge に持たせる（例: `Edge.Kind = "loop_managed"`）

これらは `examples/runtime/infinite_loop_unbounded.go` で再現可能なので、Shingan自身のテストスイートで回帰検証できる。
