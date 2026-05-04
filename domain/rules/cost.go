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
	"gpt-4o":            costTierHigh,
	"gpt-4-turbo":       costTierHigh,
	"claude-3-opus":     costTierHigh,
	"claude-3-5-sonnet": costTierHigh,
	"gemini-1.5-pro":    costTierHigh,
	// Mid cost
	"claude-3-haiku":   costTierMid,
	"gemini-1.5-flash": costTierMid,
	// Low cost
	"gpt-4o-mini":   costTierLow,
	"gpt-3.5-turbo": costTierLow,
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
// Tier: Path (ADR-007). The rule needs to know which LLM nodes lie on a
// cycle (loop subgraph) before deciding the severity, so it cannot fire
// from a pure single-node listener. The Path tier gives us the place to
// run the DFS once per analysis and reuse it across LLM checks.
//
// ConfidenceReason: ReasonHeuristicPattern. The high/low tier table is a
// curated list and the cycle membership is deterministic, but the
// "expensive in a loop ⇒ recommend mini" heuristic still depends on the
// workflow author's intent.
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

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (c *CostAnalyzer) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     c.Name(),
		Severity: domain.Warning,
		Fixable:  false,
	}
}

// Sources implements domain.PathRule. It returns every LLM node — these are
// the candidates whose model + loop membership we evaluate.
func (c *CostAnalyzer) Sources(g *domain.WorkflowGraph) []*domain.Node {
	if g == nil {
		return nil
	}
	var out []*domain.Node
	for _, n := range g.Nodes {
		if n.Type == domain.NodeTypeLLM {
			out = append(out, n)
		}
	}
	return out
}

// Sinks implements domain.PathRule. CostAnalyzer does not consume sink
// information directly — the loop subgraph traversal happens inside
// nodesInLoops — so we return nil to signal "no per-sink work".
func (c *CostAnalyzer) Sinks(g *domain.WorkflowGraph) []*domain.Node { return nil }

// Propagate implements domain.PathRule. It computes the set of nodes that lie
// on a cycle (loop subgraph), then iterates over ctx.Sources (the LLMs) and
// emits findings using runCostChecks.
func (c *CostAnalyzer) Propagate(ctx *domain.PathContext) []domain.Finding {
	if ctx == nil || ctx.Graph == nil {
		return nil
	}
	inLoop := nodesInLoops(ctx.Graph)
	return runCostChecks(ctx.Sources, inLoop)
}

// Analyze keeps the legacy AnalysisRule contract alive.
func (c *CostAnalyzer) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	inLoop := nodesInLoops(graph)
	return runCostChecks(c.Sources(graph), inLoop)
}

// runCostChecks emits findings for high-cost LLM nodes based on loop
// membership and declared task complexity.
func runCostChecks(llms []*domain.Node, inLoop map[string]bool) []domain.Finding {
	var findings []domain.Finding

	for _, node := range llms {
		model := stringConfig(node, "model")
		tier := tierFor(model)
		if tier != costTierHigh {
			continue
		}

		if inLoop[node.ID] {
			findings = append(findings, domain.Finding{
				RuleName: "cost_estimation",
				Severity: domain.Warning,
				NodeID:   node.ID,
				Message: fmt.Sprintf(
					"LLM node %q uses high-cost model %q inside a loop",
					node.ID, model,
				),
				Suggestion:       "ループ内で高額モデルが使用されています。推定コストが反復回数×単価でスケールします。miniモデルへの置換を検討してください",
				Confidence:       0.7,
				ConfidenceReason: domain.ReasonHeuristicPattern,
			})
			continue
		}

		complexity := stringConfig(node, "task_complexity")
		if complexity == "simple" {
			findings = append(findings, domain.Finding{
				RuleName: "cost_estimation",
				Severity: domain.Info,
				NodeID:   node.ID,
				Message: fmt.Sprintf(
					"LLM node %q uses high-cost model %q for a simple task",
					node.ID, model,
				),
				Suggestion:       "単純タスクに高額モデルが使用されています。より安価なモデルを検討してください",
				Confidence:       0.7,
				ConfidenceReason: domain.ReasonHeuristicPattern,
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

	var stack []string

	var dfs func(nodeID string)
	dfs = func(nodeID string) {
		state[nodeID] = inProgress
		stack = append(stack, nodeID)

		for _, edge := range graph.OutgoingEdges(nodeID) {
			target := edge.To
			switch state[target] {
			case inProgress:
				for _, id := range stack {
					inLoop[id] = true
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

func init() {
	registerBuiltin(NewCostAnalyzer())
}
