// Package rules contains static analysis rules for Shingan's workflow graph analysis.
package rules

import (
	"fmt"

	"github.com/hatyibei/shingan/domain"
)

// LoopGuardChecker detects Loop (LoopAgent) nodes that have no MaxIterations
// configured, which risks unbounded execution.
//
// Tier: Local (ADR-007) — decision per node, fits the 1-walk dispatcher.
// ConfidenceReason: ReasonExactStaticMatch (deterministic config check).
//
// This rule applies to NodeTypeLoop and the deprecated NodeTypeControl.
// NodeTypeCondition is excluded because conditional branches do not iterate.
//
// This rule is complementary to CycleDetector:
//   - CycleDetector finds cyclic graph structures and reports them with context.
//   - LoopGuardChecker directly targets the missing safety guard on the Loop node.
type LoopGuardChecker struct{}

// NewLoopGuardChecker returns a ready-to-use LoopGuardChecker.
func NewLoopGuardChecker() *LoopGuardChecker {
	return &LoopGuardChecker{}
}

// Name returns the unique rule identifier.
func (l *LoopGuardChecker) Name() string {
	return "loop_guard"
}

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (l *LoopGuardChecker) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     l.Name(),
		Severity: domain.Critical,
		Fixable:  false,
	}
}

// Listener implements domain.LocalRule. The walker dispatches Loop and the
// deprecated Control node types to the same handler so the legacy alias keeps
// working.
func (l *LoopGuardChecker) Listener(ctx *domain.RuleContext) domain.Listener {
	handle := func(c *domain.RuleContext, n *domain.Node) {
		if f, ok := evaluateLoopGuard(n); ok {
			c.Report(f)
		}
	}
	return domain.Listener{
		OnNode: map[domain.NodeType]domain.NodeHandler{
			domain.NodeTypeLoop:    handle,
			domain.NodeTypeControl: handle, // deprecated alias
		},
	}
}

// Analyze keeps the legacy AnalysisRule contract alive.
func (l *LoopGuardChecker) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	var findings []domain.Finding
	for _, node := range graph.Nodes {
		if !isLoopNode(node.Type) {
			continue
		}
		if f, ok := evaluateLoopGuard(node); ok {
			findings = append(findings, f)
		}
	}
	return findings
}

// isLoopNode returns true for node types that require a max_iterations guard:
// NodeTypeLoop (new) and NodeTypeControl (deprecated, backward-compat alias for Loop).
func isLoopNode(t domain.NodeType) bool {
	return t == domain.NodeTypeLoop || t == domain.NodeTypeControl
}

// evaluateLoopGuard checks node and returns the Finding if max_iterations is
// missing or unparseable. ok is false for safe loops.
func evaluateLoopGuard(node *domain.Node) (domain.Finding, bool) {
	raw, exists := node.Config["max_iterations"]
	if exists && raw != nil {
		if _, err := toInt(raw); err == nil {
			return domain.Finding{}, false
		}
	}
	return domain.Finding{
		RuleName: "loop_guard",
		Severity: domain.Critical,
		NodeID:   node.ID,
		Message: fmt.Sprintf(
			"LoopAgent %q has no MaxIterations configured — potential infinite loop",
			node.Name,
		),
		Suggestion: "Set MaxIterations to a bounded value " +
			"(recommended: 3-10 for testing, 50-100 for production)",
		Confidence:       1.0,
		ConfidenceReason: domain.ReasonExactStaticMatch,
	}, true
}

func init() {
	registerBuiltin(NewLoopGuardChecker())
}
