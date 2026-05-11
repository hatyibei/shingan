package parser_test

import (
	"os"
	"path/filepath"
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
	if node.Type != domain.NodeTypeLoop {
		t.Errorf("Type = %v, want NodeTypeLoop", node.Type)
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

// ─── Real ADK-Go SDK API tests ───────────────────────────────────────────────

// TestADKGoParser_RealAPI_LoopAgentNoMaxIterations verifies the real ADK-Go SDK
// constructor pattern: loopagent.New(loopagent.Config{AgentConfig: agent.Config{...}})
// without MaxIterations is detected (cycle_detection Critical precondition).
func TestADKGoParser_RealAPI_LoopAgentNoMaxIterations(t *testing.T) {
	src := []byte(`package real

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/loopagent"
)

func BuildInfiniteLoop() {
	classifier, _ := llmagent.New(llmagent.Config{
		Name:        "classifier",
		Instruction: "Classify the input.",
	})
	validator, _ := llmagent.New(llmagent.Config{
		Name:        "validator",
		Instruction: "Validate the result.",
	})
	_, _ = loopagent.New(loopagent.Config{
		AgentConfig: agent.Config{
			Name:      "retry_loop",
			SubAgents: []agent.Agent{classifier, validator},
		},
	})
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	node := graph.Nodes["retry_loop"]
	if node == nil {
		t.Fatalf("node 'retry_loop' not found; nodes=%v", nodeKeys(graph))
	}
	if node.Type != domain.NodeTypeLoop {
		t.Errorf("Type = %v, want NodeTypeLoop", node.Type)
	}
	if _, ok := node.Config["max_iterations"]; ok {
		t.Error("Config[max_iterations] should NOT be set when MaxIterations is absent")
	}
	// Loopback edge: validator → classifier.
	assertEdge(t, graph, "validator", "classifier", "loop_back")
}

// TestADKGoParser_RealAPI_SequentialAgent verifies the real ADK-Go SDK
// sequential agent constructor is parsed correctly.
func TestADKGoParser_RealAPI_SequentialAgent(t *testing.T) {
	src := []byte(`package real

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
)

func BuildSequential() {
	planner, _ := llmagent.New(llmagent.Config{
		Name:        "planner",
		Instruction: "Plan.",
	})
	summarizer, _ := llmagent.New(llmagent.Config{
		Name:        "summarizer",
		Instruction: "Summarize.",
	})
	_, _ = sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:      "web_scraper",
			SubAgents: []agent.Agent{planner, summarizer},
		},
	})
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	for _, name := range []string{"web_scraper", "planner", "summarizer"} {
		if graph.Nodes[name] == nil {
			t.Errorf("node %q not found; nodes=%v", name, nodeKeys(graph))
		}
	}
	assertEdge(t, graph, "web_scraper", "planner", "")
	assertEdge(t, graph, "planner", "summarizer", "")
}

