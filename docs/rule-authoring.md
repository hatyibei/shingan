# Rule Authoring Guide (internal)

本ガイドは Shingan の **builtin rule** を実装する内部 contributor 向けの手引きです。
v0.x の Phase 2 で追加予定の 10 ルール (Local 6 / Path 4) を書くときの一次参照ドキュメントとして利用します。

> **対象読者**: Shingan リポジトリで `domain/rules/*.go` を編集する人。
> **対象外**: 外部から plugin として動的に rule を差し込みたい人。 ADR-010 で **Plugin SDK は v1.0 まで internal-only** と決定したため、外部公開向けのドキュメントは v1.0 で `docs/plugins/getting-started.md` として別途整備します。本ガイドはその先行版です。

参照 ADR:
- [ADR-006](../shingan-adr.md) — ESLint 方式 visitor + selector + listener 採用 (1walk dispatcher)
- [ADR-007](../shingan-adr.md) — Local / Path / Global の 3 層ルール分離
- [ADR-008](../shingan-adr.md) — Confidence × ConfidenceReason 二次元品質管理
- [ADR-010](../shingan-adr.md) — Plugin SDK internal-first 戦略

---

## 1. Tier フローチャート ― どの層で書くか

最初に決めるのは **どの tier (Local / Path / Global)** で書くかです。判断基準:

```
新しいルールを書きたい
  │
  └─ そのルールが見たいのは何か?
       │
       ├─ 1 node の Config / Type メタデータだけ        ────► Local rule
       │     (例: deprecated_model, loop_guard)              (OnNode / OnAny)
       │
       ├─ 1 edge のメタデータ (Condition / From / To)   ────► Local rule
       │     (例: 動的 conditional edge の検査)               (OnEdge)
       │
       ├─ 1 node + その出力 edges の集合                ────► Local rule
       │     (例: redundant_llm_call の per-node bucket)      (OnNode + OnGraph)
       │
       ├─ source ノード → sink ノードの経路             ────► Path rule
       │     (例: pii_leak_scanner の RAG → API)             (Sources/Sinks/Propagate)
       │
       ├─ ループ内 subgraph / 直接近傍 (1-2 hop)        ────► Path rule
       │     (例: error_handler_checker の Tool→Cond)        (Sources/Sinks/Propagate)
       │
       └─ graph 全域走査が必要 (cycle / reachability)   ────► Global rule
             (例: cycle_detection, unreachable_node)         (AnalyzeGlobal)
```

**迷ったら Local から始める** のが原則です。Listener 内で 1 node 完結しないと判明したら Path / Global へ昇格します。逆方向 (Global → Local) への降格は ADR-007 でも触れているとおり計算量を悪化させるので避けてください。

### v0.5 builtin 10 ルールの分類

| Tier | ルール | 主たる handler | 計算量 |
|---|---|---|---|
| **Local (4)** | [`deprecated_model`](../domain/rules/deprecated_model.go) | `OnNode[NodeTypeLLM]` | O(V) |
| | [`loop_guard`](../domain/rules/loopguard.go) | `OnNode[NodeTypeLoop+Control]` | O(V) |
| | [`redundant_llm_call`](../domain/rules/redundant.go) | `OnNode[NodeTypeLLM]` + `OnGraph` | O(V) |
| | [`secret_exposure_scanner`](../domain/rules/secret_exposure.go) | `OnAny` (recursive Config scan) | O(V × cfg) |
| **Path (3)** | [`pii_leak_scanner`](../domain/rules/pii_leak.go) | reverse-BFS from sinks | O(sinks × (V+E)) |
| | [`error_handler_checker`](../domain/rules/errorhandler.go) | per-node 1-2 hop lookahead | O(V + E) |
| | [`cost_estimation`](../domain/rules/cost.go) | LLM × loop subgraph DFS | O(V + E) |
| **Global (3)** | [`cycle_detection`](../domain/rules/cycle.go) | DFS back-edge | O(V + E) |
| | [`unreachable_node`](../domain/rules/reachability.go) | BFS from entry | O(V + E) |
| | [`max_parallel_branches`](../domain/rules/max_parallel_branches.go) | per-node fan-out count | O(V + E) |

