// Package domain defines the core types for Shingan's workflow graph analysis.
// This package has no external dependencies — stdlib only.
package domain

import (
	"encoding/json"
	"fmt"
)

// NodeType represents the category of a workflow node.
type NodeType int

const (
	// NodeTypeLLM represents a node that calls a Large Language Model.
	NodeTypeLLM NodeType = iota
	// NodeTypeTool represents a node that invokes an external tool or API.
	NodeTypeTool
	// NodeTypeControl represents a control-flow node (loop, conditional branch, etc.).
	// Deprecated: use NodeTypeLoop or NodeTypeCondition instead.
	// For backward compatibility, JSON "control" is parsed as NodeTypeLoop.
	NodeTypeControl
	// NodeTypeHuman represents a human-in-the-loop approval/review node.
	NodeTypeHuman
	// NodeTypeOutput represents a terminal output node.
	NodeTypeOutput
	// NodeTypeLoop represents a LoopAgent node (max_iterations required).
	NodeTypeLoop
	// NodeTypeCondition represents a conditional branch node (if/switch, max_iterations not required).
	NodeTypeCondition
)

// nodeTypeStrings maps the canonical string names to NodeType values.
// Note: "control" maps to NodeTypeLoop for backward compatibility.
var nodeTypeStrings = map[string]NodeType{
	"llm":       NodeTypeLLM,
	"tool":      NodeTypeTool,
	"control":   NodeTypeLoop, // backward-compat: "control" → Loop
	"human":     NodeTypeHuman,
	"output":    NodeTypeOutput,
	"loop":      NodeTypeLoop,
	"condition": NodeTypeCondition,
}

// String returns the lowercase string representation of a NodeType.
func (t NodeType) String() string {
	switch t {
	case NodeTypeLLM:
		return "llm"
	case NodeTypeTool:
		return "tool"
	case NodeTypeControl:
		return "control"
	case NodeTypeHuman:
		return "human"
	case NodeTypeOutput:
		return "output"
	case NodeTypeLoop:
		return "loop"
	case NodeTypeCondition:
		return "condition"
	default:
		return fmt.Sprintf("NodeType(%d)", int(t))
	}
}

// MarshalJSON serializes NodeType as its string name (e.g. "llm").
func (t NodeType) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.String())
}

// UnmarshalJSON deserializes NodeType from either a string ("llm") or an integer (0).
// The string "control" is accepted for backward compatibility and treated as Loop.
func (t *NodeType) UnmarshalJSON(data []byte) error {
	// Try string first.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		nt, ok := nodeTypeStrings[s]
		if !ok {
			return fmt.Errorf("unknown node type string %q: expected one of llm, tool, control, human, output, loop, condition", s)
		}
		*t = nt
		return nil
	}

	// Fall back to integer.
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("node type must be a string (\"llm\") or integer (0): %w", err)
	}
	*t = NodeType(n)
	return nil
}

// SourcePos describes the location of a Node in the original source artifact
// (e.g. a .go file, JSON document, or exported LangGraph spec).
// All fields are optional — Parsers that cannot determine the position should
// leave it zero. Position-aware features (LSP, CodeAction, VS Code extension)
// consult SourcePos.IsZero to decide whether to surface location information.
type SourcePos struct {
	// File is the source file path (parser-defined; may be empty for embedded inputs).
	File string `json:"file,omitempty"`
	// Line is the 1-based line number. Zero means "unset".
	Line int `json:"line,omitempty"`
	// Col is the 1-based column number. Zero means "unset".
	Col int `json:"col,omitempty"`
}

// IsZero reports whether p carries no position information.
// Parsers and consumers should treat a zero SourcePos as "position unknown"
// and avoid emitting misleading range data for it.
func (p SourcePos) IsZero() bool {
	return p.File == "" && p.Line == 0 && p.Col == 0
}

