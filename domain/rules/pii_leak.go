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
// Tier: Path (ADR-007). Sources are RAG/PII tools, Sinks are external Tool
// nodes; Propagate runs reverse-BFS from each sink stopping at Human gates
// and emits Findings for any Source it encounters along the way.
//
// ConfidenceReason: ReasonHeuristicPattern. RAG/has_pii sources are strong
// signals (Confidence 0.6) but the path traversal still relies on naming and
// category hints rather than semantic taint analysis.
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

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (p *PIILeakScanner) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     p.Name(),
		Severity: domain.Warning,
		Fixable:  false,
	}
}

// piiHintKeywords is a list of name substrings that suggest PII content (case-insensitive).
var piiHintKeywords = []string{"pii", "user", "personal", "private"}

// isRAGSource returns true if the node is a definitive PII source:
// a Tool node with category "rag" or has_pii==true in its Config.
func isRAGSource(node *domain.Node) bool {
	if node.Type != domain.NodeTypeTool {
		return false
	}
	if node.Config != nil {
		if v, ok := node.Config["has_pii"]; ok {
			if b, ok := v.(bool); ok && b {
				return true
			}
		}
	}
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

// Sources implements domain.PathRule. It returns every node classified as a
// RAG source or matching a PII-hint keyword. The kind is preserved on the
// node itself; Propagate distinguishes RAG vs hint via isRAGSource/
// isPIIHintSource so we do not duplicate that logic in Sources.
func (p *PIILeakScanner) Sources(g *domain.WorkflowGraph) []*domain.Node {
	if g == nil {
		return nil
	}
	var out []*domain.Node
	for _, n := range g.Nodes {
		if isRAGSource(n) || isPIIHintSource(n) {
			out = append(out, n)
		}
	}
	return out
}

// Sinks implements domain.PathRule. It returns every node that classifies as
// an external Tool sink (api/mcp/browser).
func (p *PIILeakScanner) Sinks(g *domain.WorkflowGraph) []*domain.Node {
	if g == nil {
		return nil
	}
	var out []*domain.Node
	for _, n := range g.Nodes {
		if isExternalSink(n) {
			out = append(out, n)
		}
	}
	return out
}

// Propagate implements domain.PathRule. It runs reverse-BFS from each sink in
// ctx.Sinks, stopping at Human gates, and reports a Finding for each Source
// it discovers along the way.
func (p *PIILeakScanner) Propagate(ctx *domain.PathContext) []domain.Finding {
	if ctx == nil || ctx.Graph == nil || len(ctx.Sources) == 0 || len(ctx.Sinks) == 0 {
		return nil
	}
	return runPIIReverseBFS(ctx.Graph, ctx.Reverse, ctx.Sources, ctx.Sinks)
}

// Analyze keeps the legacy AnalysisRule contract alive.
func (p *PIILeakScanner) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	reverse := make(map[string][]domain.Edge, len(graph.Nodes))
	for _, e := range graph.Edges {
		reverse[e.To] = append(reverse[e.To], e)
	}
	sources := p.Sources(graph)
	sinks := p.Sinks(graph)
	if len(sources) == 0 || len(sinks) == 0 {
		return nil
	}
	return runPIIReverseBFS(graph, reverse, sources, sinks)
}

// runPIIReverseBFS is the path traversal shared by Propagate and the legacy
// Analyze fallback. It performs reverse-BFS from each sink, stopping at
// Human gates, and emits a Finding for each PII source it visits.
func runPIIReverseBFS(graph *domain.WorkflowGraph, reverse map[string][]domain.Edge, sources []*domain.Node, sinks []*domain.Node) []domain.Finding {
	type sourceKind uint8
	const (
		kindRAG sourceKind = iota
		kindHint
	)
	type sourceInfo struct {
		kind sourceKind
		node *domain.Node
	}

	sourceIndex := make(map[string]sourceInfo, len(sources))
	for _, n := range sources {
		if isRAGSource(n) {
			sourceIndex[n.ID] = sourceInfo{kind: kindRAG, node: n}
		} else if isPIIHintSource(n) {
			sourceIndex[n.ID] = sourceInfo{kind: kindHint, node: n}
		}
	}

	var findings []domain.Finding

	for _, sinkNode := range sinks {
		sinkID := sinkNode.ID

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

				if src, ok := sourceIndex[pred]; ok {
					severity := domain.Warning
					confidence := 0.6
					if src.kind == kindHint {
						severity = domain.Info
						confidence = 0.3
					}
					findings = append(findings, domain.Finding{
						RuleName: "pii_leak_scanner",
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
						Confidence:       confidence,
						ConfidenceReason: domain.ReasonHeuristicPattern,
					})
				}

				queue = append(queue, pred)
			}
		}
	}

	return findings
}

func init() {
	registerBuiltin(NewPIILeakScanner())
}