3-pass パイプラインは [`application/orchestrator.go`](../application/orchestrator.go) で組み立てられ、Pass 1 = Global → Pass 2 = Local (1walk dispatcher = [`application/walker.go`](../application/walker.go)) → Pass 3 = Path (reverse adjacency 共有 = [`application/path_walker.go`](../application/path_walker.go)) の順に走ります。

---

## 2. Local rule テンプレート

`LocalRule` interface を満たす struct を定義し、同時に legacy `AnalysisRule` の `Name()` / `Analyze()` も実装します (3-pass パイプラインに乗らない caller — テスト double / 旧 Orchestrator 経路 — のため)。

```go
// domain/rules/my_local_rule.go
package rules

import (
	"fmt"

	"github.com/hatyibei/shingan/domain"
)

// MyLocalRule は <検出内容> を 1 node 単位で検出する。
//
// Tier: Local (ADR-007) — decision per node, fits the 1-walk dispatcher.
// ConfidenceReason: ReasonExactStaticMatch (curated table lookup).
type MyLocalRule struct{}

func NewMyLocalRule() *MyLocalRule { return &MyLocalRule{} }

func (r *MyLocalRule) Name() string { return "my_local_rule" }

func (r *MyLocalRule) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     r.Name(),
		Severity: domain.Warning, // default; per-finding override OK
		Fixable:  false,
	}
}

// Listener implements domain.LocalRule. 1walk dispatcher will fire only on LLM nodes.
func (r *MyLocalRule) Listener(ctx *domain.RuleContext) domain.Listener {
	return domain.Listener{
		OnNode: map[domain.NodeType]domain.NodeHandler{
			domain.NodeTypeLLM: func(c *domain.RuleContext, n *domain.Node) {
				if f, ok := evaluateMyRule(n); ok {
					c.Report(f)
				}
			},
		},
	}
}

// Analyze keeps the legacy AnalysisRule contract alive (3-pass orchestrator
// type-asserts; the legacy fan-out path also receives this).
func (r *MyLocalRule) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	var findings []domain.Finding
	for _, node := range graph.Nodes {
		if node.Type != domain.NodeTypeLLM {
			continue
		}
		if f, ok := evaluateMyRule(node); ok {
			findings = append(findings, f)
		}
	}
	return findings
}

// evaluateMyRule は Listener と Analyze の両方から呼ばれる純粋関数。
// 1 node の入力に対して Finding を返す責務だけを持つ (テストしやすい)。
func evaluateMyRule(n *domain.Node) (domain.Finding, bool) {
	// ... detection logic ...
	return domain.Finding{
		RuleName:         "my_local_rule",
		Severity:         domain.Warning,
		NodeID:           n.ID,
		Message:          fmt.Sprintf("node %q triggers my_local_rule", n.ID),
		Suggestion:       "...",
		Confidence:       0.9,
		ConfidenceReason: domain.ReasonExactStaticMatch, // ADR-008 で必須
	}, true
}

func init() {
	registerBuiltin(NewMyLocalRule()) // ADR-010: internal registry
}
```

**ポイント**

- `Listener` と `Analyze` は **同じ純粋関数 (`evaluateMyRule`) を共有** すると保守が楽 (`deprecated_model.go` / `loopguard.go` 参照)。
- Listener が複数 NodeType に反応するなら handler を変数に出して `OnNode` map に複数キーで登録 (`loopguard.go` の `NodeTypeLoop` + `NodeTypeControl` パターン)。
- 全 NodeType に反応したいなら `OnAny` を使う (`secret_exposure_scanner.go` 参照)。
- per-node では決まらず graph 全体の集計が要る場合は `OnNode` で bucket を貯めて `OnGraph` で emit する (`redundant.go` の `redundant_llm_call` パターン)。
- Edge メタデータだけが対象なら `OnEdge` のみを定義 (現状の builtin にはこのパターンの実装はないが、`Listener.OnEdge` フィールドは公開済み)。

