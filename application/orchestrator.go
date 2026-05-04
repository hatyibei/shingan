// Package application contains use-case logic for Shingan.
// This layer depends only on the domain layer (Onion Architecture).
package application

import (
	"sort"
	"sync"

	"github.com/hatyibei/shingan/domain"
)

// AnalysisOrchestrator runs analysis rules over a WorkflowGraph and
// aggregates their findings into a single, sorted slice.
//
// Refactored rules implement one of the three tier interfaces (LocalRule,
// PathRule, GlobalRule) AND the legacy AnalysisRule, so the orchestrator
// dispatches them via the optimised 1-walk pipeline (ADR-006/007). Rules
// that only implement the legacy AnalysisRule (e.g. test doubles) keep
// running in their own goroutine.
//
// Execution order:
//
//	Pass 1: Global rules (DFS / BFS / fan-out aggregation) — concurrent.
//	Pass 2: Local rules (1-walk + listener dispatch) — single shared walk.
//	Pass 3: Path rules (taint / 2-hop) — concurrent, sharing a reverse adjacency.
//	Legacy: AnalysisRule-only rules — concurrent.
//	Merge: dedupe is the rule's responsibility; we sort the union.
type AnalysisOrchestrator struct {
	walker  *GraphWalker
	pathW   *PathWalker
	globalW *GlobalWalker
}

// NewAnalysisOrchestrator returns a ready-to-use AnalysisOrchestrator.
func NewAnalysisOrchestrator() *AnalysisOrchestrator {
	return &AnalysisOrchestrator{
		walker:  NewGraphWalker(),
		pathW:   NewPathWalker(),
		globalW: NewGlobalWalker(),
	}
}

// Analyze classifies each rule by its highest-priority tier interface and
// dispatches accordingly. The signature matches the pre-refactor public API
// so existing callers (CLI, MCP server, web service, HTTP API) keep working.
//
// Sort order:
//  1. Severity descending (Critical → Warning → Info)
//  2. Confidence descending
//  3. RuleName ascending (deterministic ties)
//
// An empty or nil rules slice returns an empty (non-nil) slice.
// Findings whose Confidence is exactly 0.0 are normalised to 1.0 for
// backward compatibility (rules that have not been migrated yet).
func (o *AnalysisOrchestrator) Analyze(graph *domain.WorkflowGraph, rules []domain.AnalysisRule) []domain.Finding {
	if len(rules) == 0 {
		return []domain.Finding{}
	}

	locals, paths, globals, legacy := classify(rules)

	var (
		mu       sync.Mutex
		combined []domain.Finding
		wg       sync.WaitGroup
	)

	add := func(batch []domain.Finding) {
		if len(batch) == 0 {
			return
		}
		mu.Lock()
		combined = append(combined, batch...)
		mu.Unlock()
	}

	// Pass 1: Global rules. Run first because future Local/Path rules may
	// reuse their results (cycle/reachability info, etc.).
	if len(globals) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			add(o.globalW.Walk(graph, globals))
		}()
	}

	// Pass 2: Local rules — a single shared walk over all nodes/edges.
	if len(locals) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			add(o.walker.Walk(graph, locals))
		}()
	}

	// Pass 3: Path rules.
	if len(paths) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			add(o.pathW.Walk(graph, paths))
		}()
	}

	// Legacy AnalysisRule-only rules continue to run as before. Each gets
	// its own goroutine; channel collects the slices.
	if len(legacy) > 0 {
		legacyOut := make(chan []domain.Finding, len(legacy))
		var lwg sync.WaitGroup
		for _, rule := range legacy {
			lwg.Add(1)
			go func(r domain.AnalysisRule) {
				defer lwg.Done()
				legacyOut <- r.Analyze(graph)
			}(rule)
		}
		go func() {
			lwg.Wait()
			close(legacyOut)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range legacyOut {
				add(batch)
			}
		}()
	}

	wg.Wait()

	// Normalise zero-Confidence findings produced by un-migrated rules.
	for i := range combined {
		if combined[i].Confidence == 0.0 {
			combined[i].Confidence = 1.0
		}
	}

	sort.SliceStable(combined, func(i, j int) bool {
		if combined[i].Severity != combined[j].Severity {
			return combined[i].Severity > combined[j].Severity
		}
		if combined[i].Confidence != combined[j].Confidence {
			return combined[i].Confidence > combined[j].Confidence
		}
		return combined[i].RuleName < combined[j].RuleName
	})

	return combined
}

// classify splits rules into the four execution buckets used by Analyze.
//
// Priority order: GlobalRule > PathRule > LocalRule > AnalysisRule. A rule
// implementing multiple interfaces is dispatched only via the highest-
// priority one (Global beats Path beats Local) so its findings are not
// emitted twice.
func classify(rules []domain.AnalysisRule) (
	locals []domain.LocalRule,
	paths []domain.PathRule,
	globals []domain.GlobalRule,
	legacy []domain.AnalysisRule,
) {
	for _, r := range rules {
		switch v := r.(type) {
		case domain.GlobalRule:
			globals = append(globals, v)
		case domain.PathRule:
			paths = append(paths, v)
		case domain.LocalRule:
			locals = append(locals, v)
		default:
			legacy = append(legacy, r)
		}
	}
	return
}