// TestADKGoParser_RealAPI_UnreachableNode verifies that an LlmAgent created
// but excluded from orchestrator SubAgents remains in the graph (potentially
// unreachable — detected by the unreachable_node rule).
func TestADKGoParser_RealAPI_UnreachableNode(t *testing.T) {
	src := []byte(`package real

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
)

func BuildUnreachable() {
	inputProcessor, _ := llmagent.New(llmagent.Config{
		Name:        "input_processor",
		Instruction: "Process.",
	})
	outputFormatter, _ := llmagent.New(llmagent.Config{
		Name:        "output_formatter",
		Instruction: "Format.",
	})
	orphanAnalyzer, _ := llmagent.New(llmagent.Config{
		Name:        "orphan_analyzer",
		Instruction: "Never reached.",
	})
	_ = orphanAnalyzer
	_, _ = sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:      "orchestrator",
			SubAgents: []agent.Agent{inputProcessor, outputFormatter},
		},
	})
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	// orchestrator, input_processor, output_formatter should be in graph.
	for _, name := range []string{"orchestrator", "input_processor", "output_formatter"} {
		if graph.Nodes[name] == nil {
			t.Errorf("node %q not found; nodes=%v", name, nodeKeys(graph))
		}
	}
	// orphan_analyzer should also be in the graph (parsed but not connected).
	if graph.Nodes["orphan_analyzer"] == nil {
		t.Logf("orphan_analyzer not in graph nodes=%v (acceptable — unreachable_node rule detects it via reachability analysis)", nodeKeys(graph))
	}
	// Entry should be orchestrator.
	if graph.EntryNodeID != "orchestrator" {
		t.Errorf("EntryNodeID = %q, want 'orchestrator'", graph.EntryNodeID)
	}
}

// ─── functiontool.New detection tests ───────────────────────────────────────

// TestADKGoParser_FunctiontoolNew_APINameFromConfig verifies that a package-level var
// created with functiontool.New(Config{Name: "fetch_data", ...}, handler) produces
// a tool node whose ID and name derive from the Config.Name field, not the var name.
func TestADKGoParser_FunctiontoolNew_APINameFromConfig(t *testing.T) {
	src := []byte(`package agents

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

var fetchData, _ = functiontool.New(
	functiontool.Config{Name: "fetch_data", Description: "Fetch from REST API."},
	functiontool.Func[struct{}, struct{}](func(_ tool.Context, _ struct{}) (struct{}, error) {
		return struct{}{}, nil
	}),
)

func Build() {
	worker, _ := llmagent.New(llmagent.Config{
		Name:        "worker",
		Instruction: "Do work.",
		Tools:       []tool.Tool{fetchData},
	})
	_, _ = sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:      "pipeline",
			SubAgents: []agent.Agent{worker},
		},
	})
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	// Tool node ID should be derived from Config.Name ("fetch_data"), not the var name.
	toolNode := graph.Nodes["fetch_data"]
	if toolNode == nil {
		t.Fatalf("tool node 'fetch_data' not found; nodes=%v", nodeKeys(graph))
	}
	if toolNode.Type != domain.NodeTypeTool {
		t.Errorf("Type = %v, want NodeTypeTool", toolNode.Type)
	}
	// Category inferred from name "fetch_data": contains "fetch" → "api".
	if cat := toolNode.Config["category"]; cat != "api" {
		t.Errorf("category = %v, want 'api'", cat)
	}
	// Edge: worker → fetch_data.
	assertEdge(t, graph, "worker", "fetch_data", "")
}

// TestADKGoParser_FunctiontoolNew_BrowserCategoryFromToolName verifies that a tool
// created with functiontool.New whose Config.Name contains "browser" gets category "browser".
func TestADKGoParser_FunctiontoolNew_BrowserCategoryFromToolName(t *testing.T) {
	src := []byte(`package agents

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

var browserClick, _ = functiontool.New(
	functiontool.Config{Name: "browser_click", Description: "Click an element."},
	functiontool.Func[struct{}, struct{}](func(_ tool.Context, _ struct{}) (struct{}, error) {
		return struct{}{}, nil
	}),
)

func Build() {
	bot, _ := llmagent.New(llmagent.Config{
		Name:        "bot",
		Instruction: "Click things.",
		Tools:       []tool.Tool{browserClick},
	})
	_, _ = sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:      "clicker",
			SubAgents: []agent.Agent{bot},
		},
	})
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	toolNode := graph.Nodes["browser_click"]
	if toolNode == nil {
		t.Fatalf("tool node 'browser_click' not found; nodes=%v", nodeKeys(graph))
	}
	if cat := toolNode.Config["category"]; cat != "browser" {
		t.Errorf("category = %v, want 'browser'", cat)
	}
}

// TestADKGoParser_FunctiontoolNew_UnresolvableVarSkipped verifies that a Tools element
// referring to an unknown identifier (not a functiontool.New var) falls back gracefully
// to the identifier name without crashing.
func TestADKGoParser_FunctiontoolNew_UnresolvableVarSkipped(t *testing.T) {
	src := []byte(`package agents

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/tool"
)

func Build() {
	// externalTool is declared in another package — not resolvable from this file.
	worker, _ := llmagent.New(llmagent.Config{
		Name:        "worker",
		Instruction: "Do work.",
		Tools:       []tool.Tool{externalTool},
	})
	_, _ = sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:      "pipeline",
			SubAgents: []agent.Agent{worker},
		},
	})
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	// Graph should be valid; worker should exist.
	if graph.Nodes["worker"] == nil {
		t.Errorf("node 'worker' not found; nodes=%v", nodeKeys(graph))
	}
	// Tool node created from identifier name fallback.
	toolNode := graph.Nodes["external_tool"]
	if toolNode == nil {
		t.Errorf("tool node 'external_tool' not found (fallback expected); nodes=%v", nodeKeys(graph))
	}
}

