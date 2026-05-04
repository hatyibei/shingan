package application

import (
	"github.com/hatyibei/shingan/domain"
)

// GraphWalker performs a single pass over a WorkflowGraph and dispatches each
// visited node/edge to every Local rule's Listener. This is the dispatcher
// described in ADR-006: instead of N rules each running their own O(V+E)
// walk, the walker walks once and calls N handlers per visit.
//
// The walker iterates the Nodes map (not BFS from entry) so that rules
// detecting issues on disconnected nodes (e.g. unreachable LLM with deprecated
// model) keep firing — see ADR-007 where reachability itself is a separate
// Global rule.
type GraphWalker struct{}

// NewGraphWalker returns a ready-to-use GraphWalker.
func NewGraphWalker() *GraphWalker { return &GraphWalker{} }

// Walk runs every Local rule against graph in a single pass and returns the
// concatenated findings. Nil graph or empty rules return an empty slice.
//
// Iteration order:
//  1. for each node in graph.Nodes: dispatch to each rule's OnAny / OnNode[type]
//  2. for each edge in graph.Edges: dispatch to each rule's OnEdge
//  3. for each rule: run OnGraph for any aggregation pass
//
// Each rule operates on its own RuleContext, so concurrent calls from different
// goroutines are safe as long as the rules themselves are stateless.
func (w *GraphWalker) Walk(graph *domain.WorkflowGraph, rules []domain.LocalRule) []domain.Finding {
	if graph == nil || len(rules) == 0 || len(graph.Nodes) == 0 {
		return nil
	}

	// Build per-rule contexts and listeners up front so we can dispatch by
	// type-keyed map lookup inside the hot loop.
	contexts := make([]*domain.RuleContext, len(rules))
	listeners := make([]domain.Listener, len(rules))
	for i, r := range rules {
		ctx := domain.NewRuleContext(graph, r.Meta().Name)
		contexts[i] = ctx
		listeners[i] = r.Listener(ctx)
	}

	// Pass 1: nodes.
	for _, node := range graph.Nodes {
		for i := range listeners {
			l := &listeners[i]
			if l.OnAny != nil {
				l.OnAny(contexts[i], node)
			}
			if l.OnNode != nil {
				if h, ok := l.OnNode[node.Type]; ok {
					h(contexts[i], node)
				}
			}
		}
	}

	// Pass 2: edges.
	for ei := range graph.Edges {
		edge := &graph.Edges[ei]
		for i := range listeners {
			if listeners[i].OnEdge != nil {
				listeners[i].OnEdge(contexts[i], edge)
			}
		}
	}

	// Pass 3: per-rule aggregation.
	for i := range listeners {
		if listeners[i].OnGraph != nil {
			listeners[i].OnGraph(contexts[i], graph)
		}
	}

	// Drain.
	var out []domain.Finding
	for _, ctx := range contexts {
		out = append(out, ctx.Findings()...)
	}
	return out
}
