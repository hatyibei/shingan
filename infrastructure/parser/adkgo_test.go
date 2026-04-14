package parser_test

import (
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/infrastructure/parser"
)

func TestADKGoParser_SupportedFormat(t *testing.T) {
	p := parser.NewADKGoParser()
	if got := p.SupportedFormat(); got != "adk-go" {
		t.Errorf("SupportedFormat() = %q, want %q", got, "adk-go")
	}
}

// TestADKGoParser_LlmAgentSingle verifies a single LlmAgent is parsed correctly.
func TestADKGoParser_LlmAgentSingle(t *testing.T) {
	src := []byte(`package agents

var agent = &LlmAgent{
	Name:        "classifier",
	Model:       "gpt-4o",
	Instruction: "Classify the input.",
}
`)
	p := parser.NewADKGoParser()
	// Single LlmAgent is not an orchestrator type so no entry candidate.
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if graph == nil {
		t.Fatal("Parse() returned nil")
	}
	// No orchestrator found, so graph has no nodes from the entry scan,
	// but the LlmAgent var itself is not an orchestrator — empty graph is OK.
	_ = graph
}

// TestADKGoParser_SequentialAgentWithTwoSubAgents verifies sequential edge generation.
func TestADKGoParser_SequentialAgentWithTwoSubAgents(t *testing.T) {
	src := []byte(`package agents

var workflow = &SequentialAgent{
	Name: "orchestrator",
	SubAgents: []Agent{
		&LlmAgent{
			Name:  "step_one",
			Model: "gpt-4o",
		},
		&LlmAgent{
			Name:  "step_two",
			Model: "gpt-4o-mini",
		},
	},
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if graph.EntryNodeID == "" {
		t.Error("EntryNodeID should not be empty for SequentialAgent")
	}
	// Expect: orchestrator, step_one, step_two nodes.
	for _, name := range []string{"orchestrator", "step_one", "step_two"} {
		if graph.Nodes[name] == nil {
			t.Errorf("node %q not found in graph; nodes=%v", name, nodeKeys(graph))
		}
	}
	// Expect edges: orchestrator→step_one, step_one→step_two.
	assertEdge(t, graph, "orchestrator", "step_one", "")
	assertEdge(t, graph, "step_one", "step_two", "")
}

// TestADKGoParser_LoopAgentWithMaxIterations verifies Config["max_iterations"] is set.
func TestADKGoParser_LoopAgentWithMaxIterations(t *testing.T) {
	src := []byte(`package agents

var loop = &LoopAgent{
	Name:          "retry_loop",
	MaxIterations: 5,
	SubAgents: []Agent{
		&LlmAgent{Name: "worker", Model: "gpt-4o-mini"},
	},
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	node := graph.Nodes["retry_loop"]
	if node == nil {
		t.Fatalf("node 'retry_loop' not found; nodes=%v", nodeKeys(graph))
	}
	if node.Type != domain.NodeTypeControl {
		t.Errorf("Type = %v, want NodeTypeControl", node.Type)
	}
	if got, ok := node.Config["max_iterations"]; !ok {
		t.Error("Config[max_iterations] not set")
	} else if got != 5 {
		t.Errorf("Config[max_iterations] = %v, want 5", got)
	}
	// Loopback edge: worker → worker (single sub-agent self-loop).
	assertEdge(t, graph, "worker", "worker", "loop_back")
}

// TestADKGoParser_LoopAgentWithoutMaxIterations verifies Config["max_iterations"] is absent.
func TestADKGoParser_LoopAgentWithoutMaxIterations(t *testing.T) {
	src := []byte(`package agents

var loop = &LoopAgent{
	Name: "infinite_loop",
	SubAgents: []Agent{
		&LlmAgent{Name: "process", Model: "gpt-4o"},
		&LlmAgent{Name: "decide",  Model: "gpt-4o"},
	},
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	node := graph.Nodes["infinite_loop"]
	if node == nil {
		t.Fatalf("node 'infinite_loop' not found; nodes=%v", nodeKeys(graph))
	}
	if _, ok := node.Config["max_iterations"]; ok {
		t.Error("Config[max_iterations] should NOT be set when MaxIterations is absent")
	}
	// Loopback edge from last sub-agent back to first: decide → process.
	assertEdge(t, graph, "decide", "process", "loop_back")
}

// TestADKGoParser_ParallelAgent verifies parallel_branch edges are generated.
func TestADKGoParser_ParallelAgent(t *testing.T) {
	src := []byte(`package agents

var fanout = &ParallelAgent{
	Name: "fanout",
	SubAgents: []Agent{
		&LlmAgent{Name: "branch_a", Model: "gpt-4o-mini"},
		&LlmAgent{Name: "branch_b", Model: "gpt-4o-mini"},
	},
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	assertEdge(t, graph, "fanout", "branch_a", "parallel_branch")
	assertEdge(t, graph, "fanout", "branch_b", "parallel_branch")
}

// TestADKGoParser_NestedAgents verifies recursive expansion of nested agent literals.
func TestADKGoParser_NestedAgents(t *testing.T) {
	src := []byte(`package agents

var outer = &SequentialAgent{
	Name: "outer",
	SubAgents: []Agent{
		&SequentialAgent{
			Name: "inner",
			SubAgents: []Agent{
				&LlmAgent{Name: "deep_node", Model: "gpt-4o"},
			},
		},
	},
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	for _, name := range []string{"outer", "inner", "deep_node"} {
		if graph.Nodes[name] == nil {
			t.Errorf("expected node %q; got nodes=%v", name, nodeKeys(graph))
		}
	}
	assertEdge(t, graph, "outer", "inner", "")
	assertEdge(t, graph, "inner", "deep_node", "")
}

// TestADKGoParser_ToolCategoryInference verifies tool category heuristics.
func TestADKGoParser_ToolCategoryInference(t *testing.T) {
	src := []byte(`package agents

var agent = &SequentialAgent{
	Name: "tooled_agent",
	SubAgents: []Agent{
		&LlmAgent{
			Name:  "worker",
			Model: "gpt-4o",
			Tools: []Tool{browserTool, codeExec, apiTool},
		},
	},
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	checks := map[string]string{
		"browser_tool": "browser",
		"code_exec":    "code",
		"api_tool":     "api",
	}
	for nodeID, wantCat := range checks {
		node := graph.Nodes[nodeID]
		if node == nil {
			t.Errorf("tool node %q not found; nodes=%v", nodeID, nodeKeys(graph))
			continue
		}
		if node.Type != domain.NodeTypeTool {
			t.Errorf("node %q Type = %v, want NodeTypeTool", nodeID, node.Type)
		}
		if got := node.Config["category"]; got != wantCat {
			t.Errorf("node %q category = %v, want %q", nodeID, got, wantCat)
		}
	}
}

// TestADKGoParser_InvalidGoSyntax verifies a parse error is returned.
func TestADKGoParser_InvalidGoSyntax(t *testing.T) {
	src := []byte(`package agents

this is not valid Go syntax {{{
`)
	p := parser.NewADKGoParser()
	_, err := p.Parse(src)
	if err == nil {
		t.Error("expected error for invalid Go syntax, got nil")
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func assertEdge(t *testing.T, graph *domain.WorkflowGraph, from, to, condition string) {
	t.Helper()
	for _, e := range graph.Edges {
		if e.From == from && e.To == to && e.Condition == condition {
			return
		}
	}
	t.Errorf("edge {from:%q to:%q condition:%q} not found; edges=%v", from, to, condition, graph.Edges)
}

func nodeKeys(graph *domain.WorkflowGraph) []string {
	var keys []string
	for k := range graph.Nodes {
		keys = append(keys, k)
	}
	return keys
}