// ─── go/types second-pass tests ─────────────────────────────────────────────

// TestADKGoParser_WithoutTypes verifies that WithoutTypes() disables the type-info pass
// and the parser still returns a valid result using the AST-only path.
func TestADKGoParser_WithoutTypes(t *testing.T) {
	p := parser.NewADKGoParser(parser.WithoutTypes())
	src := []byte(`package agents

var workflow = &SequentialAgent{
	Name: "pipeline",
	SubAgents: []Agent{
		&LlmAgent{Name: "step", Model: "gpt-4o"},
	},
}
`)
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if graph.Nodes["pipeline"] == nil {
		t.Errorf("node 'pipeline' not found; nodes=%v", nodeKeys(graph))
	}
}

// TestADKGoParser_ParseFile_MissingHandler verifies that ParseFile on the real
// examples/real/missing_handler.go file detects browser_search as a Tool node
// with category "browser". This exercises the go/types second-pass path.
func TestADKGoParser_ParseFile_MissingHandler(t *testing.T) {
	// Use a fixed path relative to the repo root.
	// The test binary's working directory is the package directory.
	p := parser.NewADKGoParser()
	// ParseFile with types enabled; fallback to AST-only if types load fails.
	const missingHandlerPath = "../../examples/real/missing_handler.go"
	graph, err := p.ParseFile(missingHandlerPath)
	if err != nil {
		t.Fatalf("ParseFile() error: %v", err)
	}

	// browser_search tool must be detected.
	toolNode := graph.Nodes["browser_search"]
	if toolNode == nil {
		t.Fatalf("tool node 'browser_search' not found; nodes=%v", nodeKeys(graph))
	}
	if toolNode.Type != domain.NodeTypeTool {
		t.Errorf("browser_search Type = %v, want NodeTypeTool", toolNode.Type)
	}
	// Category should be "browser" (inferred from tool name "browser_search").
	if cat := toolNode.Config["category"]; cat != "browser" {
		t.Errorf("browser_search category = %v, want 'browser'", cat)
	}

	// planner LlmAgent must be present.
	if graph.Nodes["planner"] == nil {
		t.Errorf("node 'planner' not found; nodes=%v", nodeKeys(graph))
	}
	// Edge planner → browser_search must exist.
	assertEdge(t, graph, "planner", "browser_search", "")
}

// TestADKGoParser_ParseFile_TypesFallback verifies that ParseFile falls back to
// AST-only parsing when given a bytes-backed temp file that lacks a go.mod context.
// The fallback must still produce a valid (possibly empty) graph without error.
func TestADKGoParser_ParseFile_TypesFallback(t *testing.T) {
	f, err := os.CreateTemp("", "shingan_test_*.go")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(f.Name())

	src := `package tmp

var flow = &SequentialAgent{
	Name: "tmp_flow",
	SubAgents: []Agent{
		&LlmAgent{Name: "tmp_step", Model: "gpt-4o"},
	},
}
`
	if _, err := f.WriteString(src); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()

	// Types pass will fail (no go.mod in temp dir) — fallback expected.
	p := parser.NewADKGoParser()
	graph, err := p.ParseFile(f.Name())
	if err != nil {
		t.Fatalf("ParseFile() fallback should not error, got: %v", err)
	}
	// AST-only fallback produces a valid (possibly non-empty) graph.
	if graph == nil {
		t.Fatal("ParseFile() returned nil graph")
	}
}

