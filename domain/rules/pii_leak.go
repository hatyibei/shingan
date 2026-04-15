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
//
// Algorithm (v0.2 — O(V+E) reverse-BFS from sinks):
//
//  1. Build forward + reverse adjacency lists in O(E).
//  2. Classify every node into sinks / ragSources / hintSources / humanSet in O(V).
//  3. For each sink, run reverse-BFS stopping at Human nodes.
//     Record all PII sources reachable from the sink in the reverse direction.
//  4. Emit one Finding per (sink, source) pair, preserving the original Severity.
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
//
// v0.2: O(V+E) via reverse-BFS from each sink node.
// Building the reverse adjacency list is O(E); classifying nodes is O(V);
// each reverse-BFS is O(V+E) across all sinks combined (each edge traversed at most once
// per sink that reaches it). In typical workflows with few sinks the algorithm is effectively
// O(V+E) overall.
func (p *PIILeakScanner) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}

	// ── Step 1: Build reverse adjacency list in O(E) ──────────────────────────
	// reverse[to] = list of edges pointing INTO "to"
	reverse := make(map[string][]domain.Edge, len(graph.Nodes))
	for _, e := range graph.Edges {
		reverse[e.To] = append(reverse[e.To], e)
	}

	// ── Step 2: Classify nodes in O(V) ────────────────────────────────────────
	type sourceKind uint8
	const (
		kindRAG  sourceKind = iota // Warning
		kindHint                   // Info
	)

	type sourceInfo struct {
		kind sourceKind
		node *domain.Node
	}

	humanSet := make(map[string]bool, len(graph.Nodes)/10)
	ragSources := make(map[string]sourceInfo, len(graph.Nodes)/5)
	var sinks []string

	for id, n := range graph.Nodes {
		switch {
		case n.Type == domain.NodeTypeHuman:
			humanSet[id] = true
		case isExternalSink(n):
			sinks = append(sinks, id)
		}
		// A node can be both a source and something else (e.g., a Tool with rag category).
		// We check sources regardless of sink/human classification because the original
		// implementation did node-centric classification.
		if isRAGSource(n) {
			ragSources[id] = sourceInfo{kind: kindRAG, node: n}
		} else if isPIIHintSource(n) {
			ragSources[id] = sourceInfo{kind: kindHint, node: n}
		}
	}

	if len(sinks) == 0 || len(ragSources) == 0 {
		return nil
	}

	// ── Step 3: Reverse-BFS from each sink ────────────────────────────────────
	// For each sink, traverse the graph backwards (following reverse edges).
	// Stop at Human nodes — they are safe boundaries.
	// Collect all PII source nodes reachable in the reverse direction.
	var findings []domain.Finding

	for _, sinkID := range sinks {
		sinkNode := graph.Nodes[sinkID]

		visited := make(map[string]bool, len(graph.Nodes))
		visited[sinkID] = true
		queue := []string{sinkID}

		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]

			for _, edge := range reverse[current] {
				pred := edge.From
				if visited[pred] {
					continue
				}
				predNode, exists := graph.Nodes[pred]
				if !exists {
					continue
				}

				// Human gate: this predecessor is a safe boundary.
				// Mark visited but do NOT expand further backwards through it.
				if predNode.Type == domain.NodeTypeHuman {
					visited[pred] = true
					continue
				}

				visited[pred] = true

				// If this predecessor is a PII source, emit a Finding.
				if src, ok := ragSources[pred]; ok {
					severity := domain.Warning
					confidence := 0.6 // RAG source: strong signal
					if src.kind == kindHint {
						severity = domain.Info
						confidence = 0.3 // name hint: heuristic, weak signal
					}
					findings = append(findings, domain.Finding{
						RuleName: p.Name(),
						Severity: severity,
						NodeID:   sinkID,
						Message: fmt.Sprintf(
							"potential PII leak: path from RAG/PII node %q (%s) to external tool %q (category=%q) without Human approval gate",
							pred, src.node.Name, sinkID, toolCategory(sinkNode),
						),
						Suggestion: fmt.Sprintf(
							"ノード %q と %q の間にHuman承認ノードを挿入するか、PIIフィールドをサニタイズしてください (GDPR/CCPA/個人情報保護法対応)",
							pred, sinkID,
						),
						Confidence: confidence,
					})
					// Continue expanding backwards from the source to find more upstream sources.
				}

				queue = append(queue, pred)
			}
		}
	}

	return findings
}
