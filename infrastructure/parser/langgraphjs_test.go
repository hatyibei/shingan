package parser_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/infrastructure/parser"
)

// findLangGraphJSShim resolves the bundled Node shim relative to the test
// working directory, walking up. Mirrors findShim / findCrewAIShim.
func findLangGraphJSShim(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join("infrastructure", "parser", "shims", "export_langgraphjs_server.mjs"),
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		for _, rel := range candidates {
			p := filepath.Join(dir, rel)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate export_langgraphjs_server.mjs from %q (looked in %v)", dir, candidates)
		}
		dir = parent
	}
}

// findLangGraphJSTestdata returns testdata/langgraphjs/, walking up.
func findLangGraphJSTestdata(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		p := filepath.Join(dir, "testdata", "langgraphjs")
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate testdata/langgraphjs from %q", dir)
		}
		dir = parent
	}
}

// requireNode skips the test when `node` is not on PATH. The LangGraph.js shim
// is AST-only (it never imports @langchain/langgraph), so node alone is enough
// — there is no framework-installed gate like requirePythonLangGraph.
func requireNode(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skipf("node not found in PATH: %v", err)
	}
}

func TestLangGraphJSParser_SupportedFormat(t *testing.T) {
	requireNode(t)
	p, err := parser.NewLangGraphJSParser(parser.WithLangGraphJSScriptPath(findLangGraphJSShim(t)))
	if err != nil {
		t.Fatalf("NewLangGraphJSParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if got := p.SupportedFormat(); got != "langgraph-js" {
		t.Errorf("SupportedFormat() = %q, want %q", got, "langgraph-js")
	}
}

func TestLangGraphJSParser_NodeUnavailable(t *testing.T) {
	_, err := parser.NewLangGraphJSParser(
		parser.WithLangGraphJSScriptPath(findLangGraphJSShim(t)),
		parser.WithLangGraphJSNodeBinary("node_does_not_exist_xyz_42"),
	)
	if err == nil {
		t.Fatal("expected error when node is not available")
	}
	if !strings.Contains(err.Error(), "not found in PATH") {
		t.Errorf("error %q does not mention PATH", err)
	}
	if !strings.Contains(err.Error(), "Node.js") {
		t.Errorf("error %q does not mention the Node.js install hint", err)
	}
}

func TestLangGraphJSParser_LocateShimNamed(t *testing.T) {
	path, err := parser.LocateShimNamed("export_langgraphjs_server.mjs")
	if err != nil {
		t.Fatalf("LocateShimNamed: %v", err)
	}
	if !strings.HasSuffix(path, "shims/export_langgraphjs_server.mjs") {
		t.Errorf("path %q does not end in shims/export_langgraphjs_server.mjs", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("located path does not exist: %v", err)
	}
}

// --- integration tests below require `node` on PATH (real shim) -------------

func newJSParser(t *testing.T) *parser.LangGraphJSParser {
	t.Helper()
	requireNode(t)
	p, err := parser.NewLangGraphJSParser(parser.WithLangGraphJSScriptPath(findLangGraphJSShim(t)))
	if err != nil {
		t.Fatalf("NewLangGraphJSParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func TestLangGraphJSParser_SimpleChain(t *testing.T) {
	p := newJSParser(t)
	dir := findLangGraphJSTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "simple_chain.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"a", "b"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("expected node %q in graph (nodes=%v)", id, nodeIDsJS(graph))
		}
	}
	if graph.EntryNodeID != "a" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "a")
	}
	// START/END are sentinels, never materialised as nodes.
	for _, sentinel := range []string{"__start__", "__end__", "START", "END"} {
		if _, ok := graph.Nodes[sentinel]; ok {
			t.Errorf("sentinel %q should not be a node", sentinel)
		}
	}
	if !hasEdgeJS(graph, "a", "b") {
		t.Errorf("expected edge a->b (edges=%v)", graph.Edges)
	}
	// b -> END is dropped; b must carry the exit branch instead.
	if !graph.Nodes["b"].HasExitBranch {
		t.Errorf("node b should have HasExitBranch (b -> END)")
	}
}

