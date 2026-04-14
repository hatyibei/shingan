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