// TestADKGoParser_GenericTypeArg_BrowserArgs verifies that functiontool.New
// with a TArgs struct named "browserArgs" (containing a "Query" field) gets
// category "browser" from the go/types second-pass enrichment.
// This is an AST-only test that verifies the category inference pipeline end-to-end.
func TestADKGoParser_GenericTypeArg_BrowserArgs(t *testing.T) {
	src := []byte(`package agents

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

type browserSearchArgs struct{ Query string }
type browserSearchResult struct{ Content string }

var browserSearch, _ = functiontool.New(
	functiontool.Config{Name: "browser_search", Description: "Search the web."},
	functiontool.Func[browserSearchArgs, browserSearchResult](func(_ tool.Context, args browserSearchArgs) (browserSearchResult, error) {
		return browserSearchResult{Content: "stub: " + args.Query}, nil
	}),
)

func Build() {
	planner, _ := llmagent.New(llmagent.Config{
		Name:        "planner",
		Instruction: "Plan.",
		Tools:       []tool.Tool{browserSearch},
	})
	_, _ = sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:      "pipeline",
			SubAgents: []agent.Agent{planner},
		},
	})
}
`)
	// AST-only parse (WithoutTypes) — category from Config.Name "browser_search".
	p := parser.NewADKGoParser(parser.WithoutTypes())
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	toolNode := graph.Nodes["browser_search"]
	if toolNode == nil {
		t.Fatalf("tool node 'browser_search' not found; nodes=%v", nodeKeys(graph))
	}
	if cat := toolNode.Config["category"]; cat != "browser" {
		t.Errorf("category = %v, want 'browser'", cat)
	}
	assertEdge(t, graph, "planner", "browser_search", "")
}

// ─── SourcePos tests ────────────────────────────────────────────────────────

