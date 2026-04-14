package domain_test

import (
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/testutil"
)

func TestWorkflowGraph_GetNode(t *testing.T) {
	g, err := testutil.NewBuilder().
		AddNode("a", domain.NodeTypeLLM).
		AddNode("b", domain.NodeTypeOutput).
		Entry("a").
		Build()
	if err != nil {
		t.Fatalf("build error: %v", err)
	}

	n, ok := g.GetNode("a")
	if !ok {
		t.Fatal("expected node 'a' to exist")
	}
	if n.Type != domain.NodeTypeLLM {
		t.Errorf("expected NodeTypeLLM, got %v", n.Type)
	}

	_, ok = g.GetNode("nonexistent")
	if ok {
		t.Error("expected node 'nonexistent' to not exist")
	}
}

func TestWorkflowGraph_OutgoingEdges(t *testing.T) {
	g, err := testutil.NewBuilder().
		AddNode("root", domain.NodeTypeControl).
		AddNode("left", domain.NodeTypeLLM).
		AddNode("right", domain.NodeTypeTool).
		AddConditionalEdge("root", "left", "x > 0").
		AddConditionalEdge("root", "right", "x <= 0").
		Entry("root").
		Build()
	if err != nil {
		t.Fatalf("build error: %v", err)
	}

	out := g.OutgoingEdges("root")
	if len(out) != 2 {
		t.Errorf("expected 2 outgoing edges from 'root', got %d", len(out))
	}

	out = g.OutgoingEdges("left")
	if len(out) != 0 {
		t.Errorf("expected 0 outgoing edges from 'left', got %d", len(out))
	}
}

func TestWorkflowGraph_IncomingEdges(t *testing.T) {
	g, err := testutil.NewBuilder().
		AddNode("a", domain.NodeTypeLLM).
		AddNode("b", domain.NodeTypeLLM).
		AddNode("c", domain.NodeTypeOutput).
		AddEdge("a", "c").
		AddEdge("b", "c").
		Entry("a").
		Build()
	if err != nil {
		t.Fatalf("build error: %v", err)
	}

	in := g.IncomingEdges("c")
	if len(in) != 2 {
		t.Errorf("expected 2 incoming edges to 'c', got %d", len(in))
	}

	in = g.IncomingEdges("a")
	if len(in) != 0 {
		t.Errorf("expected 0 incoming edges to 'a', got %d", len(in))
	}
}

func TestWorkflowGraph_NoEdges(t *testing.T) {
	g, err := testutil.NewBuilder().
		AddNode("solo", domain.NodeTypeOutput).
		Entry("solo").
		Build()
	if err != nil {
		t.Fatalf("build error: %v", err)
	}

	if len(g.OutgoingEdges("solo")) != 0 {
		t.Error("expected no outgoing edges from isolated node")
	}
	if len(g.IncomingEdges("solo")) != 0 {
		t.Error("expected no incoming edges to isolated node")
	}
}

func TestSeverity_Order(t *testing.T) {
	if domain.Info >= domain.Warning {
		t.Error("Info should be less than Warning")
	}
	if domain.Warning >= domain.Critical {
		t.Error("Warning should be less than Critical")
	}
}

func TestSeverity_String(t *testing.T) {
	cases := []struct {
		s    domain.Severity
		want string
	}{
		{domain.Info, "info"},
		{domain.Warning, "warning"},
		{domain.Critical, "critical"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("Severity(%d).String() = %q, want %q", c.s, got, c.want)
		}
	}
}

func TestNodeType_Values(t *testing.T) {
	// Verify iota ordering doesn't change accidentally.
	types := []domain.NodeType{
		domain.NodeTypeLLM,
		domain.NodeTypeTool,
		domain.NodeTypeControl,
		domain.NodeTypeHuman,
		domain.NodeTypeOutput,
	}
	for i, nt := range types {
		if int(nt) != i {
			t.Errorf("NodeType[%d] has value %d, expected %d", i, nt, i)
		}
	}
}
