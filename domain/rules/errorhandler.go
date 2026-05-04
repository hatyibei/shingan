package rules

import (
	"fmt"

	"github.com/hatyibei/shingan/domain"
)

// ErrorHandlerChecker detects Tool nodes whose outgoing edges are all unconditional,
// meaning there is no conditional branch to handle failure cases.
//
// Tier: Path (ADR-007). The check needs adjacency information beyond a single
// node — for the Tool branch we look 1-2 hops downstream for a Condition
// node, and for the LLM branch we look at the LLM's tool targets and inspect
// their outgoing edges. Putting both inside a Path rule keeps the helper
// graph traversal in one place; the GraphWalker's per-node handler would
// otherwise need access to outgoing edges, which violates the Local rule
// contract that handlers operate on a single node.
//
// ConfidenceReason: ReasonHeuristicPattern. The presence of a conditional
// outgoing edge is a heuristic for "error handling exists"; missing edges
// can also mean the workflow author models retries elsewhere.
//
// Severity by Tool category (Node.Config["category"]):
//   - "browser"        → Critical  (GUI operations are the most failure-prone)
//   - "api" or "mcp"   → Warning
//   - "code"           → Info
//   - (unset / other)  → Warning   (treated as "api")
type ErrorHandlerChecker struct{}

// NewErrorHandlerChecker returns a ready-to-use ErrorHandlerChecker.
func NewErrorHandlerChecker() *ErrorHandlerChecker {
	return &ErrorHandlerChecker{}
}

// Name returns the unique rule identifier.
func (e *ErrorHandlerChecker) Name() string {
	return "error_handler_checker"
}

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (e *ErrorHandlerChecker) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     e.Name(),
		Severity: domain.Warning,
		Fixable:  false,
	}
}

// Sources implements domain.PathRule. It returns every Tool node — the
// classic "tool is unreliable, must be guarded" check fires on these.
func (e *ErrorHandlerChecker) Sources(g *domain.WorkflowGraph) []*domain.Node {
	if g == nil {
		return nil
	}
	var out []*domain.Node
	for _, n := range g.Nodes {
		if n.Type == domain.NodeTypeTool {
			out = append(out, n)
		}
	}
	return out
}

// Sinks implements domain.PathRule. It returns every LLM node — the second
// branch of the rule fires on LLMs that reach a Tool with no error handling.
func (e *ErrorHandlerChecker) Sinks(g *domain.WorkflowGraph) []*domain.Node {
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

// Propagate implements domain.PathRule. ctx.Sources holds the Tool nodes and
// ctx.Sinks holds the LLM nodes; runErrorHandlerChecks runs both branches.
func (e *ErrorHandlerChecker) Propagate(ctx *domain.PathContext) []domain.Finding {
	if ctx == nil || ctx.Graph == nil {
		return nil
	}
	return runErrorHandlerChecks(ctx.Graph, ctx.Sources, ctx.Sinks)
}

// Analyze keeps the legacy AnalysisRule contract alive.
func (e *ErrorHandlerChecker) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	return runErrorHandlerChecks(graph, e.Sources(graph), e.Sinks(graph))
}

