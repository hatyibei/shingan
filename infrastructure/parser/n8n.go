// Package parser provides WorkflowParser implementations for different input formats.
//
// This file implements the n8n workflow JSON parser.
//
// Design decisions (locked in before TDD; see also docs/n8n.md):
//
//  1. Node.ID strategy — n8n's `connections` map is keyed by the human-readable
//     `name` field (not the internal `id`). To avoid threading a name→id lookup
//     through every edge resolver and rule consumer, we use `name` directly as
//     the Shingan `Node.ID`. When two n8n nodes share a name (which n8n's UI
//     forbids) we suffix `_<n>` to disambiguate.
//
//  2. Entry-node detection — n8n exports do not declare an entry node. We pick:
//       (a) The first trigger / webhook node (by array order) if any.
//       (b) Otherwise, the first node with no incoming `main` edges.
//       (c) Otherwise, the first node in the array.
//     This matches how the n8n runtime starts a workflow.
//
//  3. NodeTypeAction does not exist in domain — per ADR-003 the canonical
//     NodeType set is fixed (LLM, Tool, Control, Human, Output, Loop, Condition).
//     We map:
//       - openAi / chatGpt / anthropic / gemini / langchain.* / *llm* / *agent*  → NodeTypeLLM
//       - if / switch                                                            → NodeTypeCondition
//       - code / function / executeCommand                                       → NodeTypeTool with Config["category"]="code_execution"
//                                                                                  (lets eval_missing fire when reachable from an LLM)
//       - httpRequest / *http* / *api*                                           → NodeTypeTool with Config["category"]="api"
//       - webhook / trigger                                                      → NodeTypeTool with Config["category"]="trigger"
//       - everything else                                                        → NodeTypeTool with Config["category"]="api" (default)
//
//  4. Branching edges — n8n's `connections.<name>.main` is a 2-D array:
//       outer index = output port (0 = true / pass, 1 = false / fail for `if`),
//       inner index = parallel destinations from that port.
//     For Condition nodes (if/switch) we tag edges as Condition="true"/"false"
//     for the first two ports and Condition="branch_<n>" for any extras. For
//     non-Condition multi-port nodes we leave Condition="" on port 0 and use
//     "branch_<n>" for n > 0.
//
//  5. Disabled nodes — silently skipped (along with any edge touching them),
//     mirroring n8n's runtime behaviour. Documented in docs/n8n.md.
package parser

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hatyibei/shingan/domain"
)

// ─── n8n JSON schema structs ──────────────────────────────────────────────────

// n8nWorkflow is the top-level n8n export object. Only the fields we actually
// inspect are declared; everything else (settings, pinData, versionId, …) is
// ignored, which keeps the parser forward-compatible across n8n versions.
type n8nWorkflow struct {
	Name        string                          `json:"name"`
	Nodes       []n8nNode                       `json:"nodes"`
	Connections map[string]n8nConnectionsByPort `json:"connections"`
}

// n8nNode is a single n8n node definition.
type n8nNode struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	Parameters map[string]any `json:"parameters"`
	Position   []float64      `json:"position"`
	Disabled   bool           `json:"disabled,omitempty"`
}

// n8nConnectionsByPort is the per-source-node connections map. Keyed by
// connection type (almost always "main"); each value is a 2-D array of
// destinations (outer = output port, inner = parallel).
type n8nConnectionsByPort map[string][][]n8nConnection

// n8nConnection is a single (destination-name, destination-port) tuple.
type n8nConnection struct {
	Node  string `json:"node"`
	Type  string `json:"type"`
	Index int    `json:"index"`
}

// ─── N8nParser ────────────────────────────────────────────────────────────────

// N8nParser parses WorkflowGraph from n8n workflow export JSON.
// SupportedFormat() = "n8n".
type N8nParser struct{}

// NewN8nParser returns a ready-to-use N8nParser.
func NewN8nParser() *N8nParser {
	return &N8nParser{}
}

// SupportedFormat implements application.WorkflowParser.
func (p *N8nParser) SupportedFormat() string {
	return "n8n"
}

