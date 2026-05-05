> 🌐 Language: **English** | [日本語](./rule-authoring.ja.md)

# Rule Authoring Guide (internal)

This guide is for internal contributors who implement Shingan **builtin rules**.
It serves as the primary reference when writing the 10 rules (6 Local / 4 Path) planned for Phase 2 of v0.x.

> **Audience**: Anyone editing `domain/rules/*.go` in the Shingan repository.
> **Out of scope**: Anyone who wants to plug in rules dynamically as external plugins. Per ADR-010, the **Plugin SDK stays internal-only until v1.0**, so the externally facing documentation will land separately at v1.0 as `docs/plugins/getting-started.md`. This guide is the precursor to that document.

Referenced ADRs:
- [ADR-006](../shingan-adr.md) — ESLint-style visitor + selector + listener (1walk dispatcher)
- [ADR-007](../shingan-adr.md) — Three-tier rule split: Local / Path / Global
- [ADR-008](../shingan-adr.md) — Two-dimensional quality control with Confidence × ConfidenceReason
- [ADR-010](../shingan-adr.md) — Internal-first strategy for the Plugin SDK

---

## 1. Tier Flowchart — Which Tier to Use

The first decision is **which tier (Local / Path / Global)** the rule belongs to. Decision criteria:

```
I want to write a new rule
  │
  └─ What does it need to look at?
       │
       ├─ Just Config / Type metadata of a single node    ────► Local rule
       │     (e.g. deprecated_model, loop_guard)               (OnNode / OnAny)
       │
       ├─ Metadata of a single edge (Condition / From / To) ──► Local rule
       │     (e.g. inspecting dynamic conditional edges)        (OnEdge)
       │
       ├─ A single node plus its outgoing edges            ────► Local rule
       │     (e.g. redundant_llm_call's per-node bucket)        (OnNode + OnGraph)
       │
       ├─ A path from a source node to a sink node         ────► Path rule
       │     (e.g. pii_leak_scanner's RAG → API)               (Sources/Sinks/Propagate)
       │
       ├─ A subgraph inside a loop / immediate neighborhood (1-2 hop) ──► Path rule
       │     (e.g. error_handler_checker's Tool→Cond)          (Sources/Sinks/Propagate)
       │
       └─ Whole-graph traversal needed (cycle / reachability) ──► Global rule
             (e.g. cycle_detection, unreachable_node)           (AnalyzeGlobal)
```

**When in doubt, start with Local.** Promote to Path / Global only once you discover that the listener cannot complete the decision per single node. The reverse direction (Global → Local) is discouraged because it inflates the computational complexity, as ADR-007 also notes.

### Classification of the 10 Builtin Rules in v0.5

| Tier | Rule | Primary handler | Complexity |
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

The 3-pass pipeline is assembled in [`application/orchestrator.go`](../application/orchestrator.go) and runs in the order Pass 1 = Global → Pass 2 = Local (1walk dispatcher = [`application/walker.go`](../application/walker.go)) → Pass 3 = Path (shared reverse adjacency = [`application/path_walker.go`](../application/path_walker.go)).

---

## 2. Local Rule Template

