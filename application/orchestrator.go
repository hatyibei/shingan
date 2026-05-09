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

// GraphWithSource pairs a WorkflowGraph with the source file that produced
// it. Used by the multi-file directory pipeline (ADR-012) so per-file
// independent graphs can attribute their findings back to the originating
// file via Finding.SourceFile.
type GraphWithSource struct {
	Graph      *domain.WorkflowGraph
	SourceFile string
}

// AnalyzeMulti runs Analyze on each (graph, sourceFile) pair independently
// and returns a single sorted []Finding with each Finding stamped with its
// graph's SourceFile. Sort order matches Analyze (Severity DESC →
// Confidence DESC → RuleName ASC); all per-graph results are concatenated
// before the final sort so two findings from different files with the same
// (severity, confidence, rule) interleave deterministically.
//
// Per ADR-012: this is the canonical entry point for directory-mode
// analysis (multi-file ADK-Go, multi-file LangGraph) so independent agent
// definitions in different files are NOT merged into a single graph and
// don't produce spurious unreachable_node findings.
//
// An empty `inputs` slice returns an empty (non-nil) slice. nil inputs
// are skipped silently.
func (o *AnalysisOrchestrator) AnalyzeMulti(inputs []GraphWithSource, rules []domain.AnalysisRule) []domain.Finding {
	if len(inputs) == 0 || len(rules) == 0 {
		return []domain.Finding{}
	}

	// Build a per-source-file PosResolver as we go so the ignore-comment
	// filter can map (sourceFile, nodeID) → (file, line) without re-walking
	// the inputs.
	graphsBySource := make(map[string]*domain.WorkflowGraph, len(inputs))

	combined := make([]domain.Finding, 0, len(inputs)*8)
	for _, in := range inputs {
		if in.Graph == nil {
			continue
		}
		graphsBySource[in.SourceFile] = in.Graph
		findings := o.Analyze(in.Graph, rules)
		for i := range findings {
			findings[i].SourceFile = in.SourceFile
		}
		combined = append(combined, findings...)
	}

	// Apply `# shingan: ignore` markers (line / next-line / file scope).
	// Operational-trust feature: developers opt out of specific findings
	// without touching CI policy.
	combined = FilterIgnoredFindings(combined, func(sourceFile, nodeID string) (string, int) {
		g := graphsBySource[sourceFile]
		if g == nil || g.Nodes == nil {
			return sourceFile, 0
		}
		n, ok := g.Nodes[nodeID]
		if !ok || n == nil {
			return sourceFile, 0
		}
		file := n.Pos.File
		if file == "" {
			file = sourceFile
		}
		return file, n.Pos.Line
	})

	sort.SliceStable(combined, func(i, j int) bool {
		if combined[i].Severity != combined[j].Severity {
			return combined[i].Severity > combined[j].Severity
		}
		if combined[i].Confidence != combined[j].Confidence {
			return combined[i].Confidence > combined[j].Confidence
		}
		if combined[i].RuleName != combined[j].RuleName {
			return combined[i].RuleName < combined[j].RuleName
		}
		// Stable tiebreaker for findings sharing rule + severity + confidence
		// across files: SourceFile path ASC. Keeps directory output
		// deterministic regardless of filesystem walk order.
		return combined[i].SourceFile < combined[j].SourceFile
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
