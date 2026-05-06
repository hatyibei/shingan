> 🌐 Language: [English](./circular-dep-agents.md) | **日本語**

# circular_dep_agents — 設計・実装ドキュメント

> **対象バージョン**: Phase 2 (v0.6 系列、reliability batch)
> **実装ファイル**: `domain/rules/circular_dep_agents.go`
> **テスト**: `domain/rules/circular_dep_agents_test.go` (13 ケース)
> **層 (ADR-007)**: Path rule — Sources / Sinks / Propagate を実装 (Sinks は nil)

---

## 1. 背景・動機

multi-agent ワークフロー (LangGraph、ADK、CrewAI、AutoGen 等) では、 agent A が agent B に処理を委譲し、B が判断結果に応じて A に戻すという **delegation cycle** が起きやすい:

- A が「分析が足りない」と判断して B にリクエストを投げ直す
- B が「決定権がない」として A に投げ返す
- depth/budget guard を入れ忘れていると無限委譲 → token の浪費 / コンテキスト爆発 / latency 死

これを **静的グラフから事前検出** するのが circular_dep_agents。「graph 全体の back-edge」を見る `cycle_detection` (Global) とは異なり、**agent ノード同士に絞った循環** を扱うことで agent ワークフロー特有の文脈で警告を出す。

> **cycle_detection との関係 (意図的な overlap)**: cycle_detection は graph 全体の back-edge を Critical で報告し、circular_dep_agents は agent-delegation pattern を Warning で補足する。Severity (Critical vs Warning) と Suggestion (graph 整理 vs orchestrator pattern) で 2 つのルールが補完的に機能する設計。

---

## 2. 検出対象

### 2.1 Source (agent ノード)

`NodeType.LLM` のうち、以下のいずれか 2 シグナルにマッチするノードを agent と扱う:

| シグナル | 例 |
|---|---|
| `Config["agent_role"]` が non-empty string | `"agent_role": "planner"` (LangGraph / ADK 慣習) |
| `Config["sub_agents"]` が non-nil | `"sub_agents": ["worker_a", "worker_b"]` (orchestrator declarations) |

Tool ノード (transfer_to_agent ツール等) は agent ではなく **router** として扱う:
- 中間ノードに transfer ツールがあっても、agent → tool → agent の経路は cycle に巻き込む
- ただし agent 数のカウントは tool を**除く**ので「2 agents in cycle」=「distinct agent ID が 2 つ」

### 2.2 Cycle 判定アルゴリズム

各 source (agent) を起点に、 outgoing edges を DFS で前向きに辿り、 source 自身に戻る経路を探す。 cycle 上の **distinct agent IDs を集計**して Severity を決定:

```
distinct agents on cycle = K
K == 1 (self-edge or tool-mediated revisit)
    → if direct self-edge (A → A) → Info (self-reference)
    → if via tools only         → 検出しない (cycle_detection の領域)
K == 2                          → Warning (2-agent delegation cycle)
K >= 3                          → Warning (multi-agent delegation cycle)
```

### 2.3 Severity / Confidence マトリクス

| 状況 | Severity | Confidence | ConfidenceReason | Rationale |
|---|---|---|---|---|
| 2-agent cycle (A → B → A) | **Warning** | 0.85 | `exact_static_match` | agent 認識後は cycle 検出が確定的、最も典型的なパターン |
| 3+ agent cycle (A → B → C → A) | **Warning** | 0.75 | `exact_static_match` | 3 段以上は「意図的な hand-off pattern」の可能性が増えるので 0.75 に |
| Single agent self-reference (A → A) | **Info** | 0.6 | `heuristic_pattern` | self-recursion (re-planning 等) は意図的な場合がある |
| Single agent in cycle via tools only | (発火しない) | — | — | これは cycle_detection の領域 |

> **ConfidenceReason の選択根拠 (ADR-008)**:
> - 2-agent / 3+ agent は agent 認識さえ通ればグラフ上の cycle 検出は確定的なので `exact_static_match`
> - Self-reference は意図的な再帰の可能性が高い (planner re-plan / iterative refinement) ため `heuristic_pattern` で控えめに
> - **agent 認識自体は heuristic** (Config キー名のマッチ) だが、Severity を Warning に抑えていることでこの不確実性を吸収済み — Reason には反映しない

---

## 3. 検出アルゴリズム

```
Step 1: Sources(g) — NodeType.LLM で agent_role / sub_agents を持つノード集合 (O(V))
Step 2: Sinks(g) は nil。各 source 起点で前向き DFS を走らせる
Step 3: 各 source about:
        a. 直接 self-edge (src → src) があれば Self-reference Info を 1 件 emit
        b. DFS で next.To == src となる経路を探す
        c. 見つかったら path 上の distinct agent IDs を抽出 (sort + dedup)
        d. agent 数 >= 2 のときだけ Severity 決定 + emit (1 のときは tool-mediated cycle なので skip)
        e. Dedup key は (sorted agent IDs) または "SELF::<id>"
Step 4: 全 source 走査終了で findings を返す
```

**計算量**: O(agents × (V+E))。typical な multi-agent workflow では agents << V のため実質 O(V+E)。

**Dedup**: `(sorted agent set)` をキーに発火を 1 回に絞る。 2-agent cycle (A,B) は A 起点 / B 起点で同じ cycle を 2 回検出するが、findings は 1 件にまとめる。

---

## 4. 実装の設計判断

### 4.1 Path tier に置いた理由