---

## 3. Path rule テンプレート

`PathRule` interface は `Meta()` / `Sources()` / `Sinks()` / `Propagate()` の 4 メソッドで構成されます。`PathContext` には事前計算済みの **reverse adjacency** が入っているので、reverse-BFS / reverse-DFS は `ctx.Reverse[nodeID]` で走らせます。

```go
// domain/rules/my_path_rule.go
package rules

import (
	"fmt"

	"github.com/hatyibei/shingan/domain"
)

// MyPathRule は <source> から <sink> への到達経路で <条件> を検出する。
//
// Tier: Path (ADR-007) — needs adjacency information beyond a single node.
// ConfidenceReason: ReasonHeuristicPattern (taint propagation 的な近似)。
type MyPathRule struct{}

func NewMyPathRule() *MyPathRule { return &MyPathRule{} }

func (r *MyPathRule) Name() string { return "my_path_rule" }

func (r *MyPathRule) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     r.Name(),
		Severity: domain.Warning,
		Fixable:  false,
	}
}

// Sources は経路解析の起点ノード集合を返す。
func (r *MyPathRule) Sources(g *domain.WorkflowGraph) []*domain.Node {
	if g == nil {
		return nil
	}
	var out []*domain.Node
	for _, n := range g.Nodes {
		if n.Type == domain.NodeTypeTool {
			out = append(out, n)
		}
	}
	return out
}

// Sinks は経路解析の終点ノード集合を返す。
// Source 単独のループ・近傍検査だけで完結する場合は nil を返してよい
// (cost.go の CostAnalyzer.Sinks がこのパターン)。
func (r *MyPathRule) Sinks(g *domain.WorkflowGraph) []*domain.Node {
	if g == nil {
		return nil
	}
	var out []*domain.Node
	for _, n := range g.Nodes {
		if n.Type == domain.NodeTypeOutput {
			out = append(out, n)
		}
	}
	return out
}

// Propagate は ctx.Sources / ctx.Sinks / ctx.Reverse を使って経路解析を行う。
// reverse adjacency は path_walker.go が 1 度だけ構築して全 PathRule で共有する。
func (r *MyPathRule) Propagate(ctx *domain.PathContext) []domain.Finding {
	if ctx == nil || ctx.Graph == nil || len(ctx.Sinks) == 0 {
		return nil
	}
	var findings []domain.Finding
	for _, sink := range ctx.Sinks {
		// 例: 各 sink から reverse-BFS。
		visited := map[string]bool{sink.ID: true}
		queue := []string{sink.ID}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, e := range ctx.Reverse[cur] {
				if visited[e.From] {
					continue
				}
				visited[e.From] = true
				// ... source に行き当たったら Finding を emit ...
				queue = append(queue, e.From)
			}
		}
		_ = findings // for illustration only; populate as needed
	}
	return findings
}

// Analyze は legacy AnalysisRule 経路。reverse adjacency を自分で構築する点が
// PathContext 経由のホットパスとの唯一の違い。
func (r *MyPathRule) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	reverse := make(map[string][]domain.Edge, len(graph.Nodes))
	for _, e := range graph.Edges {
		reverse[e.To] = append(reverse[e.To], e)
	}
	ctx := &domain.PathContext{
		Graph:    graph,
		RuleName: r.Name(),
		Sources:  r.Sources(graph),
		Sinks:    r.Sinks(graph),
		Reverse:  reverse,
	}
	return r.Propagate(ctx)
}

func init() {
	registerBuiltin(NewMyPathRule())
}

// 上の Propagate 内で `fmt.Sprintf("source %q reaches sink %q", ...)` のような
// メッセージを組み立てて Finding を emit するのが典型形 (pii_leak.go 参照)。
```

**ポイント**

