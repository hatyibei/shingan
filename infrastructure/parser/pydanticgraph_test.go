package parser_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/infrastructure/parser"
)

// findPydanticGraphShim resolves the bundled Python shim relative to the test
// working directory, walking up. Mirrors findCrewAIShim / findLangGraphJSShim.
func findPydanticGraphShim(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join("infrastructure", "parser", "shims", "export_pydanticgraph_server.py"),
		filepath.Join("scripts", "export_pydanticgraph_server.py"),
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
			t.Fatalf("could not locate export_pydanticgraph_server.py from %q (looked in %v)", dir, candidates)
		}
		dir = parent
	}
}

// findPydanticGraphTestdata returns testdata/pydanticgraph/, walking up.
func findPydanticGraphTestdata(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		p := filepath.Join(dir, "testdata", "pydanticgraph")
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate testdata/pydanticgraph from %q", dir)
		}
		dir = parent
	}
}

func TestPydanticGraphParser_SupportedFormat(t *testing.T) {
	requirePython(t)
	p, err := parser.NewPydanticGraphParser(parser.WithPydanticGraphScriptPath(findPydanticGraphShim(t)))
	if err != nil {
		t.Fatalf("NewPydanticGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if got := p.SupportedFormat(); got != "pydantic-graph" {
		t.Errorf("SupportedFormat() = %q, want %q", got, "pydantic-graph")
	}
}

func TestPydanticGraphParser_PythonUnavailable(t *testing.T) {
	_, err := parser.NewPydanticGraphParser(
		parser.WithPydanticGraphScriptPath(findPydanticGraphShim(t)),
		parser.WithPydanticGraphPythonBinary("python_does_not_exist_xyz_42"),
	)
	if err == nil {
		t.Fatal("expected error when python is not available")
	}
	if !strings.Contains(err.Error(), "not found in PATH") {
		t.Errorf("error %q does not mention PATH", err)
	}
}

func TestPydanticGraphParser_LocateShimNamed(t *testing.T) {
	path, err := parser.LocateShimNamed("export_pydanticgraph_server.py")
	if err != nil {
		t.Fatalf("LocateShimNamed: %v", err)
	}
	if !strings.HasSuffix(path, "shims/export_pydanticgraph_server.py") &&
		!strings.HasSuffix(path, "scripts/export_pydanticgraph_server.py") {
		t.Errorf("path %q does not end in shims/ or scripts/ export_pydanticgraph_server.py", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("located path does not exist: %v", err)
	}
}

// --- integration tests below this point require `python3` on PATH ----------
// The shim is AST-only (it never imports pydantic_graph), so python3 alone is
// sufficient — there is intentionally NO `import pydantic_graph` gate. Gating
// on the framework would silently SKIP the load-bearing cycle test wherever
// pydantic-graph isn't installed, verifying nothing.

func newPGParser(t *testing.T) *parser.PydanticGraphParser {
	t.Helper()
	requirePython(t)
	p, err := parser.NewPydanticGraphParser(parser.WithPydanticGraphScriptPath(findPydanticGraphShim(t)))
	if err != nil {
		t.Fatalf("NewPydanticGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func pgHasEdge(g *domain.WorkflowGraph, from, to string) bool {
	for _, e := range g.Edges {
		if e.From == from && e.To == to {
			return true
		}
	}
	return false
}

func pgNodeIDs(g *domain.WorkflowGraph) []string {
	ids := make([]string, 0, len(g.Nodes))
	for k := range g.Nodes {
		ids = append(ids, k)
	}
	return ids
}

func TestPydanticGraphParser_Linear(t *testing.T) {
	p := newPGParser(t)
	dir := findPydanticGraphTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "linear.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"A", "B"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("expected node %q (nodes=%v)", id, pgNodeIDs(graph))
		}
	}
	if graph.EntryNodeID != "A" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "A")
	}
	// End is a sentinel — never a node.
	for _, sentinel := range []string{"End", "End[int]"} {
		if _, ok := graph.Nodes[sentinel]; ok {
			t.Errorf("sentinel %q should not be a node", sentinel)
		}
	}
	if !pgHasEdge(graph, "A", "B") {
		t.Errorf("expected edge A->B (edges=%v)", graph.Edges)
	}
	// B returns End[int]; B -> End is dropped but B carries the exit branch.
	if !graph.Nodes["B"].HasExitBranch {
		t.Errorf("node B should have HasExitBranch (B -> End)")
	}
	if graph.Nodes["A"].HasExitBranch {
		t.Errorf("node A should NOT have HasExitBranch (A -> B only)")
	}
	// Nodes are emitted as NodeTypeTask (leaf "step" units).
	if graph.Nodes["A"].Type != domain.NodeTypeTask {
		t.Errorf("node A type = %v, want NodeTypeTask", graph.Nodes["A"].Type)
	}
}