// Parse deserializes an n8n workflow JSON document into a WorkflowGraph.
//
// See the package-level comment for the locked-in mapping decisions.
func (p *N8nParser) Parse(input []byte) (*domain.WorkflowGraph, error) {
	var wf n8nWorkflow
	if err := json.Unmarshal(input, &wf); err != nil {
		return nil, fmt.Errorf("n8n parser: unmarshal: %w", err)
	}

	// ── Pass 1: build node table ───────────────────────────────────────────
	// Disabled nodes are skipped; remaining nodes are keyed by name. Duplicate
	// names get a "_<n>" suffix so the resulting graph still has unique IDs.
	nodes := make(map[string]*domain.Node, len(wf.Nodes))
	// nameToID maps the original n8n name to the resolved Shingan Node.ID,
	// so the connection resolver can find it even after suffixing.
	nameToID := make(map[string]string, len(wf.Nodes))
	// nodeOrder preserves array order so entry-node detection is deterministic.
	nodeOrder := make([]string, 0, len(wf.Nodes))

	suffixCounter := make(map[string]int)
	for _, raw := range wf.Nodes {
		if raw.Disabled {
			continue
		}
		nodeID := raw.Name
		if nodeID == "" {
			// Fall back to internal id when n8n drops the name (rare; happens
			// in some legacy exports). Edges keyed by name won't connect to
			// these, but the node still appears so unreachable_node fires.
			nodeID = raw.ID
		}
		// Disambiguate duplicates by appending "_<n>". n8n's UI forbids this
		// so we shouldn't see it in practice — defensive only.
		if _, exists := nodes[nodeID]; exists {
			suffixCounter[nodeID]++
			nodeID = fmt.Sprintf("%s_%d", nodeID, suffixCounter[nodeID])
		}

		nodeType, category := mapN8nNodeType(raw.Type)

		// Carry the n8n parameters through as Config so downstream rules
		// (secret_in_prompt_template, model_card_mismatch, etc.) can inspect
		// model names, prompts, URLs, etc. unchanged.
		cfg := make(map[string]any, len(raw.Parameters)+2)
		for k, v := range raw.Parameters {
			cfg[k] = v
		}
		if category != "" {
			cfg["category"] = category
		}
		cfg["n8n_type"] = raw.Type

		nodes[nodeID] = &domain.Node{
			ID:     nodeID,
			Name:   raw.Name,
			Type:   nodeType,
			Config: cfg,
		}
		nameToID[raw.Name] = nodeID
		nodeOrder = append(nodeOrder, nodeID)
	}

	// ── Pass 2: resolve edges ─────────────────────────────────────────────
	// Iterate connections in deterministic order so test output is stable.
	srcNames := make([]string, 0, len(wf.Connections))
	for src := range wf.Connections {
		srcNames = append(srcNames, src)
	}
	sort.Strings(srcNames)

	var edges []domain.Edge
	for _, srcName := range srcNames {
		fromID, ok := nameToID[srcName]
		if !ok {
			// Source name refers to a disabled / missing node — skip silently.
			continue
		}
		ports := wf.Connections[srcName]
		// Only "main" connections are part of the data flow; n8n's "ai_*"
		// connections (langchain sub-tools, memory, output parsers) are
		// langchain-specific scaffolding that don't move data along the
		// workflow. We expose them in the future when we ship langchain-
		// aware rules; for now we treat them as decorations.
		main := ports["main"]
		fromNode := nodes[fromID]
		isCondition := fromNode != nil && fromNode.Type == domain.NodeTypeCondition
		for portIdx, conns := range main {
			condition := branchCondition(isCondition, portIdx)
			for _, c := range conns {
				toID, ok := nameToID[c.Node]
				if !ok {
					// Destination is disabled / missing — drop.
					continue
				}
				edges = append(edges, domain.Edge{
					From:      fromID,
					To:        toID,
					Condition: condition,
				})
			}
		}
	}

	// ── Pass 3: pick entry node ────────────────────────────────────────────
	entryID := pickEntryNode(nodes, nodeOrder, edges)

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: entryID,
	}, nil
}