Define a struct that satisfies the `LocalRule` interface, and at the same time implement `Name()` / `Analyze()` from the legacy `AnalysisRule` (so callers that don't go through the 3-pass pipeline — test doubles, the legacy Orchestrator path — keep working).

```go
// domain/rules/my_local_rule.go
package rules

import (
	"fmt"

	"github.com/hatyibei/shingan/domain"
)

// MyLocalRule detects <what it detects> on a per-node basis.
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

// evaluateMyRule is a pure function shared by both Listener and Analyze.
// It only returns a Finding for a single node input (easy to test).
func evaluateMyRule(n *domain.Node) (domain.Finding, bool) {
	// ... detection logic ...
	return domain.Finding{
		RuleName:         "my_local_rule",
		Severity:         domain.Warning,
		NodeID:           n.ID,
		Message:          fmt.Sprintf("node %q triggers my_local_rule", n.ID),
		Suggestion:       "...",
		Confidence:       0.9,
		ConfidenceReason: domain.ReasonExactStaticMatch, // required by ADR-008
	}, true
}

func init() {
	registerBuiltin(NewMyLocalRule()) // ADR-010: internal registry
}
```

**Key points**

- Have `Listener` and `Analyze` **share the same pure function (`evaluateMyRule`)** to keep maintenance simple (see `deprecated_model.go` / `loopguard.go`).
- If the listener responds to multiple NodeTypes, extract the handler into a variable and register it under several keys in the `OnNode` map (the `NodeTypeLoop` + `NodeTypeControl` pattern in `loopguard.go`).
- To respond to every NodeType, use `OnAny` (see `secret_exposure_scanner.go`).
- When per-node alone cannot decide and graph-wide aggregation is required, accumulate buckets in `OnNode` and emit them in `OnGraph` (the `redundant_llm_call` pattern in `redundant.go`).
- If only edge metadata matters, define `OnEdge` alone (no current builtin uses this pattern, but the `Listener.OnEdge` field is already exposed).

---

## 3. Path Rule Template

The `PathRule` interface consists of four methods: `Meta()` / `Sources()` / `Sinks()` / `Propagate()`. The `PathContext` carries pre-computed **reverse adjacency**, so reverse-BFS / reverse-DFS uses `ctx.Reverse[nodeID]`.

```go
// domain/rules/my_path_rule.go
package rules

import (
	"fmt"

	"github.com/hatyibei/shingan/domain"
)

// MyPathRule detects <condition> along a path from <source> to <sink>.
//
// Tier: Path (ADR-007) — needs adjacency information beyond a single node.
// ConfidenceReason: ReasonHeuristicPattern (taint-propagation-style approximation).
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

// Sources returns the set of starting nodes for path analysis.
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

// Sinks returns the set of ending nodes for path analysis.
// Returning nil is fine when the rule only inspects loops/neighborhoods around
// sources (CostAnalyzer.Sinks in cost.go follows this pattern).
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

// Propagate runs the path analysis using ctx.Sources / ctx.Sinks / ctx.Reverse.
// path_walker.go builds the reverse adjacency once and shares it across all PathRules.
func (r *MyPathRule) Propagate(ctx *domain.PathContext) []domain.Finding {
	if ctx == nil || ctx.Graph == nil || len(ctx.Sinks) == 0 {
		return nil
	}
	var findings []domain.Finding
	for _, sink := range ctx.Sinks {
		// Example: reverse-BFS from each sink.
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
				// ... emit a Finding when a source is reached ...
				queue = append(queue, e.From)
			}
		}
		_ = findings // for illustration only; populate as needed
	}
	return findings
}

// Analyze is the legacy AnalysisRule path. Building the reverse adjacency
// itself is the only difference from the hot path that goes via PathContext.
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

// The typical shape inside Propagate above is to build a message like
// `fmt.Sprintf("source %q reaches sink %q", ...)` and emit a Finding (see pii_leak.go).
```

**Key points**

- `pii_leak_scanner` follows the **reverse-BFS** pattern that walks from sink back toward source (stopping at Human nodes). The skeleton is `runPIIReverseBFS` in [`pii_leak.go`](../domain/rules/pii_leak.go).
- `error_handler_checker` performs forward 1-2 hop lookahead via **graph.OutgoingEdges(nodeID)**. The reason it lives in the Path tier is exactly the rationale ADR-007 cites: "Local handlers cannot reach adjacent edges because the API is single-node only."
- `cost_estimation` doesn't use Sinks; it decides inside Propagate **whether each LLM in Sources belongs to a loop subgraph**. A good example of the Path tier's flexibility.

---

## 4. Global Rule Template

A Global rule is one that views the whole graph in a single pass. `AnalyzeGlobal()` may simply alias the legacy `Analyze()` (this is what `cycle.go` / `reachability.go` do).

```go
// domain/rules/my_global_rule.go
package rules

import (
	"fmt"

	"github.com/hatyibei/shingan/domain"
)

// MyGlobalRule scans the whole graph in a single pass to detect <condition>.
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
// Usually delegates to Analyze (see cycle.go / reachability.go).
func (r *MyGlobalRule) AnalyzeGlobal(graph *domain.WorkflowGraph) []domain.Finding {
	return r.Analyze(graph)
}

// Analyze is the body, also used on the legacy AnalysisRule path.
func (r *MyGlobalRule) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	var findings []domain.Finding
	for id := range graph.Nodes {
		// Example: detect nodes whose fan-out exceeds a threshold.
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

**Algorithm Selection Guide**

| Algorithm | Example | Code |
|---|---|---|
| DFS back-edge | `cycle_detection` | `cycle.go:dfs()` with visitState (unvisited → inProgress → completed) |
| BFS from entry | `unreachable_node` | the queue in `reachability.go:Analyze()` |
| per-node aggregation | `max_parallel_branches` | `len(graph.OutgoingEdges(id))` in `max_parallel_branches.go:Analyze()` |

`global_walker.go` runs each GlobalRule **in parallel goroutines**, so holding no global mutable state is mandatory.

---

## 5. ConfidenceReason Selection Guide (ADR-008)

ADR-008 mandates that every Finding **must populate ConfidenceReason**. The enum is defined in [`domain/finding.go`](../domain/finding.go).

| Reason | Recommended Confidence | When to use | Examples |
|---|---|---|---|
| `ReasonExactStaticMatch` | 1.0 (Critical) / 0.9 (Warning) | DFS back-edge / exact match / config presence check | `cycle_detection`, `deprecated_model` (table lookup), `loop_guard` (missing config) |
| `ReasonOverApproximatedDynamic` | 0.5 | Conservative approximation of a dynamic graph | LangGraph `conditional_edges` returning untyped str (stamped on the parser side; not currently used from rules) |
| `ReasonParserFallback` | 0.4 | Parser dropped information | ADK-Go nodes whose struct field type couldn't be obtained because `go/types` analysis failed |
| `ReasonExperimentalRule` | 0.6 | Newly introduced rule with shallow validation against real corpora | `prompt_injection_sink` and similar right after Phase 2 lands |
| `ReasonHeuristicPattern` | 0.3-0.7 | Name hint / category hint / structural hint | `pii_leak_scanner` (RAG=0.6 / name hint=0.3), `error_handler_checker` (0.8), Info patterns in `secret_exposure_scanner` (0.5) |

### Why these values (rationale)

- **1.0** is reserved for cases that are **logically certain**, like "DFS hit a back-edge / it's listed in a table." `cycle_detection`'s 1.0 confidence is the canonical example.
- **0.9** applies when the call is solid but mixed with secondary judgment (the Warning branch of `deprecated_model` is sure about the model name match, but it also carries a "shutdown timeline prediction").
- **0.7-0.8** corresponds to **strong heuristics**. `error_handler_checker`'s 0.8 is the approximation "if there's a conditional outgoing edge, that's error handling."
- **0.5-0.6** corresponds to **weak heuristics** or conservative approximations. The 0.6 RAG detection in `pii_leak_scanner` cannot conclude PII purely from `category=="rag"`.
- **0.3** is the weakest signal, like a **name hint**. The name-hint path of `pii_leak_scanner` (partial match against `piiHintKeywords`) sits here.

The `--min-confidence` CLI flag (v0.4 and later) filters **by number**, but seeing the Reason lets users interpret why something fell off (rendered in LSP hover / SARIF properties / Markdown reporter).

---

## 6. Severity Guidelines

`domain.Severity` has three values: `Critical` (2) > `Warning` (1) > `Info` (0).

| Severity | Decision criterion | Examples |
|---|---|---|
| **Critical** | **Definitely** fails at runtime / irreversible side effect / static definition error | `loop_guard` (max_iterations missing = infinite loop risk), the shutdown branch of `deprecated_model` (API 404), non-Loop cycles in `cycle_detection` (graph definition error), AWS access key in `secret_exposure_scanner` |
| **Warning** | **May** fail at runtime / noticeable quality issue | `error_handler_checker` (no failure flow), expensive model inside a loop in `cost_estimation`, `redundant_llm_call`, RAG path in `pii_leak_scanner` |
| **Info** | Improvement recommended / convention violation / weak hint | Expensive model on a simple task in `cost_estimation`, name-hint path in `pii_leak_scanner`, jwt / generic_secret in `secret_exposure_scanner` |

The decision criterion is **"how badly will it hurt the user if they merge as-is."** Critical should fail CI (exit code 2 from `shingan analyze`), Warning is exit code 1, Info is exit code 0.

---

## 7. Test Patterns (TDD)

`domain/rules/*_test.go` follows the convention of **using package `rules_test` (black-box tests) plus [`testutil.Builder`](../domain/testutil/builder.go)**. At minimum five cases per rule:

1. **Name() consistency** — string identifier matches expectation
2. **Positive** — Finding is emitted in the case that should be detected (assert Severity / Confidence / RuleName)
3. **Negative** — `len(findings) == 0` in the case that should not be detected
4. **Edge case** — empty graph / nil / missing EntryNodeID / nil Config etc.
5. **ConfidenceReason stamp** — assert that Finding.ConfidenceReason is `ReasonXxx`

### Local rule tests (`deprecated_model_test.go` pattern)

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

### Path rule tests (`pii_leak_test.go` pattern)

A two-tier setup that verifies Sources / Sinks / Propagate **independently** and then verifies end-to-end:

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

For end-to-end, either call `Analyze` (legacy path) or assemble a `PathContext` by hand and call `Propagate` directly. To exercise the real 3-pass pipeline, test at the level of [`application/orchestrator_test.go`](../application/orchestrator_test.go).

### Global rule tests (`cycle_test.go` pattern)

To cover DFS state transitions exhaustively (unvisited → inProgress → completed):

- linear graph (no cycles)
- self-loop (`a → a`)
- multi-step cycle (`a → b → c → a`)
- with / without a parent Loop node
- missing entry node

The `TestCycleDetector_*` suite in cycle_test.go already implements 15+ cases — mirror its structure.

---

## 8. Design Notes for the Existing 10 Rules

A 1-2 line summary plus Severity rationale plus false-positive sources for each rule. A reference card when writing new rules.

| Rule | Tier | Location | Detection summary | Severity / Confidence | False-positive sources |
|---|---|---|---|---|---|
| `deprecated_model` | Local | [deprecated_model.go](../domain/rules/deprecated_model.go) | Match `Config["model"]` against the built-in deprecated table | Critical 1.0 (shutdown) / Warning 0.9 (deprecated) | Table maintenance lag; v1.0 plans to spin out a community registry |
| `loop_guard` | Local | [loopguard.go](../domain/rules/loopguard.go) | `max_iterations` missing on NodeTypeLoop / Control | Critical 1.0 | NodeTypeControl is a deprecated alias and may be removed in the future |
| `redundant_llm_call` | Local | [redundant.go](../domain/rules/redundant.go) | Detects two or more identical (model, prompt_template) | Warning 0.9 | Conditioned on **exact match** of the template string; ignores variable-injection differences |
| `secret_exposure_scanner` | Local | [secret_exposure.go](../domain/rules/secret_exposure.go) | Recursively scans Config with 9 secret regexes | Critical/Warning 0.95 / Info 0.5 | Placeholders (`${VAR}`) are excluded by `placeholderPattern`, but mixed cases (`sk-xxx${SUFFIX}`) are re-evaluated with `hasActualSecret` |
| `pii_leak_scanner` | Path | [pii_leak.go](../domain/rules/pii_leak.go) | reverse-BFS from RAG/has_pii Tool to external Tool, stops at Human | Warning 0.6 (RAG) / Info 0.3 (name hint) | The name hint (`pii`/`user`/`personal`/`private` partial match) is intrinsically heuristic |
| `error_handler_checker` | Path | [errorhandler.go](../domain/rules/errorhandler.go) | No conditional edge after a Tool node / no handler on the Tool side along an LLM→Tool path | Critical (browser) / Warning (api/mcp) / Info (code), 0.8 | False positives possible when the workflow author implements retries on a separate node |
| `cost_estimation` | Path | [cost.go](../domain/rules/cost.go) | High-cost model used inside a loop / on a simple task | Warning (loop) / Info (simple), 0.7 | Externalizing the model price table is being considered for Phase 2 (static map in v0.x) |
| `cycle_detection` | Global | [cycle.go](../domain/rules/cycle.go) | DFS back-edge; combines with the parent Loop's max_iterations to decide Severity | Critical (unbounded) / Warning (>=100) / Info (sub-agent inside Loop), 1.0 | Parent Loop detection walks the path in reverse — possibly misjudges with complex nesting |
| `unreachable_node` | Global | [reachability.go](../domain/rules/reachability.go) | BFS from EntryNodeID; reports unvisited nodes | Warning (LLM/Tool) / Info (others), 1.0 | One Critical when EntryNodeID is unset — that is a user configuration error |
| `max_parallel_branches` | Global | [max_parallel_branches.go](../domain/rules/max_parallel_branches.go) | Measures fan-out via `OutgoingEdges` length, thresholds 10/20/100 | Critical 1.0 (>=100) / Warning 0.9 (>=20) / Info 0.7 (>=10) | Skips nodes that already have `Config["max_concurrency"]` set |

---

## 9. ConfidenceReason linter (`scripts/check_confidence_reason.sh`)

ADR-008 requires "every Finding must carry a ConfidenceReason," but **Go cannot make a struct field mandatory**, so static analysis fills the gap.

[`scripts/check_confidence_reason.sh`](../scripts/check_confidence_reason.sh) walks `domain/rules/*.go` with an awk state machine and:

- detects `domain.Finding{ ... }` literals that contain field assignments (`Foo: bar`) but no `ConfidenceReason:` line
- excludes empty sentinels `domain.Finding{}` (no field assignments)

```bash
$ make check-reason
check_confidence_reason: OK (10 files scanned)

$ make lint            # check-reason + go vet
```

The `lint` job in CI (`.github/workflows/ci.yml`) invokes `make lint`. On violations, offending sites surface like this:

```
domain/rules/foo.go:42: domain.Finding literal missing ConfidenceReason
    domain.Finding{
        RuleName: "foo",
        Severity: domain.Warning,
        ...
    }
check_confidence_reason: 1 file(s) contain offending Finding literals
```

Get into the habit of running `make lint` locally **as soon as you start coding**.

---

## 10. Naming Conventions

| Subject | Convention | Examples |
|---|---|---|
| Rule ID (file name and `Name()`) | snake_case, no verb, content as a noun | `deprecated_model`, `pii_leak_scanner`, `prompt_injection_sink` |
| File name | `domain/rules/<rule_id>.go` (some historical aliases remain) | `deprecated_model.go`, `pii_leak.go` |
| Public constructor | `New<PascalCase>()` | `NewDeprecatedModelChecker()` |
| Internal evaluator | `evaluate<RuleName>()` (per-node pure function) | `evaluateDeprecatedModel`, `evaluateLoopGuard` |

For historical reasons, some file names don't match the rule ID (`loopguard.go` ↔ `loop_guard`, `redundant.go` ↔ `redundant_llm_call`, `pii_leak.go` ↔ `pii_leak_scanner`). **New rules must align strictly.** A "rename everything in v1.0" item sits on the trade-off table (ADR-010).

Don't include verbs: `pii_leak_scanner` not `find_pii_leak`, `max_parallel_branches` not `check_max_concurrency`.

---

## 11. Auto-Registration via registerBuiltin

Per ADR-010, `registerBuiltin()` in `domain/rules/registry.go` is **lower-case and unexported** (not callable from outside). Each rule file registers itself in `init()`:

```go
func init() {
	registerBuiltin(NewMyRule())
}
```

This gives:

1. `domain/rules/registry.go:AllBuiltins()` (capitalized, exported) returns every builtin
2. `AnalyzerFactory.CreateAll()` in [`infrastructure/factory/analyzer.go`](../infrastructure/factory/analyzer.go) pass-throughs `rules.AllBuiltins()`
3. **No factory-side change is needed when adding a new rule** — the `init()` registration alone propagates to every caller (CLI / MCP / web / HTTP API / orchestrator 3-pass)

> **CONTRIBUTING.md may still carry the old steps**: the "add to factory" step became unnecessary when the registry landed in v0.5 (ADR-010). Prefer this document for the current procedure.

---

## 12. Plugin Migration Path After v1.0

Following the ADR-010 direction, the v1.0 release will:

1. Rename and capitalize `registerBuiltin()` to `Register()` so external plugins can call it
2. **Freeze the API surface** of `domain/visitor.go` / `domain/rule.go` / `domain/finding.go` at v1.0 (semver guarantee)
3. Add `docs/plugins/getting-started.md` (this guide is the substrate; edit for external publication)
4. Publish a sample plugin repo (`shingan-plugin-template`) as a separate repository

Until then, throughout the v0.x window, the **only path for external contributors is fork → upstream PR as a builtin** (see CONTRIBUTING.md).

---

## 13. Mapping to the 10 Rules in Phase 2

To be added per ADR-007's Phase 2 expansion plan (20 rules total):

- **Local additions (6)**: `unbounded_tool_arg` / `model_card_mismatch` / `dynamic_node_construction` / `missing_eval_dataset` / `secret_in_prompt_template` / `temperature_misuse`
- **Path additions (4)**: `prompt_injection_sink` / `retry_storm` / `eval_missing` / `circular_dep_agents`

Recommended flow when starting each rule:

1. **Decide the tier**: the flowchart in Section 1 of this guide
2. **Copy the closest existing rule**: 
   - simple Local → `deprecated_model.go`
   - Local + aggregation → `redundant.go`
   - Local across all NodeTypes + recursive Config → `secret_exposure.go`
   - Path reverse-BFS → `pii_leak.go`
   - Path 1-2 hop → `errorhandler.go`
   - Path + loop subgraph → `cost.go`
   - Global DFS → `cycle.go`
   - Global BFS → `reachability.go`
   - Global per-node aggregate → `max_parallel_branches.go`
3. **TDD**: write `*_test.go` first (Section 7 of this guide, at least 5 cases)
4. Run **`make lint && go test ./domain/rules/...`** locally
5. **Update the rule list table in README.md**
6. **Use `ReasonExperimentalRule` for ConfidenceReason right after introduction** (Confidence 0.6) — that aligns with ADR-008's intent

---

## Related Links

- [shingan-adr.md](../shingan-adr.md) — all ADRs
- [docs/architecture.md](./architecture.md) — Onion Architecture details
- [docs/confidence-scoring.md](./confidence-scoring.md) — original design of Confidence/Severity
- [domain/visitor.go](../domain/visitor.go) — Listener / Selector / RuleContext
- [domain/rule.go](../domain/rule.go) — LocalRule / PathRule / GlobalRule / AnalysisRule
- [domain/finding.go](../domain/finding.go) — Finding + ConfidenceReason
- [domain/rules/registry.go](../domain/rules/registry.go) — internal builtin registry
- [application/walker.go](../application/walker.go) — 1walk dispatcher (Local)
- [application/path_walker.go](../application/path_walker.go) — Path tier
- [application/global_walker.go](../application/global_walker.go) — Global tier
- [application/orchestrator.go](../application/orchestrator.go) — 3-pass pipeline
- [scripts/check_confidence_reason.sh](../scripts/check_confidence_reason.sh) — ADR-008 linter