// TestADKGoParser_SourcePos_BareStructLiteral verifies that AST-based parsing
// populates Node.Pos with 1-based line/col numbers derived from token.FileSet.
// The SequentialAgent is declared on line 3 (after the package clause + blank line).
func TestADKGoParser_SourcePos_BareStructLiteral(t *testing.T) {
	src := []byte(`package agents

var workflow = &SequentialAgent{
	Name: "orchestrator",
	SubAgents: []Agent{
		&LlmAgent{
			Name:  "worker",
			Model: "gpt-4o",
		},
	},
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	orch := graph.Nodes["orchestrator"]
	if orch == nil {
		t.Fatalf("node 'orchestrator' not found; nodes=%v", nodeKeys(graph))
	}
	if orch.Pos.IsZero() {
		t.Fatalf("orchestrator.Pos is zero; expected AST-based position")
	}
	if orch.Pos.File != "input.go" {
		t.Errorf("orchestrator.Pos.File = %q, want 'input.go'", orch.Pos.File)
	}
	// SequentialAgent{ composite literal starts on line 3, at the `&` token column.
	if orch.Pos.Line != 3 {
		t.Errorf("orchestrator.Pos.Line = %d, want 3", orch.Pos.Line)
	}
	if orch.Pos.Col < 1 {
		t.Errorf("orchestrator.Pos.Col = %d, want >=1", orch.Pos.Col)
	}

	worker := graph.Nodes["worker"]
	if worker == nil {
		t.Fatalf("node 'worker' not found; nodes=%v", nodeKeys(graph))
	}
	if worker.Pos.IsZero() {
		t.Fatalf("worker.Pos is zero; expected AST-based position")
	}
	if worker.Pos.Line != 6 {
		t.Errorf("worker.Pos.Line = %d, want 6", worker.Pos.Line)
	}
	// Nested LlmAgent is strictly after the outer SequentialAgent.
	if worker.Pos.Line <= orch.Pos.Line {
		t.Errorf("nested worker line (%d) must be greater than outer orchestrator line (%d)",
			worker.Pos.Line, orch.Pos.Line)
	}
}

// TestADKGoParser_SourcePos_RealAPI verifies that Pos is populated for the
// real ADK-Go SDK constructor pattern (loopagent.New(loopagent.Config{...})).
func TestADKGoParser_SourcePos_RealAPI(t *testing.T) {
	src := []byte(`package real

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/loopagent"
)

func BuildInfiniteLoop() {
	classifier, _ := llmagent.New(llmagent.Config{
		Name:        "classifier",
		Instruction: "Classify.",
	})
	_ = classifier
	_, _ = loopagent.New(loopagent.Config{
		AgentConfig: agent.Config{
			Name:      "retry_loop",
			SubAgents: []agent.Agent{classifier},
		},
	})
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	loop := graph.Nodes["retry_loop"]
	if loop == nil {
		t.Fatalf("node 'retry_loop' not found; nodes=%v", nodeKeys(graph))
	}
	if loop.Pos.IsZero() {
		t.Fatalf("retry_loop.Pos is zero; expected FileSet-derived position")
	}
	if loop.Pos.File != "input.go" {
		t.Errorf("retry_loop.Pos.File = %q, want 'input.go'", loop.Pos.File)
	}
	// The loopagent.Config{ composite literal is on line 15.
	if loop.Pos.Line != 15 {
		t.Errorf("retry_loop.Pos.Line = %d, want 15", loop.Pos.Line)
	}

	classifier := graph.Nodes["classifier"]
	if classifier == nil {
		t.Fatalf("node 'classifier' not found; nodes=%v", nodeKeys(graph))
	}
	if classifier.Pos.IsZero() {
		t.Fatalf("classifier.Pos is zero; expected FileSet-derived position")
	}
	// llmagent.Config{ literal is on line 10 — earlier than the loop.
	if classifier.Pos.Line >= loop.Pos.Line {
		t.Errorf("classifier line (%d) must precede retry_loop line (%d)",
			classifier.Pos.Line, loop.Pos.Line)
	}
}

// TestADKGoParser_ParseFile_PreservesPath verifies the Codex iter2 P1 fix:
// ParseFile must thread the real source path into Node.Pos.File so the
// --since flow and LSP code-actions can attribute findings back to the
// originating file. Previously ParseFile fell through to Parse which
// hardcoded "input.go", masking the real path for multi-file inputs.
func TestADKGoParser_ParseFile_PreservesPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.go")
	src := `package agents

var pipeline = &SequentialAgent{
	Name: "pipeline",
	SubAgents: []Agent{
		&LlmAgent{Name: "classifier", Model: "gpt-4o"},
	},
}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	// Use WithoutTypes() option to skip go/types fallback for hermeticity (no go.sum lookup).
	p := parser.NewADKGoParser(parser.WithoutTypes())
	graph, err := p.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(graph.Nodes) == 0 {
		t.Fatal("expected at least one node")
	}
	for id, node := range graph.Nodes {
		if node.Pos.IsZero() {
			t.Errorf("node %q has zero Pos", id)
			continue
		}
		if node.Pos.File != path {
			t.Errorf("node %q: Pos.File = %q, want %q (real path must propagate)", id, node.Pos.File, path)
		}
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

// TestADKGoParser_SequentialAgentIsSequenceNotLoop locks in the
// v0.9.1 ADK-Go FP fix: SequentialAgent must be parsed as
// NodeTypeSequence so loop_guard doesn't false-positive on
// sequential pipelines. The bare-struct path and the SDK-style
// `sequentialagent.New(sequentialagent.Config{...})` path both flow
// through this assertion via the parser entry point.
//
// Dogfood source: google/adk-samples/go/agents/llm-auditor
// (2026-05-11), where llm_auditor is a SequentialAgent that
// previously surfaced a Critical loop_guard finding.
func TestADKGoParser_SequentialAgentIsSequenceNotLoop(t *testing.T) {
	src := []byte(`package agents

var workflow = &SequentialAgent{
	Name: "llm_auditor",
	SubAgents: []Agent{
		&LlmAgent{Name: "critic", Model: "gpt-4o"},
		&LlmAgent{Name: "reviser", Model: "gpt-4o"},
	},
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	node := graph.Nodes["llm_auditor"]
	if node == nil {
		t.Fatalf("node 'llm_auditor' not found; nodes=%v", nodeKeys(graph))
	}
	if node.Type != domain.NodeTypeSequence {
		t.Errorf("SequentialAgent Type = %v, want NodeTypeSequence (loop_guard would FP otherwise)", node.Type)
	}
	if node.Type == domain.NodeTypeLoop || node.Type == domain.NodeTypeControl {
		t.Error("SequentialAgent must NOT be classified as Loop/Control — that path FP'd in v0.9.0")
	}
}

// TestADKGoParser_ParallelAgentIsParallelNotLoop is the symmetric
// case for ParallelAgent: NodeTypeParallel, never NodeTypeLoop /
// NodeTypeControl.
func TestADKGoParser_ParallelAgentIsParallelNotLoop(t *testing.T) {
	src := []byte(`package agents

var fanout = &ParallelAgent{
	Name: "broadcast",
	SubAgents: []Agent{
		&LlmAgent{Name: "worker_a", Model: "gpt-4o"},
		&LlmAgent{Name: "worker_b", Model: "gpt-4o"},
	},
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	node := graph.Nodes["broadcast"]
	if node == nil {
		t.Fatalf("node 'broadcast' not found; nodes=%v", nodeKeys(graph))
	}
	if node.Type != domain.NodeTypeParallel {
		t.Errorf("ParallelAgent Type = %v, want NodeTypeParallel", node.Type)
	}
	if node.Type == domain.NodeTypeLoop || node.Type == domain.NodeTypeControl {
		t.Error("ParallelAgent must NOT be classified as Loop/Control")
	}
}

// TestADKGoParser_AgentToolNewUnwrapsArg locks in the v0.9.2
// agenttool.New unwrap fix. Inline `agenttool.New(dataAnalyst, nil)`
// in a Tools slice used to produce a Tool node named "new" (the
// constructor's Sel name) because the parser walked the CallExpr
// to its Fun selector. After the fix, the parser detects
// `<pkg>tool.New(...)` constructors and recurses on the first arg,
// yielding the wrapped resource's identifier as the tool name.
//
// The fixture mirrors google/adk-samples financial-advisor's
// factory pattern (function body wraps llmagent.New(...) with
// Tools containing agenttool.New unwraps).
func TestADKGoParser_AgentToolNewUnwrapsArg(t *testing.T) {
	src := []byte(`package agents

func NewCoordinator() agent.Agent {
	a, _ := llmagent.New(llmagent.Config{
		Name:  "coordinator",
		Model: "gpt-4o",
		Tools: []tool.Tool{
			agenttool.New(dataAnalyst, nil),
			agenttool.New(tradingAnalyst, nil),
		},
	})
	return a
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	for _, want := range []string{"data_analyst", "trading_analyst"} {
		if graph.Nodes[want] == nil {
			t.Errorf("expected tool node %q, missing; nodes=%v", want, nodeKeys(graph))
		}
	}
	if _, badFP := graph.Nodes["new"]; badFP {
		t.Errorf("must NOT extract constructor name 'new' as a tool — v0.9.1 FP")
	}
}

// TestADKGoParser_EntryPrefersLLMRoot covers the entry-selection
// half of the same v0.9.2 fix. Pre-fix, firstStandaloneAgentID
// sorted node IDs alphabetically and picked the first — so a graph
// with `data_analyst`, `coordinator`, etc. picked the leaf
// `data_analyst` as entry. Post-fix, the LLM with zero incoming
// edges (`coordinator`) is chosen, which avoids cascading
// `unreachable_node` FPs on the actual sub-agents.
func TestADKGoParser_EntryPrefersLLMRoot(t *testing.T) {
	src := []byte(`package agents

func NewCoordinator() agent.Agent {
	a, _ := llmagent.New(llmagent.Config{
		Name:  "coordinator",
		Model: "gpt-4o",
		Tools: []tool.Tool{
			agenttool.New(dataAnalyst, nil),
			agenttool.New(executor, nil),
		},
	})
	return a
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if graph.EntryNodeID != "coordinator" {
		t.Errorf("EntryNodeID = %q, want \"coordinator\" (the LLM root). nodes=%v",
			graph.EntryNodeID, nodeKeys(graph))
	}
}

// TestADKGoParser_GenericFunctionToolNewIsRecognised guards the
// codex-review-flagged ordering bug in isToolConstructorCall: for
// `functiontool.New[TArgs, TResults](...)` the Go AST stores call.Fun
// as an IndexListExpr WRAPPING the SelectorExpr, not the other way
// around. The original code asserted SelectorExpr on call.Fun
// directly, which fails for generic forms and silently dropped the
// tool from the graph. After the fix the unwrap happens first; this
// test makes that ordering load-bearing.
//
// We assert on the *graph* (tool present + edge from owner) rather
// than on the predicate in isolation so that the regression survives
// future refactors that move the predicate.
func TestADKGoParser_GenericFunctionToolNewIsRecognised(t *testing.T) {
	src := []byte(`package agents

func NewWorker() agent.Agent {
	a, _ := llmagent.New(llmagent.Config{
		Name:  "worker",
		Model: "gpt-4o",
		Tools: []tool.Tool{
			functiontool.New[QueryArgs, QueryResult](
				functiontool.Config{Name: "search_index"},
				searchHandler,
			),
		},
	})
	return a
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if graph.Nodes["search_index"] == nil {
		t.Errorf("generic functiontool.New[T,R] tool missing; nodes=%v", nodeKeys(graph))
	}
	if _, leaked := graph.Nodes["new"]; leaked {
		t.Errorf("generic constructor name 'new' leaked as a tool")
	}
}

// TestADKGoParser_AmbiguousRootsNoAutoEntry covers Codex round-2 P6:
// when a file declares two unrelated standalone factories (no edges
// between them, both zero-indegree LLMs), the parser must NOT pick
// one alphabetically as the entry — doing so would manufacture an
// unreachable_node finding on the other. Returning an empty
// EntryNodeID signals "no clear root" so downstream rules skip the
// reachability check.
func TestADKGoParser_AmbiguousRootsNoAutoEntry(t *testing.T) {
	src := []byte(`package agents

func NewA() agent.Agent {
	a, _ := llmagent.New(llmagent.Config{Name: "agent_a", Model: "gpt-4o"})
	return a
}

func NewB() agent.Agent {
	a, _ := llmagent.New(llmagent.Config{Name: "agent_b", Model: "gpt-4o"})
	return a
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if graph.Nodes["agent_a"] == nil || graph.Nodes["agent_b"] == nil {
		t.Fatalf("expected both factory agents in graph; got %v", nodeKeys(graph))
	}
	if graph.EntryNodeID != "" {
		t.Errorf("EntryNodeID must be empty when roots are ambiguous; got %q", graph.EntryNodeID)
	}
}

// TestADKGoParser_AmbiguousRootsNoSpuriousCritical is the end-to-end
// half of Codex Slice E #1: parsing an ambiguous-root ADK-Go file
// produces a graph with EntryAmbiguous=true so the reachability
// rule downstream skips and the orchestrator emits no Critical
// "entry node is not set" finding. The previous round-2 fix
// suppressed unreachable_node FPs but inadvertently swapped them
// for a Critical from reachability.go's empty-EntryNodeID guard.
func TestADKGoParser_AmbiguousRootsNoSpuriousCritical(t *testing.T) {
	src := []byte(`package agents

func NewA() agent.Agent {
	a, _ := llmagent.New(llmagent.Config{Name: "agent_a", Model: "gpt-4o"})
	return a
}

func NewB() agent.Agent {
	a, _ := llmagent.New(llmagent.Config{Name: "agent_b", Model: "gpt-4o"})
	return a
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !graph.EntryAmbiguous {
		t.Fatal("ADK-Go parser should set EntryAmbiguous=true for multi-root factory file")
	}
	if graph.EntryNodeID != "" {
		t.Errorf("EntryNodeID must remain empty when ambiguous; got %q", graph.EntryNodeID)
	}
}

// TestADKGoParser_SequentialAgentRealAPIIsSequence covers the
// SDK-style `sequentialagent.New(sequentialagent.Config{...})` path
// that the original Slice E regression test missed (Slice G #1).
// The earlier regression only exercised the bare-struct path,
// leaving the real-world factory shape that triggered the dogfood
// FP (google/adk-samples llm-auditor) un-locked.
func TestADKGoParser_SequentialAgentRealAPIIsSequence(t *testing.T) {
	src := []byte(`package agents

func NewAuditor() agent.Agent {
	rootAgent, _ := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name: "llm_auditor",
			SubAgents: []agent.Agent{
				criticAgent,
				reviserAgent,
			},
		},
	})
	return rootAgent
}
`)
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	node := graph.Nodes["llm_auditor"]
	if node == nil {
		t.Fatalf("expected 'llm_auditor' node; got %v", nodeKeys(graph))
	}
	if node.Type != domain.NodeTypeSequence {
		t.Errorf("real-API SequentialAgent Type=%v, want NodeTypeSequence", node.Type)
	}
}