func TestLangGraphJSParser_Branching(t *testing.T) {
	p := newJSParser(t)
	dir := findLangGraphJSTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "branching.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"classify", "handleA", "handleB"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("expected node %q (nodes=%v)", id, nodeIDsJS(graph))
		}
	}
	if graph.EntryNodeID != "classify" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "classify")
	}
	// Conditional edges carry the pathMap key as the Edge.Condition.
	if !hasConditionalEdgeJS(graph, "classify", "handleA", "a") {
		t.Errorf("expected conditional edge classify->handleA (cond a), edges=%v", graph.Edges)
	}
	if !hasConditionalEdgeJS(graph, "classify", "handleB", "b") {
		t.Errorf("expected conditional edge classify->handleB (cond b), edges=%v", graph.Edges)
	}
	// Both handlers terminate at END → has_exit_branch.
	for _, id := range []string{"handleA", "handleB"} {
		if !graph.Nodes[id].HasExitBranch {
			t.Errorf("node %q should have HasExitBranch (-> END)", id)
		}
	}
}

// TestLangGraphJSParser_ReactLoop_CycleWarning is the load-bearing acceptance
// test (ADR-015): the agent<->tools react loop with a conditional END exit must
// surface as a Warning, not Critical, from cycle_detection. This proves the
// shim sets HasExitBranch on the router source so the cycle downgrades exactly
// as it does for the Python LangGraph parser.
func TestLangGraphJSParser_ReactLoop_CycleWarning(t *testing.T) {
	p := newJSParser(t)
	dir := findLangGraphJSTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "react_loop.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Layer 1 — parse structure. Failures here localise parse vs rule bugs.
	for _, id := range []string{"agent", "tools"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Fatalf("expected node %q (nodes=%v)", id, nodeIDsJS(graph))
		}
	}
	if graph.EntryNodeID != "agent" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "agent")
	}
	if !hasEdgeJS(graph, "agent", "tools") {
		t.Errorf("expected edge agent->tools (edges=%v)", graph.Edges)
	}
	if !hasEdgeJS(graph, "tools", "agent") {
		t.Errorf("expected edge tools->agent (edges=%v)", graph.Edges)
	}
	// The conditional source must carry the exit branch (END in its pathMap).
	if !graph.Nodes["agent"].HasExitBranch {
		t.Fatalf("node agent must have HasExitBranch (conditional END exit); " +
			"without it cycle_detection would emit Critical")
	}

	// Layer 2 — the actual acceptance criterion.
	findings := rules.NewCycleDetector().Analyze(graph)
	var cycleFindings []domain.Finding
	for _, f := range findings {
		if f.RuleName == "cycle_detection" {
			cycleFindings = append(cycleFindings, f)
		}
	}
	if len(cycleFindings) == 0 {
		t.Fatalf("expected a cycle_detection finding for the react loop, got none")
	}
	for _, f := range cycleFindings {
		if f.Severity == domain.Critical {
			t.Errorf("cycle_detection reported Critical for react loop; want Warning "+
				"(HasExitBranch parity broken): %+v", f)
		}
		if f.Severity != domain.Warning {
			t.Errorf("cycle_detection severity = %v, want Warning: %+v", f.Severity, f)
		}
	}
}

// --- small assertion helpers ------------------------------------------------

func nodeIDsJS(g *domain.WorkflowGraph) []string {
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	return ids
}

func hasEdgeJS(g *domain.WorkflowGraph, from, to string) bool {
	for _, e := range g.Edges {
		if e.From == from && e.To == to {
			return true
		}
	}
	return false
}

func hasConditionalEdgeJS(g *domain.WorkflowGraph, from, to, cond string) bool {
	for _, e := range g.Edges {
		if e.From == from && e.To == to && e.Condition == cond {
			return true
		}
	}
	return false
}