- `pii_leak_scanner` は **reverse-BFS** で sink から source に遡る (Human ノードで止まる) パターン。 [`pii_leak.go`](../domain/rules/pii_leak.go) の `runPIIReverseBFS` が骨格。
- `error_handler_checker` は **graph.OutgoingEdges(nodeID)** を使った forward 1-2 hop 探索。Path tier に置いた理由は ADR-007 の根拠どおり「Local handler は単一 node API のため隣接 edge にアクセスできない」。
- `cost_estimation` は Sinks を使わず、Sources の各 LLM が **loop subgraph に属するか** を Propagate 内で判定する。Path tier の柔軟さを示す例。

---

## 4. Global rule テンプレート

Global rule は graph 全体を 1 pass で見るルール。`AnalyzeGlobal()` は legacy `Analyze()` のエイリアスで構わない (`cycle.go` / `reachability.go` がそうしている)。

```go
// domain/rules/my_global_rule.go
package rules

import (
	"fmt"

	"github.com/hatyibei/shingan/domain"
)

// MyGlobalRule は graph 全体を 1 pass で走査して <条件> を検出する。
//
// Tier: Global (ADR-007) — DFS/BFS over the entire graph.
// ConfidenceReason: ReasonExactStaticMatch (deterministic graph property).
type MyGlobalRule struct{}

func NewMyGlobalRule() *MyGlobalRule { return &MyGlobalRule{} }

func (r *MyGlobalRule) Name() string { return "my_global_rule" }

func (r *MyGlobalRule) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     r.Name(),
		Severity: domain.Critical,
		Fixable:  false,
	}
}

// AnalyzeGlobal implements domain.GlobalRule.
// 通常は Analyze に委譲する (cycle.go / reachability.go 参照)。
func (r *MyGlobalRule) AnalyzeGlobal(graph *domain.WorkflowGraph) []domain.Finding {
	return r.Analyze(graph)
}

// Analyze は legacy AnalysisRule 経路でも使われる本体。
func (r *MyGlobalRule) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	var findings []domain.Finding
	for id := range graph.Nodes {
		// 例: fan-out が閾値超のノードを検出。
		if len(graph.OutgoingEdges(id)) >= 100 {
			findings = append(findings, domain.Finding{
				RuleName:         r.Name(),
				Severity:         domain.Critical,
				NodeID:           id,
				Message:          fmt.Sprintf("node %q has excessive fan-out", id),
				Suggestion:       "chunk the sub-agents",
				Confidence:       1.0,
				ConfidenceReason: domain.ReasonExactStaticMatch,
			})
		}
	}
	return findings
}

func init() {
	registerBuiltin(NewMyGlobalRule())
}
```

**アルゴリズム選択ガイド**

| アルゴリズム | 例 | コード |
|---|---|---|
| DFS back-edge | `cycle_detection` | `cycle.go:dfs()` の visitState (unvisited → inProgress → completed) |
| BFS from entry | `unreachable_node` | `reachability.go:Analyze()` の queue |
| per-node aggregation | `max_parallel_branches` | `max_parallel_branches.go:Analyze()` の `len(graph.OutgoingEdges(id))` |

`global_walker.go` は各 GlobalRule を **goroutine 並列** で走らせるので、グローバル可変状態を持たないことが必須です。

---

## 5. ConfidenceReason 選択ガイド (ADR-008)

各 Finding は ADR-008 で **必ず ConfidenceReason を埋める** ことが要求されます。 [`domain/finding.go`](../domain/finding.go) に enum 定義あり。

| Reason | 推奨 Confidence | 使う場面 | 例 |
|---|---|---|---|
| `ReasonExactStaticMatch` | 1.0 (Critical) / 0.9 (Warning) | DFS back-edge / 完全一致 / 設定値の存在チェック | `cycle_detection`, `deprecated_model` (table lookup), `loop_guard` (config 欠落) |
| `ReasonOverApproximatedDynamic` | 0.5 | 動的グラフの保守的近似 | LangGraph `conditional_edges` returning untyped str (parser 側で付与、ルール側からは現状未使用) |
| `ReasonParserFallback` | 0.4 | parser が情報落ち | `go/types` 解析失敗で struct field の型が取れなかった ADK-Go ノード |
| `ReasonExperimentalRule` | 0.6 | 新規導入直後で実コーパス検証が浅いルール | Phase 2 追加直後の `prompt_injection_sink` 等 |
| `ReasonHeuristicPattern` | 0.3-0.7 | 名前ヒント / カテゴリヒント / 構造ヒント | `pii_leak_scanner` (RAG=0.6 / 名前 hint=0.3), `error_handler_checker` (0.8), `secret_exposure_scanner` の Info パターン (0.5) |

