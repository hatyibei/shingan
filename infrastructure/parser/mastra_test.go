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

// findMastraShim resolves the bundled Node shim relative to the test working
// directory, walking up. Mirrors findLangGraphJSShim.
func findMastraShim(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("infrastructure", "parser", "shims", "export_mastra_server.mjs")
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		p := filepath.Join(dir, rel)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate export_mastra_server.mjs from %q", dir)
		}
		dir = parent
	}
}

// findMastraTestdata returns testdata/mastra/, walking up.
func findMastraTestdata(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		p := filepath.Join(dir, "testdata", "mastra")
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate testdata/mastra from %q", dir)
		}
		dir = parent
	}
}

// requireNodeMastra skips the test when `node` is not on PATH. The Mastra shim
// is AST-only (it never imports @mastra/core), so node alone is enough.
func requireNodeMastra(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skipf("node not found in PATH: %v", err)
	}
}

func TestMastraParser_SupportedFormat(t *testing.T) {
	requireNodeMastra(t)
	p, err := parser.NewMastraParser(parser.WithMastraScriptPath(findMastraShim(t)))
	if err != nil {
		t.Fatalf("NewMastraParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if got := p.SupportedFormat(); got != "mastra" {
		t.Errorf("SupportedFormat() = %q, want %q", got, "mastra")
	}
}

func TestMastraParser_NodeUnavailable(t *testing.T) {
	_, err := parser.NewMastraParser(
		parser.WithMastraScriptPath(findMastraShim(t)),
		parser.WithMastraNodeBinary("node_does_not_exist_xyz_42"),
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

func TestMastraParser_LocateShimNamed(t *testing.T) {
	path, err := parser.LocateShimNamed("export_mastra_server.mjs")
	if err != nil {
		t.Fatalf("LocateShimNamed: %v", err)
	}
	if !strings.HasSuffix(path, "shims/export_mastra_server.mjs") {
		t.Errorf("path %q does not end in shims/export_mastra_server.mjs", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("located path does not exist: %v", err)
	}
}

// --- integration tests below require `node` on PATH (real shim) -------------

func newMastraParser(t *testing.T) *parser.MastraParser {
	t.Helper()
	requireNodeMastra(t)
	p, err := parser.NewMastraParser(parser.WithMastraScriptPath(findMastraShim(t)))
	if err != nil {
		t.Fatalf("NewMastraParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// TestMastraParser_Linear: a `.then(a).then(b).commit()` chain. Step ids come
// from createStep({ id }); the entry is the first step; edge a->b.
func TestMastraParser_Linear(t *testing.T) {
	p := newMastraParser(t)
	dir := findMastraTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "linear.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"a", "b"} {
		n, ok := graph.Nodes[id]
		if !ok {
			t.Fatalf("expected node %q (nodes=%v)", id, mastraNodeIDs(graph))
		}
		if n.Type != domain.NodeTypeTask {
			t.Errorf("node %q type = %q, want task", id, n.Type)
		}
	}
	if graph.EntryNodeID != "a" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "a")
	}
	if !mastraHasEdge(graph, "a", "b") {
		t.Errorf("expected edge a->b (edges=%v)", graph.Edges)
	}
	// No exit sentinel: Mastra ends at .commit(); HasExitBranch must stay false.
	if graph.Nodes["b"].HasExitBranch {
		t.Errorf("node b must NOT have HasExitBranch (Mastra has no exit sentinel)")
	}
}

// TestMastraParser_Branching: `.branch([[c,x],[c,y]])` fans out from the
// preceding step to both handlers. The conditions are JS closures we cannot
// read, so the edges carry an EMPTY condition.
func TestMastraParser_Branching(t *testing.T) {
	p := newMastraParser(t)
	dir := findMastraTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "branching.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"classify", "handleA", "handleB"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Fatalf("expected node %q (nodes=%v)", id, mastraNodeIDs(graph))
		}
	}
	if graph.EntryNodeID != "classify" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "classify")
	}
	for _, target := range []string{"handleA", "handleB"} {
		if !mastraHasEdge(graph, "classify", target) {
			t.Errorf("expected branch edge classify->%s (edges=%v)", target, graph.Edges)
		}
	}
	// Closures are unreadable: branch edges must carry an EMPTY condition (never
	// an invented label).
	for _, e := range graph.Edges {
		if e.From == "classify" && e.Condition != "" {
			t.Errorf("branch edge %s->%s carries condition %q; closures must yield empty Condition",
				e.From, e.To, e.Condition)
		}
	}
}

