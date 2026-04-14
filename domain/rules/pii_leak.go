package rules

import (
	"fmt"
	"strings"

	"github.com/hatyibei/shingan/domain"
)

// externalCategories is the set of tool categories considered as external data sinks.
var externalCategories = map[string]bool{
	"api":     true,
	"mcp":     true,
	"browser": true,
}

// PIILeakScanner detects potential PII leakage paths in a workflow graph.
//
// A leakage path is any route from a PII-source node (RAG node or node with
// has_pii=true) to an external-sink node (Tool with category in
// {api, mcp, browser}) that does not pass through a Human approval node.
//
// Severity rules:
//   - RAG node (category=="rag") or has_pii==true → external sink, no Human gate → Warning
//   - Node whose name contains a PII hint keyword → external sink, no Human gate → Info
type PIILeakScanner struct{}

// NewPIILeakScanner returns a ready-to-use PIILeakScanner.
func NewPIILeakScanner() *PIILeakScanner {
	return &PIILeakScanner{}
}

// Name returns the unique rule identifier.
func (p *PIILeakScanner) Name() string {
	return "pii_leak_scanner"
}

// piiHintKeywords is a list of name substrings that suggest PII content (case-insensitive).
var piiHintKeywords = []string{"pii", "user", "personal", "private"}

// isRAGSource returns true if the node is a definitive PII source:
// a Tool node with category "rag" or has_pii==true in its Config.
func isRAGSource(node *domain.Node) bool {
	if node.Type != domain.NodeTypeTool {
		return false
	}
	// Explicit has_pii flag (JSON bool).
	if node.Config != nil {
		if v, ok := node.Config["has_pii"]; ok {
			if b, ok := v.(bool); ok && b {
				return true
			}
		}
	}
	// RAG category implies potential PII.
	return toolCategory(node) == "rag"
}

// isPIIHintSource returns true if the node's name contains a PII-hint keyword.
// These produce lower-severity (Info) findings.
func isPIIHintSource(node *domain.Node) bool {
	lower := strings.ToLower(node.Name)
	for _, kw := range piiHintKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// isExternalSink returns true if the node is a Tool node whose category is
// an external data sink (api, mcp, or browser).
func isExternalSink(node *domain.Node) bool {
	if node.Type != domain.NodeTypeTool {
		return false
	}
	return externalCategories[toolCategory(node)]
}

// Analyze scans the workflow graph for PII leakage paths and returns findings.
func (p *PIILeakScanner) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}

	var findings []domain.Finding

	for _, node := range graph.Nodes {
		ragSrc := isRAGSource(node)
		hintSrc := !ragSrc && isPIIHintSource(node)

		if !ragSrc && !hintSrc {
			continue
		}

		severity := domain.Warning
		if hintSrc {
			severity = domain.Info
		}

		// BFS from this PII source node.
		// State: visited maps node ID → bool.
		// When we encounter a Human node, we stop that branch (it's gated).
		// When we encounter an external sink, we emit a finding and stop that branch.
		visited := make(map[string]bool)
		visited[node.ID] = true
		queue := []string{node.ID}

		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]

			for _, edge := range graph.OutgoingEdges(current) {
				target := edge.To
				if visited[target] {
					continue
				}
				targetNode, exists := graph.Nodes[target]
				if !exists {
					continue
				}

				// Human gate: stop this branch, PII is protected.
				if targetNode.Type == domain.NodeTypeHuman {
					visited[target] = true
					// Do not expand further from Human — consider it a safe boundary.
					continue
				}

				// External sink without a Human gate: leakage detected.
				if isExternalSink(targetNode) {
					findings = append(findings, domain.Finding{
						RuleName: p.Name(),
						Severity: severity,
						NodeID:   target,
						Message: fmt.Sprintf(
							"potential PII leak: path from RAG/PII node %q (%s) to external tool %q (category=%q) without Human approval gate",
							node.ID, node.Name, target, toolCategory(targetNode),
						),
						Suggestion: fmt.Sprintf(
							"ノード %q と %q の間にHuman承認ノードを挿入するか、PIIフィールドをサニタイズしてください (GDPR/CCPA/個人情報保護法対応)",
							node.ID, target,
						),
					})
					// Mark visited and do not expand further from the sink.
					visited[target] = true
					continue
				}

				// Intermediate node: continue BFS exploration.
				visited[target] = true
				queue = append(queue, target)
			}
		}
	}

	return findings
}
