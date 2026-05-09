package parser_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/infrastructure/parser"
)

// osStat is a tiny indirection so the test file's import block stays minimal.
var osStat = os.Stat

// findTestdataDir returns the absolute path to testdata/langgraph/, walking
// upwards from the current test working directory.
func findTestdataDir(t *testing.T) string {
	t.Helper()
	dir, err := filepathAbs(".")
	if err != nil {
		t.Fatalf("abs cwd: %v", err)
	}
	for {
		p := filepath.Join(dir, "testdata", "langgraph")
		if isDir(p) {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate testdata/langgraph from %q", dir)
		}
		dir = parent
	}
}

// filepathAbs / isDir are wrappers kept tiny so the import block stays clean.
func filepathAbs(p string) (string, error) { return filepath.Abs(p) }
func isDir(p string) bool {
	info, err := osStat(p)
	return err == nil && info.IsDir()
}

func TestLangGraphParser_SupportedFormat(t *testing.T) {
	requirePython(t)
	p, err := parser.NewLangGraphParser(parser.WithLangGraphScriptPath(findShim(t)))
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if got := p.SupportedFormat(); got != "langgraph" {
		t.Errorf("SupportedFormat() = %q, want %q", got, "langgraph")
	}
}

func TestLangGraphParser_PythonUnavailable(t *testing.T) {
	// Force a bad Python binary so the worker can't even start. Either the
	// spawn fails outright (preferred path) or the very first call surfaces
	// the missing-binary error.
	_, err := parser.NewLangGraphParser(
		parser.WithLangGraphScriptPath(findShim(t)),
		parser.WithLangGraphPythonBinary("python_does_not_exist_xyz_42"),
	)
	if err == nil {
		t.Fatal("expected error when python is not available")
	}
	if !strings.Contains(err.Error(), "not found in PATH") {
		t.Errorf("error %q does not mention PATH", err)
	}
}

func TestLangGraphParser_LangGraphMissing(t *testing.T) {
	requirePython(t)
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found")
	}
	cmd := exec.Command("python3", "-c", "import langgraph")
	if err := cmd.Run(); err == nil {
		t.Skip("langgraph IS installed in this environment; this test only runs when langgraph is missing")
	}

	p, err := parser.NewLangGraphParser(parser.WithLangGraphScriptPath(findShim(t)))
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	_, parseErr := p.Parse([]byte("from langgraph.graph import StateGraph\n"))
	if parseErr == nil {
		t.Fatal("expected error when langgraph is missing")
	}
	if !strings.Contains(parseErr.Error(), "pip install langgraph") {
		t.Errorf("error %q does not mention `pip install langgraph`", parseErr)
	}
}

// integration tests below all require a live `python3 -c 'import langgraph'`.
// They share the parser instance via t.Cleanup so each test still gets its
// own subprocess (no state leakage).