### なぜこの値か (根拠)

- **1.0** は「DFS で back-edge を踏んだ / 表に載っている」のような **論理的に確定** している場合のみ。`cycle_detection` の Confidence が 1.0 なのはこの典型例。
- **0.9** は確実だが副次的判定が混じる場合 (`deprecated_model` の Warning 枝はモデル名一致は確実だが「shutdown 時期の予測」が含まれる)。
- **0.7-0.8** は **強いヒューリスティック** 。`error_handler_checker` 0.8 は「conditional outgoing edge があれば error handling 」という近似。
- **0.5-0.6** は **弱いヒューリスティック** または保守的近似。`pii_leak_scanner` の RAG 検出 0.6 は category=="rag" だけで PII 確定とは言えない。
- **0.3** は **名前ヒント** のような最弱シグナル。`pii_leak_scanner` の名前 hint 経路 (`piiHintKeywords` に部分一致) はここ。

`--min-confidence` CLI フラグ (v0.4 以降) は **数値で**フィルタしますが、Reason を見るとなぜ落ちたかをユーザーが解釈できます (LSP hover / SARIF properties / Markdown reporter で表示)。

---

## 6. Severity ガイドライン

`domain.Severity` は `Critical` (2) > `Warning` (1) > `Info` (0) の 3 値。

| Severity | 判断軸 | 例 |
|---|---|---|
| **Critical** | 実行時に**確実に**失敗 / 不可逆な副作用 / 静的な定義エラー | `loop_guard` (max_iterations 未設定 = 無限ループリスク), `deprecated_model` の shutdown 枝 (API 404), `cycle_detection` の非Loopサイクル (graph 定義エラー), `secret_exposure_scanner` の AWS access key |
| **Warning** | 実行時に失敗する**可能性** / 顕著な品質問題 | `error_handler_checker` (失敗時フローなし), `cost_estimation` のループ内高額モデル, `redundant_llm_call`, `pii_leak_scanner` の RAG 経路 |
| **Info** | 改善推奨 / 慣習違反 / 弱いヒント | `cost_estimation` の単純タスク高額モデル, `pii_leak_scanner` の名前 hint 経路, `secret_exposure_scanner` の jwt / generic_secret |

判断軸は **「ユーザーがそのまま merge した時にどれくらい困るか」** 。Critical は CI を落とすべき (`shingan analyze` の exit code 2)、Warning は exit code 1、Info は exit code 0。

---

## 7. テストパターン (TDD)

`domain/rules/*_test.go` は **package `rules_test` (黒箱テスト) + [`testutil.Builder`](../domain/testutil/builder.go) を使う** 規約です。1 ルール最低 5 ケース:

1. **Name() 整合** — 文字列識別子が期待どおり
2. **Positive** — 検出されるべきケースで Finding が出る (Severity / Confidence / RuleName を assert)
3. **Negative** — 検出されないケースで `len(findings) == 0`
4. **Edge case** — 空グラフ / nil / EntryNodeID 不在 / Config nil 等
5. **ConfidenceReason stamp** — Finding.ConfidenceReason が `ReasonXxx` であることを assert

### Local rule の test (`deprecated_model_test.go` パターン)

```go
package rules_test

import (
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/domain/testutil"
)

func TestMyLocalRule_Name(t *testing.T) {
	if got := rules.NewMyLocalRule().Name(); got != "my_local_rule" {
		t.Errorf("Name() = %q, want %q", got, "my_local_rule")
	}
}

func TestMyLocalRule_DetectsCondition(t *testing.T) {
	g, err := testutil.NewBuilder().
		AddNodeWithConfig("llm", domain.NodeTypeLLM, map[string]any{"model": "deprecated-x"}).
		Entry("llm").
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	findings := rules.NewMyLocalRule().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].ConfidenceReason != domain.ReasonExactStaticMatch {
		t.Errorf("Reason = %q, want %q",
			findings[0].ConfidenceReason, domain.ReasonExactStaticMatch)
	}
}
```