// branchCondition returns the Edge.Condition string for the given output port
// index. Condition nodes (if/switch) get "true"/"false" labels for the first
// two ports; everything else uses "branch_<n>" for n > 0 and "" for port 0
// (so a single-port linear flow has unconditional edges).
func branchCondition(isCondition bool, portIdx int) string {
	if isCondition {
		switch portIdx {
		case 0:
			return "true"
		case 1:
			return "false"
		default:
			return fmt.Sprintf("branch_%d", portIdx)
		}
	}
	if portIdx == 0 {
		return ""
	}
	return fmt.Sprintf("branch_%d", portIdx)
}

// pickEntryNode picks the entry node per the documented strategy:
//  1. First trigger/webhook node (by array order).
//  2. First node with no incoming "main" edges (by array order).
//  3. First node in the array.
//
// Returns "" if the graph has no nodes.
func pickEntryNode(nodes map[string]*domain.Node, order []string, edges []domain.Edge) string {
	if len(order) == 0 {
		return ""
	}

	// (1) Look for trigger/webhook first.
	for _, id := range order {
		n, ok := nodes[id]
		if !ok || n == nil {
			continue
		}
		if cat, _ := n.Config["category"].(string); cat == "trigger" {
			return id
		}
	}

	// (2) First node without incoming edges.
	hasIncoming := make(map[string]struct{}, len(edges))
	for _, e := range edges {
		hasIncoming[e.To] = struct{}{}
	}
	for _, id := range order {
		if _, in := hasIncoming[id]; !in {
			return id
		}
	}

	// (3) Fallback: first node in array order.
	return order[0]
}

// ─── NodeType mapping ─────────────────────────────────────────────────────────

// mapN8nNodeType returns the canonical Shingan NodeType plus an optional
// Tool category for the supplied n8n type string. The matcher is case-
// insensitive and substring-based so future n8n type names (`*OpenAi*`,
// `*ChatGpt*`, …) keep working without code changes.
func mapN8nNodeType(t string) (domain.NodeType, string) {
	lt := strings.ToLower(t)

	// LLM family — explicit patterns first because some patterns overlap
	// with the Tool default ("api" e.g. "openai" contains "ai" but not "api").
	switch {
	case containsAny(lt, "openai", "chatgpt", "anthropic", "claude", "gemini",
		"vertex", "bedrock", "ollama", "mistral", "cohere", "huggingface"):
		return domain.NodeTypeLLM, ""
	case strings.Contains(lt, "n8n-nodes-langchain"):
		return domain.NodeTypeLLM, ""
	case containsAny(lt, "ai-agent", "ai_agent", ".agent", "agent.", "/agent"):
		return domain.NodeTypeLLM, ""
	case strings.Contains(lt, "llm"):
		return domain.NodeTypeLLM, ""
	}

	// Code-execution Tools (eval_missing trigger surface).
	if containsAny(lt, ".code", "executecommand", "function", "pythonfunction") {
		return domain.NodeTypeTool, "code_execution"
	}

	// Conditional branching.
	if endsWithAny(lt, ".if", ".switch") || containsAny(lt, "filter", "router") {
		return domain.NodeTypeCondition, ""
	}

	// Triggers / webhooks — entry node candidates. Note: n8n type strings
	// for triggers usually end in "trigger" or are "webhook"; we also accept
	// "manualtrigger" because the manual-fire button is a common entry.
	if containsAny(lt, "webhook", "trigger") {
		return domain.NodeTypeTool, "trigger"
	}

	// HTTP / API.
	if containsAny(lt, "httprequest", "http", ".api", "rest") {
		return domain.NodeTypeTool, "api"
	}

	// Default — treat unknown nodes as generic Tools so the rest of the
	// pipeline (cycle/reachability/error_handler) still works.
	return domain.NodeTypeTool, "api"
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

func endsWithAny(s string, suffixes ...string) bool {
	for _, sx := range suffixes {
		if strings.HasSuffix(s, sx) {
			return true
		}
	}
	return false
}
