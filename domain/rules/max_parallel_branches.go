// Package rules contains static analysis rules for Shingan's workflow graph analysis.
package rules

import (
	"fmt"

	"github.com/hatyibei/shingan/domain"
)

// MaxParallelBranchesChecker detects nodes whose fan-out (number of outgoing edges)
// exceeds safe concurrency thresholds for parallel agent execution.
//
// This rule helps prevent API rate limit exhaustion and uncontrolled resource
// consumption when ParallelAgent / LoopAgent / SequentialAgent spawns too many
// concurrent sub-agents.
//
// Tier: Global (ADR-007) — needs to count outgoing edges per node, single
// pass over the edge list. ConfidenceReason: ReasonExactStaticMatch.
//
// Thresholds:
//   - Critical: fan-out >= 100
//   - Warning:  fan-out >= 20
//   - Info:     fan-out >= 10
//
// Exceptions:
//   - If Config["max_concurrency"] is set, the node opts in to explicit concurrency
//     control and is skipped.
type MaxParallelBranchesChecker struct{}

// NewMaxParallelBranchesChecker returns a ready-to-use MaxParallelBranchesChecker.
func NewMaxParallelBranchesChecker() *MaxParallelBranchesChecker {
	return &MaxParallelBranchesChecker{}
}

// Name returns the unique rule identifier.
func (m *MaxParallelBranchesChecker) Name() string {
	return "max_parallel_branches"
}

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (m *MaxParallelBranchesChecker) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     m.Name(),
		Severity: domain.Critical,
		Fixable:  false,
	}
}

// AnalyzeGlobal implements domain.GlobalRule by delegating to Analyze.
func (m *MaxParallelBranchesChecker) AnalyzeGlobal(graph *domain.WorkflowGraph) []domain.Finding {
	return m.Analyze(graph)
}

// Analyze scans all nodes and reports any node whose outgoing edge count (fan-out)
// exceeds the configured thresholds. Nodes with Config["max_concurrency"] set are
// excluded because they have opted in to explicit concurrency control.
func (m *MaxParallelBranchesChecker) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}

	var findings []domain.Finding

	for id, node := range graph.Nodes {
		// max_concurrency 設定があれば尊重してスキップ
		if _, ok := node.Config["max_concurrency"]; ok {
			continue
		}

		out := graph.OutgoingEdges(id)
		fanout := len(out)
		if fanout < 10 {
			continue
		}

		var sev domain.Severity
		var conf float64

		switch {
		case fanout >= 100:
			sev = domain.Critical
			conf = 1.0
		case fanout >= 20:
			sev = domain.Warning
			conf = 0.9
		default: // >= 10
			sev = domain.Info
			conf = 0.7
		}

		findings = append(findings, domain.Finding{
			RuleName:         "max_parallel_branches",
			Severity:         sev,
			NodeID:           id,
			Message:          fmt.Sprintf("node %q has %d outgoing edges (fan-out), may exceed concurrency limits or cause API rate limit", id, fanout),
			Suggestion:       "Chunk sub-agents into groups of 10 or set Config[\"max_concurrency\"] to limit parallel execution",
			Confidence:       conf,
			ConfidenceReason: domain.ReasonExactStaticMatch,
		})
	}

	return findings
}

func init() {
	registerBuiltin(NewMaxParallelBranchesChecker())
}