// TestMastraParser_LoopWithExit is the load-bearing acceptance test (ADR-015):
// a `.dountil(step, cond)` loop the workflow continues PAST. The cycle {poll}
// has a structural exit (poll->finalize), so cycle_detection must downgrade
// Critical -> Warning via cycleHasExit, WITHOUT any fake exit sentinel (Mastra
// has none).
func TestMastraParser_LoopWithExit(t *testing.T) {
	p := newMastraParser(t)
	dir := findMastraTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "loop_with_exit.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Layer 1 — parse structure. Failures here localise parse vs rule bugs.
	for _, id := range []string{"start", "poll", "finalize"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Fatalf("expected node %q (nodes=%v)", id, mastraNodeIDs(graph))
		}
	}
	if graph.EntryNodeID != "start" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "start")
	}
	if !mastraHasEdge(graph, "start", "poll") {
		t.Fatalf("expected edge start->poll (edges=%v)", graph.Edges)
	}
	// The self-loop back-edge (the .dountil repeat).
	if !mastraHasEdge(graph, "poll", "poll") {
		t.Fatalf("expected self-loop poll->poll (edges=%v)", graph.Edges)
	}
	// The STRUCTURAL exit edge leaving the cycle — load-bearing. Without it the
	// cycle would false-Critical (there is no sentinel to fall back on).
	if !mastraHasEdge(graph, "poll", "finalize") {
		t.Fatalf("expected structural exit edge poll->finalize (edges=%v); "+
			"without it cycle_detection would emit Critical", graph.Edges)
	}
	// No fake sentinel: confirm we did NOT synthesise an exit branch.
	if graph.Nodes["poll"].HasExitBranch {
		t.Fatalf("node poll must NOT have HasExitBranch — the downgrade must come " +
			"from the structural exit edge, not a fake sentinel")
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
		t.Fatalf("expected a cycle_detection finding for the loop, got none")
	}
	for _, f := range cycleFindings {
		if f.Severity == domain.Critical {
			t.Errorf("cycle_detection reported Critical for the structurally-exited loop; "+
				"want Warning (cycleHasExit must downgrade via poll->finalize): %+v", f)
		}
		if f.Severity != domain.Warning {
			t.Errorf("cycle_detection severity = %v, want Warning: %+v", f.Severity, f)
		}
	}
}

// TestMastraParser_LoopThenMapExit is the regression for the .dountil(...).map(...)
// bounded-loop false-positive (wild: hashintel/labs sgai-agent-planner
// planning-workflow.ts). The loop's continuation is a pass-through `.map(...)`,
// which emits no node, so NO structural exit edge can form — the loop step would
// degenerate to a lone self-loop and cycle.go would false-Critical a bounded
// loop. The shim flags has_exit_branch on the loop step (the pass-through exit
// has no materialisable step id), so cycle_detection must downgrade
// Critical -> Warning (NOT to zero).
func TestMastraParser_LoopThenMapExit(t *testing.T) {
	p := newMastraParser(t)
	dir := findMastraTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "loop_map_exit.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Layer 1 — parse structure. The pass-through exit emits no node, so the
	// only structural node/edge are the loop step and its self-loop.
	rev, ok := graph.Nodes["revise"]
	if !ok {
		t.Fatalf("expected node %q (nodes=%v)", "revise", mastraNodeIDs(graph))
	}
	if !mastraHasEdge(graph, "revise", "revise") {
		t.Fatalf("expected self-loop revise->revise (edges=%v)", graph.Edges)
	}
	// The .map() exit has no resolvable step id, so there is NO structural exit
	// edge — the downgrade must come from the has_exit_branch flag instead.
	if !rev.HasExitBranch {
		t.Fatalf("loop step revise must have HasExitBranch (the .map() exit has no " +
			"materialisable step id, so the structural-edge route is impossible)")
	}

	// Layer 2 — the acceptance criterion: bounded cycle => Warning, not Critical,
	// and NOT suppressed to zero.
	findings := rules.NewCycleDetector().Analyze(graph)
	var cycleFindings []domain.Finding
	for _, f := range findings {
		if f.RuleName == "cycle_detection" {
			cycleFindings = append(cycleFindings, f)
		}
	}
	if len(cycleFindings) == 0 {
		t.Fatalf("expected a cycle_detection finding for the bounded loop, got none")
	}
	for _, f := range cycleFindings {
		if f.Severity == domain.Critical {
			t.Errorf("cycle_detection reported Critical for the .dountil(...).map(...) "+
				"bounded loop; want Warning (has_exit_branch must downgrade): %+v", f)
		}
		if f.Severity != domain.Warning {
			t.Errorf("cycle_detection severity = %v, want Warning: %+v", f.Severity, f)
		}
	}
}