- Local rule では graph 隣接情報が見えない
- Global rule で全 graph DFS をすると **agent 認識 + cycle 検出** を毎回やり直す必要がある (cycle_detection と DFS を共有しても agent filter は path-specific)
- Path tier で各 agent を起点に DFS することで、 agent-induced subgraph の cycle を必要最低限の操作で見つけられる

`Sinks` は使わないが、 `PathRule` interface は Sinks を nil 許容するので問題なし (CostAnalyzer / RetryStorm と同型)。

### 4.2 cycle_detection との overlap を許容する理由

両ルールが同じ cycle を別 Severity で報告するのは **意図的**:

- cycle_detection は「graph 構造として cycle が無限ループになり得るか」を論理的に判定 (loop_guard + max_iterations + parent Loop の組み合わせ) して Critical を出す
- circular_dep_agents は「**agent 同士の transfer**」という run-time 文脈でのみ意味がある cycle を Warning で報告
- Severity と Suggestion (graph 整理 vs orchestrator pattern) でユーザは 2 つのアドバイスを切り分けられる

ADR-007 の3層分離原則とも整合的: cycle_detection は Global (全域 DFS)、circular_dep_agents は Path (agent-induced subgraph) で計算量プロファイルが異なる。

### 4.3 「single agent + tools cycle」を発火させない理由

`agent_a → tool_a → tool_b → agent_a` のような cycle は **agent 同士の delegation** ではない (tool が再帰しているだけ)。これを circular_dep_agents で警告すると false positive を量産するため、distinct agent count >= 2 を必須条件にしている。 単独 agent の self-edge `agent_a → agent_a` だけは Info で報告する (再帰意図が見えるので)。

### 4.4 self-reference を Info にした理由

planner が「自分を呼び直して計画を refine する」のは LangGraph の `tool_node` ループや AutoGen の reflection pattern で頻出する正当なパターン。ここを Critical / Warning で出すと false positive が大量発生するため、Info + heuristic_pattern で「気にしないなら無視できる」ノイズに留めている。 `--min-confidence 0.7` で完全に suppress できる。

---

## 5. 推奨される対策パターン

### 5.1 Orchestrator パターン (推奨)

```python
# OK: orchestrator が transfer 決定権を独占
class OrchestratorAgent:
    sub_agents = [PlannerAgent(), WorkerAgent()]

    def step(self, state):
        if state.needs_planning:
            return self.sub_agents[0].plan(state)  # planner は orchestrator にだけ返す
        return self.sub_agents[1].execute(state)

# planner と worker は互いに transfer できない (orchestrator 経由のみ)
```

### 5.2 max_handoffs Budget

```python
# OK: handoff 回数に上限を設定
class PlannerAgent:
    max_handoffs = 5  # 5 回 hand-off したら強制終了

    def transfer(self, target):
        if self.handoff_count >= self.max_handoffs:
            raise StopIteration("max_handoffs exceeded")
        ...
```

### 5.3 Depth Budget

```python
# OK: 階層深さで打ち切り
def execute(state, depth=0):
    if depth >= MAX_DEPTH:
        return state
    next_agent = pick(state)
    return next_agent.execute(state, depth + 1)
```

### 5.4 ライブラリ別サポート例

| ライブラリ | 推奨 API |
|---|---|
| LangGraph | `compile(checkpointer=..., interrupt_before=[...])` で人為的な break point |
| OpenAI Swarm | `max_turns` パラメータで turn count を制限 |
| AutoGen | `Agent.max_consecutive_auto_reply` |
| CrewAI | `Crew(max_iter=N)` |
| ADK-Go | `AgentConfig.MaxSteps` (実装による) |

---

## 6. 既知の False Positive / False Negative

### False Positive

- **意図的な orchestrator-sub_agent 双方向**: `orchestrator → planner → orchestrator` は本ルールで cycle 扱いになるが、 orchestrator パターンとして正当。 Severity を Warning に抑えており、 ユーザは `--min-confidence 0.9` で抑制可能。
- **conditional edge で実は循環しない構造**: `if step == 1 → planner、 else → output` のような分岐は静的にはエッジが両方存在し cycle に見えるが、ランタイムでは出現しないケース。 Confidence 0.85 はこのレベルの不確実性を許容している。

### False Negative

- **agent 識別子が独自キー**: `Config["role"]` / `Config["agent_id"]` 等で agent を declares するフレームワークでは検出できない。 Phase 2 後半で agent identification の DSL 拡張を検討。
- **動的に生成される agents**: `LangGraph.add_node()` を runtime で追加するパターンは graph parse 時に存在せず検出不可。 LangGraph parser (Phase 1 P) を拡張する path で対応予定。
- **transfer が tool でなく直接 LLM call として書かれているケース**: 双方の LLM ノードが Config で agent_role を明示していないと source 集合に含まれず検出できない。

---

## 7. 関連ルール / ADR

- **ADR-006**: ESLint visitor pattern — Local rule との対比。本ルールは Path tier。
- **ADR-007**: Local / Path / Global の3層分離 — 本ルールが Path tier に属する根拠 + cycle_detection との計算量プロファイルの差。
- **ADR-008**: ConfidenceReason — exact_static_match (multi-agent cycle) と heuristic_pattern (self-ref) を使い分け。
- `cycle_detection` (Global) — graph 全体の back-edge を Critical で報告する補完ルール。 **意図的に overlap** して両方発火する設計。
- `loop_guard` (Local) — Loop ノード単独の max_iterations 欠落検出。 agent cycle ではないが似た「無限ループ」の系譜。
- `prompt_injection_sink` (Path) — agent 間 transfer の途中で攻撃面が生まれるケースとセットで運用すると価値が上がる。
