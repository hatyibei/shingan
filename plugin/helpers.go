package plugin

import "github.com/hatyibei/shingan/domain"

// NewFinding is the conventional constructor plugin authors should
// use when emitting findings. It fills in sensible defaults
// (Confidence=1.0, ConfidenceReason=ReasonExactStaticMatch) so the
// most common case — "the rule deterministically matched this node" —
// requires zero boilerplate. Override Confidence/ConfidenceReason on
// the returned struct when your detection is heuristic / pattern-
// based / over-approximated.
//
// The signature is the minimum set of fields every Finding needs to
// carry useful information into reporters and IDE integrations:
// who detected it, where, how bad, and why. Suggestion is left for
// the caller to fill — most rules want a context-specific
// remediation hint, not a template.
func NewFinding(ruleName, nodeID string, severity domain.Severity, message string) domain.Finding {
	return domain.Finding{
		RuleName:         ruleName,
		NodeID:           nodeID,
		Severity:         severity,
		Confidence:       1.0,
		ConfidenceReason: domain.ReasonExactStaticMatch,
		Message:          message,
	}
}

// NodesOfType returns every Node in g whose Type matches t. Returns
// an empty slice (not nil) when no node matches, so callers can range
// without a nil check.
//
// The canonical iteration pattern in plugin rules is:
//
//	for _, n := range plugin.NodesOfType(g, domain.NodeTypeLLM) {
//	    if /* something about n.Config */ {
//	        out = append(out, plugin.NewFinding(...))
//	    }
//	}
//
// Order is map-iteration (non-deterministic) — callers needing
// stable ordering should sort by Node.ID before reporting.
func NodesOfType(g *domain.WorkflowGraph, t domain.NodeType) []*domain.Node {
	if g == nil {
		return []*domain.Node{}
	}
	out := make([]*domain.Node, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		if n == nil {
			continue
		}
		if n.Type == t {
			out = append(out, n)
		}
	}
	return out
}
