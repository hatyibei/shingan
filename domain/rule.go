package domain

// RuleMeta carries the metadata every rule (Local, Path, Global) advertises to
// the orchestrator and reporters.
type RuleMeta struct {
	// Name is the unique identifier for the rule (e.g. "deprecated_model").
	Name string
	// Severity is the default severity used when a rule does not override it
	// per-finding. Individual Findings may still set their own Severity.
	Severity Severity
	// Fixable indicates the rule can produce an auto-fix (TextEdit). Reporters
	// can surface this in IDE/editor integrations.
	Fixable bool
}

// AnalysisRule is the legacy contract implemented by every Shingan analysis
// rule prior to the visitor refactor (ADR-006).
//
// New rules should implement one of LocalRule / PathRule / GlobalRule instead.
// AnalysisRule is preserved so that pre-existing callers (CLI, MCP server,
// HTTP service, web app) and external test doubles continue to work unchanged
// during the transition. Refactored rules implement BOTH this interface and
// the new tier interface so they can be passed through []AnalysisRule
// pipelines while still benefiting from the 1-walk dispatcher.
//
// Deprecated: implement LocalRule, PathRule, or GlobalRule for new rules.
type AnalysisRule interface {
	// Name returns the unique identifier for this rule (e.g. "cycle_detection").
	Name() string

	// Analyze inspects the given WorkflowGraph and returns all findings.
	// An empty (or nil) slice means no issues were detected.
	Analyze(graph *WorkflowGraph) []Finding
}

// LocalRule covers rules whose decision can be made by inspecting a single
// node or edge in isolation. The walker visits each node exactly once and
// dispatches to the listener's OnNode/OnAny handler.
//
// Examples: deprecated_model, secret_exposure, loop_guard, redundant_llm_call.
type LocalRule interface {
	Meta() RuleMeta
	// Listener returns the handler bundle the GraphWalker should dispatch to.
	// The returned listener may close over ctx to call ctx.Report on match.
	Listener(ctx *RuleContext) Listener
}

// PathRule covers rules that need to follow paths between source nodes and
// sink nodes (taint propagation, error-handling reachability, loop subgraph
// extraction). The orchestrator computes Sources and Sinks once per graph
// and then invokes Propagate, which is responsible for the path traversal.
//
// Examples: pii_leak_scanner, error_handler_checker, cost_estimation.
type PathRule interface {
	Meta() RuleMeta
	// Sources returns the start nodes for path analysis (e.g. PII data sources).
	Sources(g *WorkflowGraph) []*Node
	// Sinks returns the terminal nodes for path analysis (e.g. external APIs).
	Sinks(g *WorkflowGraph) []*Node
	// Propagate runs the path-traversal logic and returns findings. It receives
	// a PathContext that gives the rule access to the graph, sources, sinks and
	// shared helpers (reverse adjacency, BFS).
	Propagate(ctx *PathContext) []Finding
}

// GlobalRule covers rules that require a single pass over the entire graph
// (cycle detection via DFS, BFS reachability, fan-out aggregation). They are
// run before Local and Path passes because the latter may consume their
// results in the future.
//
// Examples: cycle_detection, unreachable_node, max_parallel_branches.
type GlobalRule interface {
	Meta() RuleMeta
	AnalyzeGlobal(g *WorkflowGraph) []Finding
}

// PathContext is supplied to PathRule.Propagate. It exposes the graph being
// analysed plus the precomputed Sources/Sinks for the rule.
//
// PathContext is part of the public contract for PathRule implementations and
// is filled in by application/path_walker.go. Only fields the rule needs are
// exposed; the walker keeps any internal scratch state private.
type PathContext struct {
	Graph    *WorkflowGraph
	RuleName string
	Sources  []*Node
	Sinks    []*Node

	// Reverse is the precomputed reverse adjacency list (Edge.To → []Edge).
	// It is shared across rules in the same Path pass, saving an O(E) build
	// per rule. Treat it as read-only.
	Reverse map[string][]Edge
}
