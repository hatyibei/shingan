package rules

import (
	"fmt"

	"github.com/hatyibei/shingan/domain"
)

// ReachabilityChecker detects nodes that are unreachable from the entry node
// of a WorkflowGraph using BFS traversal.
//
// Tier: Global (ADR-007) — single BFS over the entire graph.
// ConfidenceReason: ReasonExactStaticMatch (deterministic BFS reachability).
//
// Severity rules:
//   - EntryNodeID is empty or does not exist in the graph → single Critical finding
//   - Unreachable LLM or Tool node → Warning (wasted implementation)
//   - Unreachable Control, Human, or Output node → Info (may be intentional)
type ReachabilityChecker struct{}

// NewReachabilityChecker returns a ready-to-use ReachabilityChecker.
func NewReachabilityChecker() *ReachabilityChecker {
	return &ReachabilityChecker{}
}

// Name returns the unique rule identifier.
func (r *ReachabilityChecker) Name() string {
	return "unreachable_node"
}

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (r *ReachabilityChecker) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     r.Name(),
		Severity: domain.Warning,
		Fixable:  false,
	}
}

// AnalyzeGlobal implements domain.GlobalRule by delegating to Analyze.
func (r *ReachabilityChecker) AnalyzeGlobal(graph *domain.WorkflowGraph) []domain.Finding {
	return r.Analyze(graph)
}

// Analyze performs BFS from EntryNodeID and reports unreachable nodes.
// If EntryNodeID is unset or not found in the graph, a single Critical finding is returned.
func (r *ReachabilityChecker) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}

	// Guard: entry node must be set and exist in the graph.
	if graph.EntryNodeID == "" {
		return []domain.Finding{
			{
				RuleName:   r.Name(),
				Severity:   domain.Critical,
				NodeID:     "",
				Message:    "entry node is not set: reachability analysis cannot be performed",
				Suggestion:       "Set EntryNodeID to the ID of the node where workflow execution begins.",
				Confidence:       1.0,
				ConfidenceReason: domain.ReasonExactStaticMatch,
			},
		}
	}

	if _, exists := graph.Nodes[graph.EntryNodeID]; !exists {
		return []domain.Finding{
			{
				RuleName: r.Name(),
				Severity: domain.Critical,
				NodeID:   graph.EntryNodeID,
				Message: fmt.Sprintf(
					"entry node %q does not exist in the graph: reachability analysis cannot be performed",
					graph.EntryNodeID,
				),
				Suggestion:       "Ensure EntryNodeID matches a registered node ID.",
				Confidence:       1.0,
				ConfidenceReason: domain.ReasonExactStaticMatch,
			},
		}
	}

	// BFS to collect all reachable node IDs.
	visited := make(map[string]bool, len(graph.Nodes))
	queue := []string{graph.EntryNodeID}
	visited[graph.EntryNodeID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, edge := range graph.OutgoingEdges(current) {
			target := edge.To
			if !visited[target] {
				if _, ok := graph.Nodes[target]; ok {
					visited[target] = true
					queue = append(queue, target)
				}
				// dangling edge reference — out of scope for this rule, skip
			}
		}
	}

	// Report every node that was never visited.
	var findings []domain.Finding
	for id, node := range graph.Nodes {
		if visited[id] {
			continue
		}

		sev := r.severityFor(node.Type)
		findings = append(findings, domain.Finding{
			RuleName: r.Name(),
			Severity: sev,
			NodeID:   id,
			Message: fmt.Sprintf(
				"node %q (type=%s) is unreachable from entry node %q",
				id, nodeTypeName(node.Type), graph.EntryNodeID,
			),
			Suggestion:       "Connect the node to the main workflow or remove it if it is unused.",
			Confidence:       1.0,
			ConfidenceReason: domain.ReasonExactStaticMatch,
		})
	}

	return findings
}

// severityFor returns the appropriate severity for an unreachable node
// based on its type.
func (r *ReachabilityChecker) severityFor(t domain.NodeType) domain.Severity {
	switch t {
	case domain.NodeTypeLLM, domain.NodeTypeTool:
		return domain.Warning
	default:
		return domain.Info
	}
}

// nodeTypeName converts a NodeType to a human-readable string for messages.
func nodeTypeName(t domain.NodeType) string {
	switch t {
	case domain.NodeTypeLLM:
		return "LLM"
	case domain.NodeTypeTool:
		return "Tool"
	case domain.NodeTypeControl:
		return "Control"
	case domain.NodeTypeLoop:
		return "Loop"
	case domain.NodeTypeCondition:
		return "Condition"
	case domain.NodeTypeHuman:
		return "Human"
	case domain.NodeTypeOutput:
		return "Output"
	default:
		return fmt.Sprintf("NodeType(%d)", int(t))
	}
}

func init() {
	registerBuiltin(NewReachabilityChecker())
}
