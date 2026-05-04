package application

import (
	"sync"

	"github.com/hatyibei/shingan/domain"
)

// GlobalWalker runs Global-tier rules (cycle detection, reachability, fan-out
// aggregation) concurrently. Each rule owns its full graph traversal — the
// walker is just a goroutine fan-out so multiple Global rules complete in
// parallel.
type GlobalWalker struct{}

// NewGlobalWalker returns a ready-to-use GlobalWalker.
func NewGlobalWalker() *GlobalWalker { return &GlobalWalker{} }

// Walk runs every Global rule against graph and returns the concatenated
// findings. Nil graph or empty rules return an empty slice.
func (w *GlobalWalker) Walk(graph *domain.WorkflowGraph, rules []domain.GlobalRule) []domain.Finding {
	if len(rules) == 0 {
		return nil
	}

	results := make(chan []domain.Finding, len(rules))
	var wg sync.WaitGroup
	for _, r := range rules {
		wg.Add(1)
		go func(rule domain.GlobalRule) {
			defer wg.Done()
			results <- rule.AnalyzeGlobal(graph)
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
