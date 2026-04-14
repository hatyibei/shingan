package rules

import (
	"fmt"

	"github.com/hatyibei/shingan/domain"
)

// costTier represents the pricing tier of an LLM model.
type costTier int

const (
	costTierUnknown costTier = iota
	costTierLow
	costTierMid
	costTierHigh
)

// modelCostTier maps known model names to their cost tier.
var modelCostTier = map[string]costTier{
	// High cost
	"gpt-4o":             costTierHigh,
	"gpt-4-turbo":        costTierHigh,
	"claude-3-opus":      costTierHigh,
	"claude-3-5-sonnet":  costTierHigh,
	"gemini-1.5-pro":     costTierHigh,
	// Mid cost
	"claude-3-haiku":     costTierMid,
	"gemini-1.5-flash":   costTierMid,
	// Low cost
	"gpt-4o-mini":        costTierLow,
	"gpt-3.5-turbo":      costTierLow,
}

// tierFor returns the costTier for the given model name.
// Unknown models are treated as mid-tier.
func tierFor(model string) costTier {
	if t, ok := modelCostTier[model]; ok {
		return t
	}
	return costTierMid // unknown → mid
}

// CostAnalyzer detects LLM nodes that use expensive models where cheaper
// alternatives would suffice.
//
// Severity rules:
//   - High-cost model inside a loop → Warning
//   - High-cost model outside a loop with task_complexity == "simple" → Info
//   - All other cases → no finding
type CostAnalyzer struct{}

// NewCostAnalyzer returns a ready-to-use CostAnalyzer.
func NewCostAnalyzer() *CostAnalyzer {
	return &CostAnalyzer{}
}

// Name returns the unique rule identifier.
func (c *CostAnalyzer) Name() string {
	return "cost_estimation"
}

// Analyze iterates over all LLM nodes and reports cost-inefficient model
// selections based on loop membership and declared task complexity.
func (c *CostAnalyzer) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}

	// Identify nodes that participate in at least one cycle using DFS back-edge
	// detection (same algorithm as CycleDetector).
	inLoop := nodesInLoops(graph)

	var findings []domain.Finding

	for _, node := range graph.Nodes {
		if node.Type != domain.NodeTypeLLM {
			continue
		}

		// Retrieve model name from Config.
		model := stringConfig(node, "model")

		tier := tierFor(model)
		if tier != costTierHigh {
			// Only high-cost models are flagged.
			continue
		}

		if inLoop[node.ID] {
			findings = append(findings, domain.Finding{
				RuleName: c.Name(),
				Severity: domain.Warning,
				NodeID:   node.ID,
				Message: fmt.Sprintf(
					"LLM node %q uses high-cost model %q inside a loop",
					node.ID, model,
				),
				Suggestion: "ループ内で高額モデルが使用されています。推定コストが反復回数×単価でスケールします。miniモデルへの置換を検討してください",
			})
			continue
		}

		// Outside a loop — check task_complexity.
		complexity := stringConfig(node, "task_complexity")
		if complexity == "simple" {
			findings = append(findings, domain.Finding{
				RuleName: c.Name(),
				Severity: domain.Info,
				NodeID:   node.ID,
				Message: fmt.Sprintf(
					"LLM node %q uses high-cost model %q for a simple task",
					node.ID, model,
				),
				Suggestion: "単純タスクに高額モデルが使用されています。より安価なモデルを検討してください",
			})
		}
	}

	return findings
}

// stringConfig returns node.Config[key] as a string, or "" if absent or not a string.
func stringConfig(node *domain.Node, key string) string {
	if node.Config == nil {
		return ""
	}
	v, ok := node.Config[key]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// nodesInLoops returns the set of node IDs that lie on at least one cycle in
// the graph. It uses DFS back-edge detection: whenever a back edge (u → v) is
// found, all nodes currently on the DFS stack between v and u (inclusive) are
// members of that cycle.
func nodesInLoops(graph *domain.WorkflowGraph) map[string]bool {
	inLoop := make(map[string]bool)
	state := make(map[string]visitState, len(graph.Nodes))

	// stack holds the current DFS path (ordered list of node IDs).
	var stack []string

	var dfs func(nodeID string)
	dfs = func(nodeID string) {
		state[nodeID] = inProgress
		stack = append(stack, nodeID)

		for _, edge := range graph.OutgoingEdges(nodeID) {
			target := edge.To
			switch state[target] {
			case inProgress:
				// Back edge found — mark all nodes from target to current as in-loop.
				for _, id := range stack {
					inLoop[id] = true
					// Once we reach target the entire relevant portion is marked;
					// continue to mark the rest of the stack up to nodeID as well.
				}
			case unvisited:
				if _, exists := graph.Nodes[target]; exists {
					dfs(target)
				}
			case completed:
				// already processed
			}
		}

		stack = stack[:len(stack)-1]
		state[nodeID] = completed
	}

	if _, ok := graph.Nodes[graph.EntryNodeID]; ok {
		dfs(graph.EntryNodeID)
	}
	for id := range graph.Nodes {
		if state[id] == unvisited {
			dfs(id)
		}
	}

	return inLoop
}