### Path rule の test (`pii_leak_test.go` パターン)

Sources / Sinks / Propagate を**それぞれ独立したテスト**で検証 + end-to-end でも検証する 2 段構成 :

```go
func TestMyPathRule_Sources_OnlyToolNodes(t *testing.T) {
	g, _ := testutil.NewBuilder().
		AddNode("tool", domain.NodeTypeTool).
		AddNode("llm", domain.NodeTypeLLM).
		Entry("tool").Build()

	srcs := (&rules.MyPathRule{}).Sources(g)
	if len(srcs) != 1 || srcs[0].ID != "tool" {
		t.Errorf("Sources = %+v, want exactly tool", srcs)
	}
}
```

end-to-end は `Analyze` (legacy 経路) を呼ぶか、`PathContext` を手作業で組んで `Propagate` を直接呼ぶ。本物の 3-pass パイプラインを回したいなら [`application/orchestrator_test.go`](../application/orchestrator_test.go) のレベルでテストします。

### Global rule の test (`cycle_test.go` パターン)

DFS state 遷移を網羅 (unvisited → inProgress → completed) する観点で:

- 線形グラフ (サイクル無し)
- 自己ループ (`a → a`)
- 多段サイクル (`a → b → c → a`)
- 親 Loop ノードあり / なし
- entry node 不在

cycle_test.go の `TestCycleDetector_*` 系で 15+ ケース実装済み、構造を真似てください。

---

## 8. 既存 10 ルールの設計記録

各ルールの 1-2 行サマリ + Severity 根拠 + 偽陽性源。新ルール書くときの参照カード。

| Rule | Tier | 場所 | 検出概要 | Severity / Confidence | 偽陽性源 |
|---|---|---|---|---|---|
| `deprecated_model` | Local | [deprecated_model.go](../domain/rules/deprecated_model.go) | `Config["model"]` を built-in deprecated 表とマッチ | Critical 1.0 (shutdown) / Warning 0.9 (deprecated) | 表メンテナンス遅れ; v1.0 で community registry へ分離予定 |
| `loop_guard` | Local | [loopguard.go](../domain/rules/loopguard.go) | NodeTypeLoop / Control で `max_iterations` 未設定 | Critical 1.0 | NodeTypeControl は deprecated alias なので将来削除可 |
| `redundant_llm_call` | Local | [redundant.go](../domain/rules/redundant.go) | 同一 (model, prompt_template) を 2 つ以上検出 | Warning 0.9 | テンプレート文字列の **完全一致** が条件、変数注入の差分は見ていない |
| `secret_exposure_scanner` | Local | [secret_exposure.go](../domain/rules/secret_exposure.go) | Config を再帰スキャン、9 種の secret regex | Critical/Warning 0.95 / Info 0.5 | placeholder (`${VAR}`) は `placeholderPattern` で除外するが mixed (`sk-xxx${SUFFIX}`) は `hasActualSecret` で再判定 |
| `pii_leak_scanner` | Path | [pii_leak.go](../domain/rules/pii_leak.go) | RAG/has_pii Tool → external Tool への reverse-BFS、Human で止まる | Warning 0.6 (RAG) / Info 0.3 (name hint) | name hint (`pii`/`user`/`personal`/`private` 部分一致) は本質的にヒューリスティック |
| `error_handler_checker` | Path | [errorhandler.go](../domain/rules/errorhandler.go) | Tool ノード後に conditional edge が無い / LLM→Tool 経路で Tool 側に handler 無し | Critical (browser) / Warning (api/mcp) / Info (code), 0.8 | Workflow author が retry を別ノードで実装している場合は false positive 可能性あり |
| `cost_estimation` | Path | [cost.go](../domain/rules/cost.go) | high-cost model を loop 内 / simple task で使用 | Warning (loop) / Info (simple), 0.7 | model 価格表は Phase 2 で外出し検討 (v0.x は static map) |
| `cycle_detection` | Global | [cycle.go](../domain/rules/cycle.go) | DFS back-edge、parent Loop の max_iterations と組合せて Severity 判定 | Critical (unbounded) / Warning (>=100) / Info (Loop 内 sub-agent), 1.0 | parent Loop の検出は path 内逆順走査 — 複雑 nest で誤判定の可能性あり |
| `unreachable_node` | Global | [reachability.go](../domain/rules/reachability.go) | EntryNodeID から BFS、未訪問 node を報告 | Warning (LLM/Tool) / Info (others), 1.0 | EntryNodeID 未設定で Critical 1 件、これはユーザー側設定ミス |
| `max_parallel_branches` | Global | [max_parallel_branches.go](../domain/rules/max_parallel_branches.go) | `OutgoingEdges` の長さで fan-out 計測、閾値 10/20/100 | Critical 1.0 (>=100) / Warning 0.9 (>=20) / Info 0.7 (>=10) | `Config["max_concurrency"]` 設定済みノードはスキップ |

