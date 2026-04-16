package parser_test

import (
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/infrastructure/parser"
)

func TestJSONParser_SupportedFormat(t *testing.T) {
	p := parser.NewJSONParser()
	if got := p.SupportedFormat(); got != "json" {
		t.Errorf("SupportedFormat() = %q, want %q", got, "json")
	}
}

func TestJSONParser_ValidGraph(t *testing.T) {
	input := []byte(`{
		"nodes": [
			{"id": "a", "name": "NodeA", "type": "llm"},
			{"id": "b", "name": "NodeB", "type": "tool"}
		],
		"edges": [{"from": "a", "to": "b"}],
		"entry_node_id": "a"
	}`)

	p := parser.NewJSONParser()
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() returned unexpected error: %v", err)
	}
	if graph == nil {
		t.Fatal("Parse() returned nil graph")
	}
	if graph.EntryNodeID != "a" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "a")
	}
	if len(graph.Nodes) != 2 {
		t.Errorf("len(Nodes) = %d, want 2", len(graph.Nodes))
	}
	if len(graph.Edges) != 1 {
		t.Errorf("len(Edges) = %d, want 1", len(graph.Edges))
	}
	if graph.Nodes["a"] == nil {
		t.Error("node 'a' not found in graph")
	}
}

func TestJSONParser_NodeTypeStringDeserialization(t *testing.T) {
	input := []byte(`{
		"nodes": [{"id": "n1", "name": "LLMNode", "type": "llm"}],
		"edges": [],
		"entry_node_id": "n1"
	}`)

	p := parser.NewJSONParser()
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() returned unexpected error: %v", err)
	}
	node := graph.Nodes["n1"]
	if node == nil {
		t.Fatal("node 'n1' not found")
	}
	if node.Type != domain.NodeTypeLLM {
		t.Errorf("node.Type = %v, want NodeTypeLLM", node.Type)
	}
}

func TestJSONParser_InvalidJSON(t *testing.T) {
	p := parser.NewJSONParser()
	_, err := p.Parse([]byte(`{not valid json`))
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestJSONParser_EmptyInput(t *testing.T) {
	p := parser.NewJSONParser()
	_, err := p.Parse([]byte(``))
	if err == nil {
		t.Error("expected error for empty input, got nil")
	}
}

// TestJSONParser_PreservesSourcePos verifies that a Node's "pos" field is
// round-tripped from the JSON input into domain.Node.Pos unchanged.
// This is Phase 2 Parser contract: position-aware consumers (LSP) may feed
// JSON graphs containing source positions exported from external tools.
func TestJSONParser_PreservesSourcePos(t *testing.T) {
	input := []byte(`{
		"nodes": [
			{"id": "a", "name": "NodeA", "type": "llm", "pos": {"file": "agent.py", "line": 42, "col": 7}},
			{"id": "b", "name": "NodeB", "type": "tool"}
		],
		"edges": [{"from": "a", "to": "b"}],
		"entry_node_id": "a"
	}`)

	p := parser.NewJSONParser()
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	nodeA := graph.Nodes["a"]
	if nodeA == nil {
		t.Fatal("node 'a' not found")
	}
	if nodeA.Pos.IsZero() {
		t.Fatalf("node 'a' Pos is zero; expected preserved input position")
	}
	wantA := domain.SourcePos{File: "agent.py", Line: 42, Col: 7}
	if nodeA.Pos != wantA {
		t.Errorf("node 'a' Pos = %+v, want %+v", nodeA.Pos, wantA)
	}

	// Node 'b' omitted the "pos" field; it must remain zero (backward-compat).
	nodeB := graph.Nodes["b"]
	if nodeB == nil {
		t.Fatal("node 'b' not found")
	}
	if !nodeB.Pos.IsZero() {
		t.Errorf("node 'b' Pos = %+v, want zero (field omitted in input)", nodeB.Pos)
	}
}

// TestJSONParser_NoPosField_BackwardCompat verifies that input JSON without
// any "pos" field — the v0.5.0 format — still parses cleanly with zero Pos
// on every node.
func TestJSONParser_NoPosField_BackwardCompat(t *testing.T) {
	// Legacy format — no "pos" on any node.
	input := []byte(`{
		"nodes": [
			{"id": "a", "name": "NodeA", "type": "llm"},
			{"id": "b", "name": "NodeB", "type": "output"}
		],
		"edges": [{"from": "a", "to": "b"}],
		"entry_node_id": "a"
	}`)

	p := parser.NewJSONParser()
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	for _, id := range []string{"a", "b"} {
		n := graph.Nodes[id]
		if n == nil {
			t.Fatalf("node %q not found", id)
		}
		if !n.Pos.IsZero() {
			t.Errorf("node %q Pos = %+v, want zero", id, n.Pos)
		}
	}
}
