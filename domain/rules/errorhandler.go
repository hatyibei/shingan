package rules

import (
	"fmt"

	"github.com/hatyibei/shingan/domain"
)

// ErrorHandlerChecker detects Tool nodes whose outgoing edges are all unconditional,
// meaning there is no conditional branch to handle failure cases.
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
		// Tool has conditional edges directly — error handling present.
		return true
	}
	// 2-hop check: if the next node is a Condition node with conditional edges, count as handled.
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

// Analyze checks for missing error-handling in two complementary ways:
//
//  1. Tool nodes (NodeTypeTool) with outgoing edges that are all unconditional —
//     the traditional check used when tool nodes have explicit outgoing edges.
//     Exception: if the next hop is a Condition node with conditional edges,
//     it counts as error handling (2-hop tracking).
//     Exception: nodes with Config["reliable"]==true are skipped.
//
//  2. LLM nodes (NodeTypeLLM) that have at least one outgoing edge to a Tool node
//     but whose outgoing edges are all unconditional — this covers the common ADK-Go
//     pattern where the parser emits LLM→Tool edges (not Tool→next edges), meaning
//     the Tool nodes themselves are always terminal and the LLM node is the right
//     place to require a conditional error-handling branch.
func (e *ErrorHandlerChecker) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}

	var findings []domain.Finding

	for _, node := range graph.Nodes {
		switch node.Type {
		case domain.NodeTypeTool:
			// Skip reliable (deterministic) tools — they are not expected to fail.
			if isReliable(node) {
				continue
			}
			// Check 1: Tool node with non-empty outgoing edges, no error handling detected.
			outgoing := graph.OutgoingEdges(node.ID)
			if len(outgoing) == 0 {
				continue
			}
			if toolHasErrorHandling(graph, node) {
				continue
			}
			category := toolCategory(node)
			findings = append(findings, domain.Finding{
				RuleName:   e.Name(),
				Severity:   severityForCategory(category),
				NodeID:     node.ID,
				Message:    fmt.Sprintf("Tool node %q (category=%q) has no conditional outgoing edges: error handling is missing", node.ID, category),
				Suggestion: "このノード後に条件分岐ノードを配置して、失敗時フローを定義してください",
				Confidence: 0.8,
			})

		case domain.NodeTypeLLM:
			// Check 2: LLM node connects to one or more Tool nodes that are all terminal
			// (no outgoing edges), meaning no error-handling path exists for tool failures.
			// This covers the ADK-Go pattern where LLM→Tool edges are emitted by the parser
			// and Tool nodes are always terminal (the LLM is responsible for branching).
			outgoing := graph.OutgoingEdges(node.ID)
			if len(outgoing) == 0 {
				continue
			}
			// Collect tool targets.
			var toolTargets []*domain.Node
			for _, edge := range outgoing {
				if target, ok := graph.Nodes[edge.To]; ok && target.Type == domain.NodeTypeTool {
					toolTargets = append(toolTargets, target)
				}
			}
			if len(toolTargets) == 0 {
				continue
			}
			// Check if any tool target has conditional outgoing edges (error handling present).
			anyToolHasHandler := false
			for _, toolNode := range toolTargets {
				toolOut := graph.OutgoingEdges(toolNode.ID)
				for _, e := range toolOut {
					if e.Condition != "" {
						anyToolHasHandler = true
						break
					}
				}
				if anyToolHasHandler {
					break
				}
			}
			// Also check if the LLM node itself has conditional outgoing edges
			// (conditional routing away from tool on error).
			if !anyToolHasHandler && !allUnconditional(outgoing) {
				anyToolHasHandler = true
			}
			if !anyToolHasHandler {
				findings = append(findings, domain.Finding{
					RuleName:   e.Name(),
					Severity:   domain.Warning,
					NodeID:     node.ID,
					Message:    fmt.Sprintf("LLM node %q uses tool(s) but has no conditional outgoing edges: error handling for tool failures is missing", node.ID),
					Suggestion: "ツール呼び出し後に条件分岐ノードを配置して、失敗時フローを定義してください",
					Confidence: 0.8,
				})
			}
		}
	}

	return findings
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
		// Unknown category is treated like "api".
		return domain.Warning
	}
}