// Node is the abstract representation of a single step in a workflow.
type Node struct {
	// ID is the unique identifier of the node within a WorkflowGraph.
	ID string `json:"id"`
	// Name is the human-readable label.
	Name string `json:"name"`
	// Type classifies the node's role in the workflow.
	Type NodeType `json:"type"`
	// Config holds framework-specific settings (model name, max_iterations, etc.).
	Config map[string]any `json:"config,omitempty"`
	// Pos is an optional source location for the node.
	// Position-aware parsers (adk-go AST) fill this automatically; JSON-based
	// parsers preserve the "pos" field from input if present. Consumers should
	// use SourcePos.IsZero to gate position-aware behavior.
	Pos SourcePos `json:"pos,omitempty"`
	// HasExitBranch is set by parsers when the source code structurally
	// declares an exit from this node — e.g. a LangGraph
	// `add_edge(node, END)`, a `Literal[END, …]` router return, or a
	// `Command(goto=END)` body return. The flag carries
	// framework-agnostic meaning ("the framework will exit the workflow
	// from here under at least one branch") so cycle_detection and
	// similar rules can downgrade structural cycles whose only exit is
	// via a sentinel. Typed field rather than a Config map key so domain
	// rules don't depend on a parser-private string contract.
	HasExitBranch bool `json:"has_exit_branch,omitempty"`
}

// Edge represents a directed connection between two nodes.
type Edge struct {
	// From is the ID of the source node.
	From string `json:"from"`
	// To is the ID of the destination node.
	To string `json:"to"`
	// Condition is an optional expression that must evaluate to true for this
	// edge to be traversed. Empty string means unconditional.
	Condition string `json:"condition,omitempty"`
}

// WorkflowGraph is the framework-agnostic representation of an agent workflow.
// Parsers in the infrastructure layer convert framework-specific definitions
// (ADK-Go structs, n8n JSON, etc.) into this canonical form.
type WorkflowGraph struct {
	// Nodes is the set of all nodes keyed by their ID.
	Nodes map[string]*Node `json:"nodes"`
	// Edges is the ordered list of directed edges.
	Edges []Edge `json:"edges"`
	// EntryNodeID is the ID of the node where execution begins.
	EntryNodeID string `json:"entry_node_id"`
}

// workflowGraphJSON is the intermediate type used when unmarshalling a
// WorkflowGraph from JSON. The Nodes field is stored as an array in JSON
// to make hand-authoring testdata easier, but the domain type uses a map.
type workflowGraphJSON struct {
	Nodes       []*Node `json:"nodes"`
	Edges       []Edge  `json:"edges"`
	EntryNodeID string  `json:"entry_node_id"`
}

// UnmarshalJSON deserializes a WorkflowGraph from JSON. The JSON format stores
// nodes as an array; this method converts them to the map[string]*Node form
// used internally by the domain layer.
func (g *WorkflowGraph) UnmarshalJSON(data []byte) error {
	var raw workflowGraphJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal WorkflowGraph: %w", err)
	}

	g.Edges = raw.Edges
	g.EntryNodeID = raw.EntryNodeID
	g.Nodes = make(map[string]*Node, len(raw.Nodes))
	for _, n := range raw.Nodes {
		if n == nil {
			continue
		}
		g.Nodes[n.ID] = n
	}
	return nil
}

// GetNode returns the node with the given ID and whether it was found.
func (g *WorkflowGraph) GetNode(id string) (*Node, bool) {
	n, ok := g.Nodes[id]
	return n, ok
}

// OutgoingEdges returns all edges whose source is nodeID.
func (g *WorkflowGraph) OutgoingEdges(nodeID string) []Edge {
	var out []Edge
	for _, e := range g.Edges {
		if e.From == nodeID {
			out = append(out, e)
		}
	}
	return out
}

// IncomingEdges returns all edges whose destination is nodeID.
func (g *WorkflowGraph) IncomingEdges(nodeID string) []Edge {
	var in []Edge
	for _, e := range g.Edges {
		if e.To == nodeID {
			in = append(in, e)
		}
	}
	return in
}