// TestMastraParser_LoopTerminalStaysCritical is the discriminating counterpart
// to TestMastraParser_LoopThenMapExit: it locks the NARROWNESS of the
// .dountil(...).map(...) fix. A genuinely chain-TERMINAL loop
// (`.dountil(poll, cond).commit()` — no continuation of ANY kind) must NOT be
// given a synthetic exit: has_exit_branch stays false and cycle_detection keeps
// Critical. Without this test, a later "simplification" that flags the loop step
// directly in the .dountil branch would silently mask a real unbounded loop and
// nothing would catch it.
func TestMastraParser_LoopTerminalStaysCritical(t *testing.T) {
	p := newMastraParser(t)
	dir := findMastraTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "loop_terminal.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	poll, ok := graph.Nodes["poll"]
	if !ok {
		t.Fatalf("expected node %q (nodes=%v)", "poll", mastraNodeIDs(graph))
	}
	if !mastraHasEdge(graph, "poll", "poll") {
		t.Fatalf("expected self-loop poll->poll (edges=%v)", graph.Edges)
	}
	// No continuation follows the loop — we invent NO exit (neither edge nor flag).
	if poll.HasExitBranch {
		t.Fatalf("terminal loop step poll must NOT have HasExitBranch — there is no " +
			"continuation, so no exit may be synthesised")
	}

	// The lone exit-less self-loop must stay Critical.
	findings := rules.NewCycleDetector().Analyze(graph)
	var cycleFindings []domain.Finding
	for _, f := range findings {
		if f.RuleName == "cycle_detection" {
			cycleFindings = append(cycleFindings, f)
		}
	}
	if len(cycleFindings) == 0 {
		t.Fatalf("expected a cycle_detection finding for the terminal loop, got none")
	}
	sawCritical := false
	for _, f := range cycleFindings {
		if f.Severity == domain.Critical {
			sawCritical = true
		}
	}
	if !sawCritical {
		t.Errorf("terminal exit-less loop must stay Critical; got %+v", cycleFindings)
	}
}

// TestMastraParser_NonMastraFile locks in the robustness contract: a .ts file
// with no createWorkflow usage (or a syntax error) yields an empty graph, never
// an error / worker crash. The worker must survive and parse a valid file next.
func TestMastraParser_NonMastraFile(t *testing.T) {
	p := newMastraParser(t)
	dir := findMastraTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "not_mastra.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(graph.Nodes) != 0 {
		t.Errorf("expected 0 nodes for a non-Mastra file, got %d: %v", len(graph.Nodes), mastraNodeIDs(graph))
	}

	// Syntax error → empty graph, worker stays alive.
	bad, err := p.Parse([]byte("const x = (=> {\n"))
	if err != nil {
		t.Fatalf("Parse(syntax error): %v", err)
	}
	if len(bad.Nodes) != 0 {
		t.Errorf("expected 0 nodes for a syntax-error file, got %d", len(bad.Nodes))
	}

	// Worker survives: a valid parse after the error still works.
	ok, err := p.Parse([]byte(
		"import { createStep, createWorkflow } from \"@mastra/core/workflows\";\n" +
			"const s1 = createStep({ id: \"s1\", execute: async () => ({}) });\n" +
			"const s2 = createStep({ id: \"s2\", execute: async () => ({}) });\n" +
			"export const wf = createWorkflow({ id: \"wf\" }).then(s1).then(s2).commit();\n",
	))
	if err != nil {
		t.Fatalf("Parse(valid after error): %v", err)
	}
	if !mastraHasEdge(ok, "s1", "s2") {
		t.Errorf("worker did not recover: expected edge s1->s2 (edges=%v)", ok.Edges)
	}
}

// TestMastraParser_AliasedImports locks the codex-review P2 fix: aliased Mastra
// imports (createStep as makeStep, createWorkflow as makeWorkflow) must still be
// recognised — they previously yielded an empty graph.
func TestMastraParser_AliasedImports(t *testing.T) {
	p := newMastraParser(t)
	dir := findMastraTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "aliased_imports.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"a", "b"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Fatalf("expected node %q from aliased imports (nodes=%v)", id, mastraNodeIDs(graph))
		}
	}
	if !mastraHasEdge(graph, "a", "b") {
		t.Errorf("expected edge a->b from the aliased createWorkflow chain (edges=%v)", graph.Edges)
	}
}

// TestMastraParser_LocalCreateStepNotMastra locks the codex-review P2 fix
// (false-positive half): local helpers named createStep/createWorkflow that are
// NOT imported from @mastra/* must NOT be parsed as a Mastra workflow.
func TestMastraParser_LocalCreateStepNotMastra(t *testing.T) {
	p := newMastraParser(t)
	dir := findMastraTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "local_create_step.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(graph.Nodes) != 0 || len(graph.Edges) != 0 {
		t.Errorf("non-@mastra createStep/createWorkflow must yield an empty graph, got %d nodes / %d edges: %v",
			len(graph.Nodes), len(graph.Edges), mastraNodeIDs(graph))
	}
}

// --- small assertion helpers ------------------------------------------------

func mastraNodeIDs(g *domain.WorkflowGraph) []string {
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	return ids
}

func mastraHasEdge(g *domain.WorkflowGraph, from, to string) bool {
	for _, e := range g.Edges {
		if e.From == from && e.To == to {
			return true
		}
	}
	return false
}
