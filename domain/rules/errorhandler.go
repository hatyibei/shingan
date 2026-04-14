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

// Analyze iterates over all Tool nodes and reports those whose outgoing edges
// are entirely unconditional (i.e., no error-handling branch is present).
func (e *ErrorHandlerChecker) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}

	var findings []domain.Finding

	for _, node := range graph.Nodes {
		if node.Type != domain.NodeTypeTool {
			continue
		}

		outgoing := graph.OutgoingEdges(node.ID)

		// A node with no outgoing edges is a terminal node.
		// There is nothing to handle — not flagged by this rule.
		if len(outgoing) == 0 {
			continue
		}

		// Check whether at least one outgoing edge has a condition.
		hasConditional := false
		for _, edge := range outgoing {
			if edge.Condition != "" {
				hasConditional = true
				break
			}
		}

		if hasConditional {
			// Error handling is present — no finding.
			continue
		}

		// All edges are unconditional: error handling is missing.
		category := toolCategory(node)
		severity := severityForCategory(category)

		findings = append(findings, domain.Finding{
			RuleName:   e.Name(),
			Severity:   severity,
			NodeID:     node.ID,
			Message:    fmt.Sprintf("Tool node %q (category=%q) has no conditional outgoing edges: error handling is missing", node.ID, category),
			Suggestion: "このノード後に条件分岐ノードを配置して、失敗時フローを定義してください",
		})
	}

	return findings
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
