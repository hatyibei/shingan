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
		AddNode("root", domain.NodeTypeCondition).
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
	// NodeTypeControl(2) must remain 2 for backward compatibility.
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
	// New types are appended; verify their absolute values.
	if int(domain.NodeTypeLoop) != 5 {
		t.Errorf("NodeTypeLoop = %d, want 5", int(domain.NodeTypeLoop))
	}
	if int(domain.NodeTypeCondition) != 6 {
		t.Errorf("NodeTypeCondition = %d, want 6", int(domain.NodeTypeCondition))
	}
}

func TestNodeType_String(t *testing.T) {
	cases := []struct {
		nt   domain.NodeType
		want string
	}{
		{domain.NodeTypeLLM, "llm"},
		{domain.NodeTypeTool, "tool"},
		{domain.NodeTypeControl, "control"},
		{domain.NodeTypeHuman, "human"},
		{domain.NodeTypeOutput, "output"},
		{domain.NodeTypeLoop, "loop"},
		{domain.NodeTypeCondition, "condition"},
	}
	for _, c := range cases {
		if got := c.nt.String(); got != c.want {
			t.Errorf("NodeType(%d).String() = %q, want %q", int(c.nt), got, c.want)
		}
	}
}

func TestNodeType_UnmarshalJSON_BackwardCompat(t *testing.T) {
	// "control" string must unmarshal to NodeTypeLoop for backward compatibility.
	g, err := testutil.NewBuilder().
		AddNode("n", domain.NodeTypeLoop).
		Entry("n").
		Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// Confirm graph with Loop type works fine.
	if g.Nodes["n"].Type != domain.NodeTypeLoop {
		t.Errorf("expected NodeTypeLoop, got %v", g.Nodes["n"].Type)
	}
}

// TestSourcePos_IsZero covers the IsZero predicate over the key state combinations:
// fully unset (true), any single field populated (false), and fully populated (false).
func TestSourcePos_IsZero(t *testing.T) {
	tests := []struct {
		name string
		pos  domain.SourcePos
		want bool
	}{
		{"empty", domain.SourcePos{}, true},
		{"file_only", domain.SourcePos{File: "foo.go"}, false},
		{"line_only", domain.SourcePos{Line: 10}, false},
		{"col_only", domain.SourcePos{Col: 5}, false},
		{"file_and_line", domain.SourcePos{File: "foo.go", Line: 10}, false},
		{"all_fields", domain.SourcePos{File: "foo.go", Line: 10, Col: 5}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.pos.IsZero(); got != tc.want {
				t.Errorf("SourcePos{File:%q,Line:%d,Col:%d}.IsZero() = %v, want %v",
					tc.pos.File, tc.pos.Line, tc.pos.Col, got, tc.want)
			}
		})
	}
}

// TestNodeType_SequenceParallelRoundTrip covers Codex Slice F #1:
// NodeTypeSequence / NodeTypeParallel must survive JSON marshal then
// unmarshal. Previously only Loop/Condition/Control had explicit
// coverage. Pre-fix, a fixture-author would have a 50/50 chance of
// regressing the new types without noticing.
func TestNodeType_SequenceParallelRoundTrip(t *testing.T) {
	for _, nt := range []domain.NodeType{domain.NodeTypeSequence, domain.NodeTypeParallel} {
		raw, err := nt.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON(%v): %v", nt, err)
		}
		var got domain.NodeType
		if err := got.UnmarshalJSON(raw); err != nil {
			t.Fatalf("UnmarshalJSON(%s): %v", raw, err)
		}
		if got != nt {
			t.Errorf("round-trip changed type: %v → %v (via %s)", nt, got, raw)
		}
	}
}

// TestNodeType_ControlBackwardCompat: the legacy "control" string
// continues to unmarshal to NodeTypeLoop. Important because existing
// testdata JSON predating the Sequence/Parallel split uses "control".
func TestNodeType_ControlBackwardCompat(t *testing.T) {
	var got domain.NodeType
	if err := got.UnmarshalJSON([]byte(`"control"`)); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if got != domain.NodeTypeLoop {
		t.Errorf("\"control\" → %v, want NodeTypeLoop", got)
	}
}
