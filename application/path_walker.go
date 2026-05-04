package application

import (
	"sync"

	"github.com/hatyibei/shingan/domain"
)

// PathWalker drives Path-tier rules (taint propagation, error-handling
// reachability, loop subgraph extraction). For each rule it precomputes the
// Sources/Sinks and a shared reverse adjacency list, then calls Propagate.
//
// The reverse adjacency list is built once per Walk call and reused by every
// rule, saving an O(E) build per rule. Rules run concurrently because each
// produces an independent slice of findings.
type PathWalker struct{}

// NewPathWalker returns a ready-to-use PathWalker.
func NewPathWalker() *PathWalker { return &PathWalker{} }

// Walk runs every Path rule against graph and returns the concatenated
// findings. Nil graph or empty rules return an empty slice.
func (w *PathWalker) Walk(graph *domain.WorkflowGraph, rules []domain.PathRule) []domain.Finding {
	if graph == nil || len(rules) == 0 || len(graph.Nodes) == 0 {
		return nil
	}

	reverse := buildReverseAdjacency(graph)

	results := make(chan []domain.Finding, len(rules))
	var wg sync.WaitGroup
	for _, r := range rules {
		wg.Add(1)
		go func(rule domain.PathRule) {
			defer wg.Done()
			ctx := &domain.PathContext{
				Graph:    graph,
				RuleName: rule.Meta().Name,
				Sources:  rule.Sources(graph),
				Sinks:    rule.Sinks(graph),
				Reverse:  reverse,
			}
			results <- rule.Propagate(ctx)
		}(r)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var out []domain.Finding
	for batch := range results {
		out = append(out, batch...)
	}
	return out
}

// buildReverseAdjacency returns a map keyed by Edge.To so callers can iterate
// predecessors of a node in O(1) lookup time. Each value is a slice of edges
// pointing INTO the keyed node.
func buildReverseAdjacency(g *domain.WorkflowGraph) map[string][]domain.Edge {
	reverse := make(map[string][]domain.Edge, len(g.Nodes))
	for _, e := range g.Edges {
		reverse[e.To] = append(reverse[e.To], e)
	}
	return reverse
}
