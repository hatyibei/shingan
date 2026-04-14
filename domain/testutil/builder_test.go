package testutil_test

import (
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/testutil"
)

func TestBuilder_SimpleLinearGraph(t *testing.T) {
	g, err := testutil.NewBuilder().
		AddNode("a", domain.NodeTypeLLM).
		AddNode("b", domain.NodeTypeTool).
		AddEdge("a", "b").
		Entry("a").
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(g.Nodes))
	}
	if len(g.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(g.Edges))
	}
	if g.EntryNodeID != "a" {
		t.Errorf("expected entry node 'a', got %q", g.EntryNodeID)
	}
}

func TestBuilder_ConditionalEdge(t *testing.T) {
	g, err := testutil.NewBuilder().
		AddNode("start", domain.NodeTypeCondition).
		AddNode("yes", domain.NodeTypeLLM).
		AddNode("no", domain.NodeTypeOutput).
		AddConditionalEdge("start", "yes", "result == true").
		AddConditionalEdge("start", "no", "result == false").
		Entry("start").
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.Edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(g.Edges))
	}
	edges := g.OutgoingEdges("start")
	if len(edges) != 2 {
		t.Errorf("expected 2 outgoing edges from 'start', got %d", len(edges))
	}
}

func TestBuilder_NodeWithConfig(t *testing.T) {
	cfg := map[string]any{"model": "gpt-4o", "max_tokens": 1000}
	g, err := testutil.NewBuilder().
		AddNodeWithConfig("llm", domain.NodeTypeLLM, cfg).
		Entry("llm").
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	n, ok := g.GetNode("llm")
	if !ok {
		t.Fatal("node 'llm' not found")
	}
	if n.Config["model"] != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %v", n.Config["model"])
	}
}

func TestBuilder_LoopGraph(t *testing.T) {
	// Simulate a loop: start -> work -> check -> work (cycle via conditional back-edge)
	g, err := testutil.NewBuilder().
		AddLoopNode("start", 10).
		AddNode("work", domain.NodeTypeTool).
		AddConditionNode("check", "not_done").
		AddNode("done", domain.NodeTypeOutput).
		AddEdge("start", "work").
		AddConditionalEdge("check", "work", "not_done").
		AddConditionalEdge("check", "done", "done").
		AddEdge("work", "check").
		Entry("start").
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.Nodes) != 4 {
		t.Errorf("expected 4 nodes, got %d", len(g.Nodes))
	}
	// work has 2 incoming edges (start -> work, check -> work) and 1 outgoing (work -> check)
	in := g.IncomingEdges("work")
	out := g.OutgoingEdges("work")
	if len(in) != 2 {
		t.Errorf("expected 2 incoming edges to 'work', got %d", len(in))
	}
	if len(out) != 1 {
		t.Errorf("expected 1 outgoing edge from 'work', got %d", len(out))
	}
}

func TestBuilder_AddLoopNode(t *testing.T) {
	g, err := testutil.NewBuilder().
		AddLoopNode("retry", 5).
		AddNode("work", domain.NodeTypeLLM).
		AddEdge("retry", "work").
		Entry("retry").
		Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	n, ok := g.GetNode("retry")
	if !ok {
		t.Fatal("node 'retry' not found")
	}
	if n.Type != domain.NodeTypeLoop {
		t.Errorf("expected NodeTypeLoop, got %v", n.Type)
	}
	if n.Config["max_iterations"] != 5 {
		t.Errorf("expected max_iterations=5, got %v", n.Config["max_iterations"])
	}
}

func TestBuilder_AddConditionNode(t *testing.T) {
	g, err := testutil.NewBuilder().
		AddConditionNode("branch", "err != nil").
		AddNode("success", domain.NodeTypeOutput).
		AddNode("failure", domain.NodeTypeOutput).
		AddConditionalEdge("branch", "success", "err == nil").
		AddConditionalEdge("branch", "failure", "err != nil").
		Entry("branch").
		Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	n, ok := g.GetNode("branch")
	if !ok {
		t.Fatal("node 'branch' not found")
	}
	if n.Type != domain.NodeTypeCondition {
		t.Errorf("expected NodeTypeCondition, got %v", n.Type)
	}
	if n.Config["expression"] != "err != nil" {
		t.Errorf("expected expression='err != nil', got %v", n.Config["expression"])
	}
}

func TestBuilder_ErrorOnDuplicateNode(t *testing.T) {
	_, err := testutil.NewBuilder().
		AddNode("a", domain.NodeTypeLLM).
		AddNode("a", domain.NodeTypeTool). // duplicate
		Entry("a").
		Build()

	if err == nil {
		t.Error("expected error for duplicate node ID, got nil")
	}
}

func TestBuilder_ErrorOnUnknownEdgeNode(t *testing.T) {
	_, err := testutil.NewBuilder().
		AddNode("a", domain.NodeTypeLLM).
		AddEdge("a", "nonexistent").
		Entry("a").
		Build()

	if err == nil {
		t.Error("expected error for edge referencing unknown node, got nil")
	}
}

func TestBuilder_ErrorOnMissingEntry(t *testing.T) {
	_, err := testutil.NewBuilder().
		AddNode("a", domain.NodeTypeLLM).
		Build() // no Entry() call

	if err == nil {
		t.Error("expected error when entry node is not set, got nil")
	}
}

func TestBuilder_ErrorOnUnregisteredEntry(t *testing.T) {
	_, err := testutil.NewBuilder().
		AddNode("a", domain.NodeTypeLLM).
		Entry("z"). // z not registered
		Build()

	if err == nil {
		t.Error("expected error when entry node is not registered, got nil")
	}
}