---

## 9. ConfidenceReason linter (`scripts/check_confidence_reason.sh`)

ADR-008 は「Finding には ConfidenceReason を必ず付ける」を要求しますが、**Go では struct field を必須化できない** ので静的解析で代替しています。

[`scripts/check_confidence_reason.sh`](../scripts/check_confidence_reason.sh) は `domain/rules/*.go` を awk state machine で走査し、

- `domain.Finding{ ... }` リテラルのうち、フィールド代入 (`Foo: bar`) を含み、かつ `ConfidenceReason:` 行が無いものを検出
- 空 sentinel `domain.Finding{}` (フィールド代入なし) は除外

```bash
$ make check-reason
check_confidence_reason: OK (10 files scanned)

$ make lint            # check-reason + go vet
```

CI (`.github/workflows/ci.yml`) の `lint` ジョブから `make lint` を呼び出しています。違反すると以下のように offending site が出ます:

```
domain/rules/foo.go:42: domain.Finding literal missing ConfidenceReason
    domain.Finding{
        RuleName: "foo",
        Severity: domain.Warning,
        ...
    }
check_confidence_reason: 1 file(s) contain offending Finding literals
```

**書き始める時** に `make lint` をローカルで走らせる癖をつけてください。

---

## 10. 命名規約

| 対象 | 規約 | 例 |
|---|---|---|
| Rule ID (ファイル名・`Name()`) | snake_case、動詞含めず内容を名詞で | `deprecated_model`, `pii_leak_scanner`, `prompt_injection_sink` |
| ファイル名 | `domain/rules/<rule_id>.go` (一部 historical alias あり) | `deprecated_model.go`, `pii_leak.go` |
| 公開コンストラクタ | `New<PascalCase>()` | `NewDeprecatedModelChecker()` |
| 内部評価関数 | `evaluate<RuleName>()` (per-node 純粋関数) | `evaluateDeprecatedModel`, `evaluateLoopGuard` |

歴史的経緯で一部のファイル名が rule ID と一致していません (`loopguard.go` ↔ `loop_guard`、`redundant.go` ↔ `redundant_llm_call`、`pii_leak.go` ↔ `pii_leak_scanner`)。**新規ルールは厳密に揃えてください**。`v1.0 で全 rename` がトレードオフ表に入っています (ADR-010)。

動詞は含めません: `find_pii_leak` ではなく `pii_leak_scanner`、 `check_max_concurrency` ではなく `max_parallel_branches` 。

---

## 11. registerBuiltin による自動登録

ADR-010 で `domain/rules/registry.go` の `registerBuiltin()` は **小文字で unexported** にしています (外部から呼ばれない)。各ルールファイルの `init()` で自分自身を登録します:

```go
func init() {
	registerBuiltin(NewMyRule())
}
```

これにより:

