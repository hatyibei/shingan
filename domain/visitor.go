package domain

// NodeHandler is invoked by the GraphWalker for each node that matches a
// Listener's selector. ctx exposes Report() so rules can emit Findings without
// allocating their own slices.
type NodeHandler func(ctx *RuleContext, node *Node)

// EdgeHandler is invoked by the GraphWalker for every edge in the graph.
type EdgeHandler func(ctx *RuleContext, edge *Edge)

// GraphHandler is invoked once per graph after node/edge traversal completes.
// It is intended for rules that need a final aggregation pass (e.g. duplicate
// detection that groups findings by some key).
type GraphHandler func(ctx *RuleContext, g *WorkflowGraph)

// Listener bundles handlers a Local rule wants the walker to dispatch to it.
//
// OnNode is keyed by NodeType — a handler is invoked only when the walker
// visits a node of that type. Use the zero NodeType (NodeTypeLLM) and a
// custom predicate inside the handler if you need polymorphic dispatch.
//
// All fields are optional. A rule that only inspects edges may leave OnNode
// nil; a rule that only needs the final aggregation pass may set OnGraph alone.
type Listener struct {
	OnNode  map[NodeType]NodeHandler
	OnAny   NodeHandler // fired for every node regardless of type
	OnEdge  EdgeHandler
	OnGraph GraphHandler
}

// Predicate is a structural filter applied to a node. It receives the graph
// and the candidate node and returns true if the node satisfies the filter.
type Predicate func(g *WorkflowGraph, n *Node) bool

// Selector narrows the set of nodes a listener cares about. The walker first
// checks NodeTypes (if non-empty) and then evaluates each Predicate in order.
// All predicates must return true for the node to be considered a match.
//
// Selectors are optional — a Listener may use OnNode/OnAny directly without
// declaring a Selector. The Selector type is provided as an extension point
// for future ESLint-esquery-style filtering (ADR-006).
type Selector struct {
	NodeTypes  []NodeType
	Predicates []Predicate
}

// RuleContext is the per-rule scratchpad passed to handlers. It exposes the
// graph being analysed and accumulates findings via Report.
//
// RuleContext is not safe for concurrent use by a single rule's handlers, but
// the walker creates a fresh context per rule so different rules running in
// parallel do not share state.
type RuleContext struct {
	Graph    *WorkflowGraph
	RuleName string

	findings []Finding
}

// NewRuleContext returns a RuleContext bound to graph for ruleName. The
// returned context starts with an empty Findings slice.
func NewRuleContext(graph *WorkflowGraph, ruleName string) *RuleContext {
	return &RuleContext{Graph: graph, RuleName: ruleName}
}

// Report appends f to the context's accumulated findings. The walker's
// dispatch loop drains the slice via Findings() after each rule finishes.
//
// If f.RuleName is empty it is set from the context to reduce boilerplate
// inside rule handlers.
func (c *RuleContext) Report(f Finding) {
	if f.RuleName == "" {
		f.RuleName = c.RuleName
	}
	c.findings = append(c.findings, f)
}

// Findings returns the accumulated findings and resets the internal buffer.
// Call this once after the walker has finished dispatching to the rule's
// listener.
func (c *RuleContext) Findings() []Finding {
	out := c.findings
	c.findings = nil
	return out
}