func TestLangGraphParser_SimpleChain(t *testing.T) {
	requirePythonLangGraph(t)
	dir := findTestdataDir(t)

	p, err := parser.NewLangGraphParser(parser.WithLangGraphScriptPath(findShim(t)))
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	graph, err := p.ParseFile(filepath.Join(dir, "simple_chain.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// START / END are pseudo-sentinels in LangGraph; we elide them so that
	// Shingan's `loop_guard` doesn't misclassify the START node as a LoopAgent.
	wantNodes := []string{"classify", "respond"}
	for _, id := range wantNodes {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("missing expected node %q", id)
		}
	}
	for _, sentinel := range []string{"__start__", "__end__"} {
		if _, ok := graph.Nodes[sentinel]; ok {
			t.Errorf("sentinel %q must not appear as a node", sentinel)
		}
	}
	if got := graph.EntryNodeID; got != "classify" {
		t.Errorf("EntryNodeID = %q, want \"classify\"", got)
	}
	// classify -> respond, single inter-node edge (START/END dropped).
	if got, want := len(graph.Edges), 1; got != want {
		t.Errorf("len(Edges) = %d, want %d", got, want)
	}
	for _, id := range []string{"classify", "respond"} {
		n := graph.Nodes[id]
		if n.Type != domain.NodeTypeLLM {
			t.Errorf("node %q Type = %v, want NodeTypeLLM", id, n.Type)
		}
	}
}

func TestLangGraphParser_Branching(t *testing.T) {
	requirePythonLangGraph(t)
	dir := findTestdataDir(t)

	p, err := parser.NewLangGraphParser(parser.WithLangGraphScriptPath(findShim(t)))
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	graph, err := p.ParseFile(filepath.Join(dir, "branching.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// Conditional edges are over-approximated → triage should fan out to all
	// three workers, each via a labelled edge.
	conditional := 0
	for _, e := range graph.Edges {
		if e.From == "triage" && e.Condition != "" {
			conditional++
		}
	}
	if conditional < 3 {
		t.Errorf("expected at least 3 conditional edges from triage, got %d", conditional)
	}
}

func TestLangGraphParser_ReactLoop(t *testing.T) {
	requirePythonLangGraph(t)
	dir := findTestdataDir(t)

	p, err := parser.NewLangGraphParser(parser.WithLangGraphScriptPath(findShim(t)))
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	graph, err := p.ParseFile(filepath.Join(dir, "react_loop.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// model→tools and tools→model both present → cycle.
	hasMT, hasTM := false, false
	for _, e := range graph.Edges {
		if e.From == "model" && e.To == "tools" {
			hasMT = true
		}
		if e.From == "tools" && e.To == "model" {
			hasTM = true
		}
	}
	if !hasMT || !hasTM {
		t.Errorf("expected model⇄tools cycle, got hasMT=%v hasTM=%v", hasMT, hasTM)
	}
	if n, ok := graph.Nodes["tools"]; ok && n.Type != domain.NodeTypeTool {
		t.Errorf("tools node Type = %v, want NodeTypeTool", n.Type)
	}
}

func TestLangGraphParser_MultiAgent(t *testing.T) {
	requirePythonLangGraph(t)
	dir := findTestdataDir(t)

	p, err := parser.NewLangGraphParser(parser.WithLangGraphScriptPath(findShim(t)))
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	graph, err := p.ParseFile(filepath.Join(dir, "multi_agent.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// All workers loop back to supervisor.
	loopBacks := 0
	for _, e := range graph.Edges {
		if e.To == "supervisor" && e.From != "__start__" {
			loopBacks++
		}
	}
	if loopBacks < 3 {
		t.Errorf("expected >= 3 worker→supervisor loopback edges, got %d", loopBacks)
	}
}

func TestLangGraphParser_WorkerCrashRecovery(t *testing.T) {
	// We don't restart automatically yet (Phase 2-A), but the parser must at
	// least surface a clean error rather than hanging when the worker dies.
	requirePython(t)
	p, err := parser.NewLangGraphParser(parser.WithLangGraphScriptPath(findShim(t)))
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	// Force-close mid-session.
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := p.ParseFile("does_not_matter.py"); err == nil {
		t.Fatal("expected error on parse after Close()")
	}
}

// TestLangGraphParser_SiblingImports verifies the iter5 P1 fix: when
// an LSP buffer has an on-disk path, the shim adds dirname(filename) to
// sys.path so a sibling-package import (`from helpers import ...`)
// resolves the same way it does in the CLI's ParseFile path.
//
// Issue #10 regression test: skipped when langgraph is not installed.
func TestLangGraphParser_SiblingImports(t *testing.T) {
	requirePythonLangGraph(t)

	dir := t.TempDir()
	helpersPath := filepath.Join(dir, "helpers.py")
	agentPath := filepath.Join(dir, "agent.py")

	if err := os.WriteFile(helpersPath, []byte(`# helpers.py — sibling module
def make_state():
    return {"key": "value"}
`), 0o644); err != nil {
		t.Fatalf("write helpers.py: %v", err)
	}

	// agent.py imports the sibling module by bare name — this only
	// resolves when the parent directory is on sys.path.
	if err := os.WriteFile(agentPath, []byte(`# agent.py — uses sibling helpers
from helpers import make_state
from langgraph.graph import StateGraph
import operator

def step(state):
    return state

g = StateGraph(dict)
g.add_node("step", step)
g.set_entry_point("step")
g.add_edge("step", "__end__")
graph = g.compile()
`), 0o644); err != nil {
		t.Fatalf("write agent.py: %v", err)
	}

	p, err := parser.NewLangGraphParser()
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	graph, err := p.ParseFile(agentPath)
	if err != nil {
		t.Fatalf("ParseFile with sibling import failed (sys.path not propagated?): %v", err)
	}
	if len(graph.Nodes) == 0 {
		t.Error("expected at least one node from a successfully parsed sibling-import workflow")
	}
}

// TestLangGraphParser_NodeIDPunctuationPreserved verifies the iter5 P2
// fix: punctuation-bearing node names ("search-web" vs "search_web")
// must yield distinct node IDs so the shim doesn't silently merge them.
//
// Issue #10 regression test.
func TestLangGraphParser_NodeIDPunctuationPreserved(t *testing.T) {
	requirePythonLangGraph(t)

	src := `from langgraph.graph import StateGraph

def fn1(state): return state
def fn2(state): return state

g = StateGraph(dict)
g.add_node("search-web", fn1)
g.add_node("search_web", fn2)
g.set_entry_point("search-web")
g.add_edge("search-web", "search_web")
g.add_edge("search_web", "__end__")
graph = g.compile()
`

	p, err := parser.NewLangGraphParser()
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	wfg, err := p.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// The two punctuation variants must remain distinct.
	if _, ok := wfg.Nodes["search-web"]; !ok {
		t.Errorf("expected node ID 'search-web' in graph; got nodes: %v", nodeIDList(wfg))
	}
	if _, ok := wfg.Nodes["search_web"]; !ok {
		t.Errorf("expected node ID 'search_web' (distinct from search-web); got nodes: %v", nodeIDList(wfg))
	}
	if len(wfg.Nodes) < 2 {
		t.Errorf("expected at least 2 nodes (no merge), got %d: %v", len(wfg.Nodes), nodeIDList(wfg))
	}
}

// TestLangGraphParser_MultiGraphSubgraph verifies that when a module
// defines two independent StateGraph instances (subgraph composition,
// the LangGraph-recommended pattern for `open_deep_research`-style
// pipelines), the AST visitor returns the OUTER graph — the variable
// the user's `graph = builder.compile()` actually exposed — not a
// flat-merge of both.
//
// Regression: an earlier visitor merged both StateGraphs into a single
// node set under the inner graph's entry point, producing six
// false-positive unreachable_node warnings on every outer-graph node.
func TestLangGraphParser_MultiGraphSubgraph(t *testing.T) {
	requirePythonLangGraph(t)
	dir := findTestdataDir(t)

	p, err := parser.NewLangGraphParser(parser.WithLangGraphScriptPath(findShim(t)))
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	graph, err := p.ParseFile(filepath.Join(dir, "multi_graph_subgraph.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Outer-graph nodes (must all be present): plan, section, gather, finalize.
	for _, want := range []string{"plan", "section", "gather", "finalize"} {
		if _, ok := graph.Nodes[want]; !ok {
			t.Errorf("expected outer-graph node %q in result; got %v", want, nodeIDList(graph))
		}
	}
	// Inner-only nodes must NOT bleed into the outer graph: gen_query,
	// web_search, write_section live exclusively on the inner subgraph
	// and would only appear if the visitor flat-merged.
	for _, leak := range []string{"gen_query", "web_search", "write_section"} {
		if _, ok := graph.Nodes[leak]; ok {
			t.Errorf("inner subgraph node %q leaked into outer graph result; got %v", leak, nodeIDList(graph))
		}
	}
	if got := graph.EntryNodeID; got != "plan" {
		t.Errorf("EntryNodeID = %q, want %q (outer graph's START → plan edge)", got, "plan")
	}
}

// TestLangGraphParser_CommandGoto verifies that the AST shim
// recovers implicit edges from `def fn(...) -> Command[Literal[...]]`
// type annotations AND from inline `return Command(goto="x")` calls.
//
// Without this, every LangGraph node whose handler dispatches via
// Command appears as a dead-end in the static graph and triggers
// false-positive `unreachable_node` warnings on every downstream
// node. Real-world dogfood: open_deep_research/legacy/graph.py
// reduced 9 findings to 1 once Command/goto + list path_map were
// both extracted.
func TestLangGraphParser_CommandGoto(t *testing.T) {
	requirePythonLangGraph(t)
	dir := findTestdataDir(t)

	p, err := parser.NewLangGraphParser(parser.WithLangGraphScriptPath(findShim(t)))
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	graph, err := p.ParseFile(filepath.Join(dir, "command_goto.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	hasEdge := func(from, to string) bool {
		for _, e := range graph.Edges {
			if e.From == from && e.To == to {
				return true
			}
		}
		return false
	}

	// Source 1: typed Command[Literal["planner", "writer"]] on
	// human_feedback. Both destinations must materialise.
	for _, dst := range []string{"planner", "writer"} {
		if !hasEdge("human_feedback", dst) {
			t.Errorf("expected typed-Command edge human_feedback → %s; edges: %+v", dst, graph.Edges)
		}
	}

	// Source 2: bare Command(goto=...) inside `router`. Both
	// destinations (archiver, reviewer) must materialise.
	for _, dst := range []string{"archiver", "reviewer"} {
		if !hasEdge("router", dst) {
			t.Errorf("expected bare-Command edge router → %s; edges: %+v", dst, graph.Edges)
		}
	}
}

// TestLangGraphParser_ConditionalRouterLiteral verifies that
// `add_conditional_edges("src", router_fn)` (no mapping argument)
// recovers destinations from the router function's `-> Literal[...]`
// return-type annotation.
//
// Without this, every node behind such a conditional appears as
// `unreachable_node` because the AST visitor only saw the START →
// router source edge. Real-world dogfood: executive-ai-assistant
// (langchain-ai) emitted 11 false-positive unreachable warnings on
// graph.py before this lookup landed.
func TestLangGraphParser_ConditionalRouterLiteral(t *testing.T) {
	requirePythonLangGraph(t)
	dir := findTestdataDir(t)

	p, err := parser.NewLangGraphParser(parser.WithLangGraphScriptPath(findShim(t)))
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	graph, err := p.ParseFile(filepath.Join(dir, "conditional_router_literal.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	hasEdge := func(from, to string) bool {
		for _, e := range graph.Edges {
			if e.From == from && e.To == to {
				return true
			}
		}
		return false
	}

	// All three destinations from `route_after_triage`'s
	// `-> Literal["draft", "archive", "notify"]` must materialise as
	// edges from the conditional source `triage`.
	for _, dst := range []string{"draft", "archive", "notify"} {
		if !hasEdge("triage", dst) {
			t.Errorf("expected router-Literal edge triage → %s; edges: %+v", dst, graph.Edges)
		}
	}
}

// TestLangGraphParser_FanInListSrc verifies that
// `builder.add_edge(["a", "b"], "c")` produces edges {a→c, b→c}.
//
// Dogfood: langgraph/libs/langgraph/bench/wide_state.py uses this
// fan-in idiom on its `["three", "four"] → "five"` join. Before the
// list-src fix the source list was treated as non-literal and the
// edge dropped, leaving `five` and `six` apparently unreachable.
func TestLangGraphParser_FanInListSrc(t *testing.T) {
	requirePythonLangGraph(t)

	src := `from langgraph.graph import StateGraph, START, END

def fn(state): return state

g = StateGraph(dict)
g.add_node("a", fn)
g.add_node("b", fn)
g.add_node("c", fn)
g.add_edge(START, "a")
g.add_edge(START, "b")
g.add_edge(["a", "b"], "c")
g.add_edge("c", END)
graph = g.compile()
`
	p, err := parser.NewLangGraphParser()
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	wfg, err := p.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	hasEdge := func(from, to string) bool {
		for _, e := range wfg.Edges {
			if e.From == from && e.To == to {
				return true
			}
		}
		return false
	}
	if !hasEdge("a", "c") {
		t.Errorf("expected edge a→c (fan-in expansion); edges: %+v", wfg.Edges)
	}
	if !hasEdge("b", "c") {
		t.Errorf("expected edge b→c (fan-in expansion); edges: %+v", wfg.Edges)
	}
}

// nodeIDList returns the sorted node ID slice for diagnostic messages.
func nodeIDList(g *domain.WorkflowGraph) []string {
	out := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		out = append(out, id)
	}
	return out
}