1. `domain/rules/registry.go:AllBuiltins()` (大文字、exported) が全 builtin を返す
2. [`infrastructure/factory/analyzer.go`](../infrastructure/factory/analyzer.go) の `AnalyzerFactory.CreateAll()` が `rules.AllBuiltins()` を素通しで返す
3. 新規ルール追加時に **factory 側の改修は不要** — `init()` の register だけで全 caller (CLI / MCP / web / HTTP API / orchestrator 3-pass) に伝搬する

> **CONTRIBUTING.md に古い手順** が残っているかもしれません: 「factory に追加」ステップは v0.5 の registry 導入 (ADR-010) で不要になりました。ガイドの現行手順は本ドキュメントを優先してください。

---

## 12. v1.0 後の plugin 移行パス

ADR-010 の方針どおり、v1.0 リリース時に以下を実施します:

1. `registerBuiltin()` を `Register()` に rename + 大文字化 → 外部 plugin から呼べるようにする
2. `domain/visitor.go` / `domain/rule.go` / `domain/finding.go` の API 表面を **v1.0 で固定** (semver 保証)
3. `docs/plugins/getting-started.md` を新設 (本ガイドが下敷き、外部公開向けに編集)
4. sample plugin repo (`shingan-plugin-template`) を別 repo として公開

それまでの v0.x 期間中は **fork → builtin として upstream PR** が外部 contributor の唯一の経路です (CONTRIBUTING.md 参照)。

---

## 13. Phase 2 で書く 10 ルールへの当てはめ

ADR-007 の Phase 2 増強計画 (合計 20 ルール) で追加予定:

- **Local 追加 (6)**: `unbounded_tool_arg` / `model_card_mismatch` / `dynamic_node_construction` / `missing_eval_dataset` / `secret_in_prompt_template` / `temperature_misuse`
- **Path 追加 (4)**: `prompt_injection_sink` / `retry_storm` / `eval_missing` / `circular_dep_agents`

各ルール書き始めるときの推奨フロー:

1. **Tier 決定**: 本ガイド Section 1 のフローチャート
2. **既存近接ルールをコピー**: 
   - Local 単純 → `deprecated_model.go`
   - Local + aggregation → `redundant.go`
   - Local 全 NodeType + Config 再帰 → `secret_exposure.go`
   - Path reverse-BFS → `pii_leak.go`
   - Path 1-2 hop → `errorhandler.go`
   - Path + loop subgraph → `cost.go`
   - Global DFS → `cycle.go`
   - Global BFS → `reachability.go`
   - Global per-node aggregate → `max_parallel_branches.go`
3. **TDD**: `*_test.go` を先に書く (本ガイド Section 7、最低 5 ケース)
4. **`make lint && go test ./domain/rules/...`** をローカル実行
5. **README.md の解析ルール一覧表を更新**
6. **ConfidenceReason は新規ルール導入直後なら `ReasonExperimentalRule`** (Confidence 0.6) で出すのが ADR-008 の意図と整合

---

## 関連リンク

- [shingan-adr.md](../shingan-adr.md) — 全 ADR
- [docs/architecture.md](./architecture.md) — Onion Architecture 詳細
- [docs/confidence-scoring.md](./confidence-scoring.md) — Confidence/Severity の元設計
- [domain/visitor.go](../domain/visitor.go) — Listener / Selector / RuleContext
- [domain/rule.go](../domain/rule.go) — LocalRule / PathRule / GlobalRule / AnalysisRule
- [domain/finding.go](../domain/finding.go) — Finding + ConfidenceReason
- [domain/rules/registry.go](../domain/rules/registry.go) — internal builtin registry
- [application/walker.go](../application/walker.go) — 1walk dispatcher (Local)
- [application/path_walker.go](../application/path_walker.go) — Path tier
- [application/global_walker.go](../application/global_walker.go) — Global tier
- [application/orchestrator.go](../application/orchestrator.go) — 3-pass pipeline
- [scripts/check_confidence_reason.sh](../scripts/check_confidence_reason.sh) — ADR-008 linter