// runErrorHandlerChecks contains the original two-branch detection logic,
// extracted so Analyze and Propagate share one implementation.
func runErrorHandlerChecks(graph *domain.WorkflowGraph, tools []*domain.Node, llms []*domain.Node) []domain.Finding {
	var findings []domain.Finding

	for _, node := range tools {
		// Skip reliable (deterministic) tools — they are not expected to fail.
		if isReliable(node) {
			continue
		}
		outgoing := graph.OutgoingEdges(node.ID)
		if len(outgoing) == 0 {
			continue
		}
		if toolHasErrorHandling(graph, node) {
			continue
		}
		category := toolCategory(node)
		findings = append(findings, domain.Finding{
			RuleName:         "error_handler_checker",
			Severity:         severityForCategory(category),
			NodeID:           node.ID,
			Message:          fmt.Sprintf("Tool node %q (category=%q) has no conditional outgoing edges: error handling is missing", node.ID, category),
			Suggestion:       "このノード後に条件分岐ノードを配置して、失敗時フローを定義してください",
			Confidence:       0.8,
			ConfidenceReason: domain.ReasonHeuristicPattern,
		})
	}

	for _, node := range llms {
		outgoing := graph.OutgoingEdges(node.ID)
		if len(outgoing) == 0 {
			continue
		}
		var toolTargets []*domain.Node
		for _, edge := range outgoing {
			if target, ok := graph.Nodes[edge.To]; ok && target.Type == domain.NodeTypeTool {
				toolTargets = append(toolTargets, target)
			}
		}
		if len(toolTargets) == 0 {
			continue
		}
		anyToolHasHandler := false
		for _, toolNode := range toolTargets {
			toolOut := graph.OutgoingEdges(toolNode.ID)
			for _, edge := range toolOut {
				if edge.Condition != "" {
					anyToolHasHandler = true
					break
				}
			}
			if anyToolHasHandler {
				break
			}
		}
		if !anyToolHasHandler && !allUnconditional(outgoing) {
			anyToolHasHandler = true
		}
		if !anyToolHasHandler {
			findings = append(findings, domain.Finding{
				RuleName:         "error_handler_checker",
				Severity:         domain.Warning,
				NodeID:           node.ID,
				Message:          fmt.Sprintf("LLM node %q uses tool(s) but has no conditional outgoing edges: error handling for tool failures is missing", node.ID),
				Suggestion:       "ツール呼び出し後に条件分岐ノードを配置して、失敗時フローを定義してください",
				Confidence:       0.8,
				ConfidenceReason: domain.ReasonHeuristicPattern,
			})
		}
	}

	return findings
}

// hasConditionBranch returns true if the given node has at least one outgoing
// conditional edge — meaning it has error-handling in place.
func hasConditionBranch(graph *domain.WorkflowGraph, nodeID string) bool {
	for _, e := range graph.OutgoingEdges(nodeID) {
		if e.Condition != "" {
			return true
		}
	}
	return false
}

// isConditionNode returns true for NodeTypeCondition and the deprecated NodeTypeControl
// (which is treated as a loop but may also appear as a condition-like branch in old graphs).
func isConditionNode(t domain.NodeType) bool {
	return t == domain.NodeTypeCondition || t == domain.NodeTypeControl
}

// toolHasErrorHandling returns true if the given Tool node has error handling.
// Error handling is detected in two ways:
//  1. The Tool node itself has at least one conditional outgoing edge.
//  2. The next hop is a Condition node (NodeTypeCondition or deprecated NodeTypeControl)
//     and that Condition node has at least one conditional outgoing edge.
func toolHasErrorHandling(graph *domain.WorkflowGraph, toolNode *domain.Node) bool {
	outgoing := graph.OutgoingEdges(toolNode.ID)
	if !allUnconditional(outgoing) {
		return true
	}
	for _, edge := range outgoing {
		next, ok := graph.Nodes[edge.To]
		if !ok {
			continue
		}
		if isConditionNode(next.Type) && hasConditionBranch(graph, next.ID) {
			return true
		}
	}
	return false
}

// isReliable returns true if the node has Config["reliable"] == true.
// Reliable nodes (pure functions, deterministic algorithms) are excluded from
// error-handler checks because they are not expected to fail.
func isReliable(node *domain.Node) bool {
	if node.Config == nil {
		return false
	}
	v, ok := node.Config["reliable"]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

// allUnconditional returns true when every edge in the slice has an empty Condition.
func allUnconditional(edges []domain.Edge) bool {
	for _, e := range edges {
		if e.Condition != "" {
			return false
		}
	}
	return true
}

// toolCategory returns the category string for a Tool node.
// If Node.Config["category"] is not set or is not a string, it defaults to "api".
func toolCategory(node *domain.Node) string {
	if node.Config == nil {
		return "api"
	}
	raw, ok := node.Config["category"]
	if !ok || raw == nil {
		return "api"
	}
	cat, ok := raw.(string)
	if !ok || cat == "" {
		return "api"
	}
	return cat
}

// severityForCategory maps a Tool category to the appropriate Severity.
func severityForCategory(category string) domain.Severity {
	switch category {
	case "browser":
		return domain.Critical
	case "api", "mcp":
		return domain.Warning
	case "code":
		return domain.Info
	default:
		return domain.Warning
	}
}

func init() {
	registerBuiltin(NewErrorHandlerChecker())
}
