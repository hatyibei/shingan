// Package testutil provides helpers for constructing WorkflowGraph instances
// in tests without requiring a real framework parser.
//
// Example usage:
//
//	graph, err := testutil.NewBuilder().
//	    AddNode("a", domain.NodeTypeLLM).
//	    AddNode("b", domain.NodeTypeTool).
//	    AddEdge("a", "b").
//	    Entry("a").
//	    Build()
package testutil

import (
	"fmt"

	"github.com/hatyibei/shingan/domain"
)

// Builder assembles a WorkflowGraph using a fluent API.
// All methods except Build return the same *Builder to allow chaining.
type Builder struct {
	nodes       map[string]*domain.Node
	edges       []domain.Edge
	entryNodeID string
	errs        []error
}

// NewBuilder returns an empty Builder.
func NewBuilder() *Builder {
	return &Builder{
		nodes: make(map[string]*domain.Node),
	}
}

// AddNode registers a node with the given ID and type.
// The node's Name is set to the same value as ID for convenience.
// If a node with the same ID already exists, an error is recorded.
func (b *Builder) AddNode(id string, nodeType domain.NodeType) *Builder {
	if _, exists := b.nodes[id]; exists {
		b.errs = append(b.errs, fmt.Errorf("duplicate node ID %q", id))
		return b
	}
	b.nodes[id] = &domain.Node{
		ID:     id,
		Name:   id,
		Type:   nodeType,
		Config: make(map[string]any),
	}
	return b
}

// AddNodeWithConfig registers a node with additional framework-specific config.
func (b *Builder) AddNodeWithConfig(id string, nodeType domain.NodeType, config map[string]any) *Builder {
	b.AddNode(id, nodeType)
	if n, ok := b.nodes[id]; ok {
		n.Config = config
	}
	return b
}

// AddLoopNode registers a Loop node with a max_iterations config value.
func (b *Builder) AddLoopNode(id string, maxIter int) *Builder {
	return b.AddNodeWithConfig(id, domain.NodeTypeLoop, map[string]any{"max_iterations": maxIter})
}

// AddExitNode registers a node and flags it as having a structural exit
// branch (e.g. a parser-detected `add_edge(node, END)` or
// `Literal[END, …]` router return). Used by cycle_detection tests to
// simulate bounded-cycle paths without the parser.
func (b *Builder) AddExitNode(id string, nodeType domain.NodeType) *Builder {
	b.AddNode(id, nodeType)
	if n, ok := b.nodes[id]; ok {
		n.HasExitBranch = true
	}
	return b
}

// AddConditionNode registers a Condition node with an expression config value.
func (b *Builder) AddConditionNode(id string, expression string) *Builder {
	return b.AddNodeWithConfig(id, domain.NodeTypeCondition, map[string]any{"expression": expression})
}

// AddEdge adds a directed unconditional edge from → to.
// If either node has not been registered, an error is recorded.
func (b *Builder) AddEdge(from, to string) *Builder {
	return b.AddConditionalEdge(from, to, "")
}

// AddConditionalEdge adds a directed edge from → to with an optional condition.
// An empty condition string means the edge is unconditional.
func (b *Builder) AddConditionalEdge(from, to, condition string) *Builder {
	if _, ok := b.nodes[from]; !ok {
		b.errs = append(b.errs, fmt.Errorf("edge references unknown source node %q", from))
	}
	if _, ok := b.nodes[to]; !ok {
		b.errs = append(b.errs, fmt.Errorf("edge references unknown destination node %q", to))
	}
	b.edges = append(b.edges, domain.Edge{From: from, To: to, Condition: condition})
	return b
}

// Entry sets the entry node of the graph.
// If the node has not been registered, an error is recorded at Build time.
func (b *Builder) Entry(nodeID string) *Builder {
	b.entryNodeID = nodeID
	return b
}

// Build validates the accumulated state and returns the constructed WorkflowGraph.
// Returns an error if:
//   - any AddNode / AddEdge call recorded an error
//   - no entry node was set
//   - the entry node ID does not correspond to a registered node
func (b *Builder) Build() (*domain.WorkflowGraph, error) {
	if len(b.errs) > 0 {
		return nil, b.errs[0]
	}
	if b.entryNodeID == "" {
		return nil, fmt.Errorf("entry node must be set via Entry()")
	}
	if _, ok := b.nodes[b.entryNodeID]; !ok {
		return nil, fmt.Errorf("entry node %q is not registered", b.entryNodeID)
	}
	return &domain.WorkflowGraph{
		Nodes:       b.nodes,
		Edges:       b.edges,
		EntryNodeID: b.entryNodeID,
	}, nil
}