func TestPydanticGraphParser_Branching(t *testing.T) {
	p := newPGParser(t)
	dir := findPydanticGraphTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "branching.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"Classify", "HandleA", "HandleB"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("expected node %q (nodes=%v)", id, pgNodeIDs(graph))
		}
	}
	if graph.EntryNodeID != "Classify" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "Classify")
	}
	// The run union produces an edge to each branch target.
	if !pgHasEdge(graph, "Classify", "HandleA") {
		t.Errorf("expected edge Classify->HandleA (edges=%v)", graph.Edges)
	}
	if !pgHasEdge(graph, "Classify", "HandleB") {
		t.Errorf("expected edge Classify->HandleB (edges=%v)", graph.Edges)
	}
	// Both handlers return End → has_exit_branch.
	for _, id := range []string{"HandleA", "HandleB"} {
		if !graph.Nodes[id].HasExitBranch {
			t.Errorf("node %q should have HasExitBranch (-> End)", id)
		}
	}
}

// TestPydanticGraphParser_CycleWithEnd_Warning is the load-bearing acceptance
// test (ADR-015), mirroring TestLangGraphJSParser_ReactLoop_CycleWarning. A node
// whose run() returns `Self | End` forms a self-loop that exits via End; the
// shim sets HasExitBranch so cycle_detection downgrades Critical -> Warning.
func TestPydanticGraphParser_CycleWithEnd_Warning(t *testing.T) {
	p := newPGParser(t)
	dir := findPydanticGraphTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "cycle_with_end.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Layer 1 — parse structure. Failures here localise parse vs rule bugs.
	if _, ok := graph.Nodes["Counter"]; !ok {
		t.Fatalf("expected node Counter (nodes=%v)", pgNodeIDs(graph))
	}
	// Self in the run union → self-edge (the cycle). Without it there is no
	// cycle and the rule fires nothing.
	if !pgHasEdge(graph, "Counter", "Counter") {
		t.Fatalf("expected self-edge Counter->Counter (Self in run union); edges=%v", graph.Edges)
	}
	// End in the union → exit branch. Without it the cycle false-Criticals.
	if !graph.Nodes["Counter"].HasExitBranch {
		t.Fatalf("node Counter must have HasExitBranch (End in run union); " +
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
		t.Fatalf("expected a cycle_detection finding for the self-loop, got none")
	}
	for _, f := range cycleFindings {
		if f.Severity == domain.Critical {
			t.Errorf("cycle_detection reported Critical for the End-exit loop; want Warning "+
				"(HasExitBranch parity broken): %+v", f)
		}
		if f.Severity != domain.Warning {
			t.Errorf("cycle_detection severity = %v, want Warning: %+v", f.Severity, f)
		}
	}
}

// TestPydanticGraphParser_NonPydanticFile locks in the robustness contract: a
// .py file with no BaseNode subclasses (or a syntax error) yields an empty
// graph, never an error / worker crash.
func TestPydanticGraphParser_NonPydanticFile(t *testing.T) {
	p := newPGParser(t)

	graph, err := p.Parse([]byte("import os\n\ndef hello():\n    return 1\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(graph.Nodes) != 0 {
		t.Errorf("expected 0 nodes for a non-pydantic file, got %d: %v", len(graph.Nodes), pgNodeIDs(graph))
	}

	// Syntax error → empty graph, worker stays alive (next call still works).
	bad, err := p.Parse([]byte("def broken(:\n"))
	if err != nil {
		t.Fatalf("Parse(syntax error): %v", err)
	}
	if len(bad.Nodes) != 0 {
		t.Errorf("expected 0 nodes for a syntax-error file, got %d", len(bad.Nodes))
	}
}

// TestPydanticGraphParser_MultiRootAmbiguous locks the codex-review P2 fix: a
// graph with multiple zero-in-degree roots and no explicit start is entry-
// ambiguous (pydantic-graph runs from any node), so the parser leaves the
// entry unset + flags EntryAmbiguous, and reachability SKIPS it rather than
// picking one root and reporting the other as an unreachable false positive.
func TestPydanticGraphParser_MultiRootAmbiguous(t *testing.T) {
	p := newPGParser(t)
	dir := findPydanticGraphTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "multi_root.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"A", "B"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("expected node %q (nodes=%v)", id, pgNodeIDs(graph))
		}
	}
	if !graph.EntryAmbiguous {
		t.Errorf("expected EntryAmbiguous=true for a two-root graph with no explicit start")
	}
	if graph.EntryNodeID != "" {
		t.Errorf("EntryNodeID must be empty when ambiguous, got %q", graph.EntryNodeID)
	}
	// Reachability must skip — no unreachable_node FP on the non-chosen root.
	for _, f := range rules.NewReachabilityChecker().Analyze(graph) {
		if f.RuleName == "unreachable_node" {
			t.Errorf("ambiguous-entry graph must not produce unreachable_node findings: %+v", f)
		}
	}
}
