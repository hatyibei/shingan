// Package rules contains static analysis rules for Shingan's workflow graph analysis.
package rules

import (
	"fmt"

	"github.com/hatyibei/shingan/domain"
)

// LoopGuardChecker detects Control (LoopAgent) nodes that have no MaxIterations
// configured, which risks unbounded execution.
//
// This rule is complementary to CycleDetector:
//   - CycleDetector finds cyclic graph structures and reports them with context.
//   - LoopGuardChecker directly targets the missing safety guard on the Control node.
type LoopGuardChecker struct{}

// NewLoopGuardChecker returns a ready-to-use LoopGuardChecker.
func NewLoopGuardChecker() *LoopGuardChecker {
	return &LoopGuardChecker{}
}

// Name returns the unique rule identifier.
func (l *LoopGuardChecker) Name() string {
	return "loop_guard"
}

// Analyze scans all nodes and reports any Control node missing a valid max_iterations.
func (l *LoopGuardChecker) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}

	var findings []domain.Finding

	for _, node := range graph.Nodes {
		if node.Type != domain.NodeTypeControl {
			continue
		}

		raw, exists := node.Config["max_iterations"]
		if !exists || raw == nil {
			// max_iterations key is absent entirely.
			findings = append(findings, domain.Finding{
				RuleName: l.Name(),
				Severity: domain.Critical,
				NodeID:   node.ID,
				Message: fmt.Sprintf(
					"LoopAgent %q has no MaxIterations configured — potential infinite loop",
					node.Name,
				),
				Suggestion: "Set MaxIterations to a bounded value " +
					"(recommended: 3-10 for testing, 50-100 for production)",
			})
			continue
		}

		// Key exists — verify it is parseable as an integer.
		if _, err := toInt(raw); err != nil {
			// Non-numeric value; treat as missing.
			findings = append(findings, domain.Finding{
				RuleName: l.Name(),
				Severity: domain.Critical,
				NodeID:   node.ID,
				Message: fmt.Sprintf(
					"LoopAgent %q has no MaxIterations configured — potential infinite loop",
					node.Name,
				),
				Suggestion: "Set MaxIterations to a bounded value " +
					"(recommended: 3-10 for testing, 50-100 for production)",
			})
		}
		// Numeric value present → no finding from LoopGuardChecker
		// (CycleDetector handles the >= 100 Warning separately).
	}

	return findings
}
