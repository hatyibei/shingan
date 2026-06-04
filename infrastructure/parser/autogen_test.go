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

// findAutoGenShim resolves the bundled Python shim relative to the test working
// directory, walking up. Mirrors findPydanticGraphShim.
func findAutoGenShim(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join("infrastructure", "parser", "shims", "export_autogen_server.py"),
		filepath.Join("scripts", "export_autogen_server.py"),
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
			t.Fatalf("could not locate export_autogen_server.py from %q (looked in %v)", dir, candidates)
		}
		dir = parent
	}
}

// findAutoGenTestdata returns testdata/autogen/, walking up.
func findAutoGenTestdata(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		p := filepath.Join(dir, "testdata", "autogen")
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate testdata/autogen from %q", dir)
		}
		dir = parent
	}
}

func TestAutoGenParser_SupportedFormat(t *testing.T) {
	requirePython(t)
	p, err := parser.NewAutoGenParser(parser.WithAutoGenScriptPath(findAutoGenShim(t)))
	if err != nil {
		t.Fatalf("NewAutoGenParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if got := p.SupportedFormat(); got != "autogen" {
		t.Errorf("SupportedFormat() = %q, want %q", got, "autogen")
	}
}

func TestAutoGenParser_PythonUnavailable(t *testing.T) {
	_, err := parser.NewAutoGenParser(
		parser.WithAutoGenScriptPath(findAutoGenShim(t)),
		parser.WithAutoGenPythonBinary("python_does_not_exist_xyz_42"),
	)
	if err == nil {
		t.Fatal("expected error when python is not available")
	}
	if !strings.Contains(err.Error(), "not found in PATH") {
		t.Errorf("error %q does not mention PATH", err)
	}
}

func TestAutoGenParser_LocateShimNamed(t *testing.T) {
	path, err := parser.LocateShimNamed("export_autogen_server.py")
	if err != nil {
		t.Fatalf("LocateShimNamed: %v", err)
	}
	if !strings.HasSuffix(path, "shims/export_autogen_server.py") &&
		!strings.HasSuffix(path, "scripts/export_autogen_server.py") {
		t.Errorf("path %q does not end in shims/ or scripts/ export_autogen_server.py", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("located path does not exist: %v", err)
	}
}

// --- integration tests below this point require `python3` on PATH ----------
// The shim is AST-only (it never imports autogen_agentchat), so python3 alone
// is sufficient — there is intentionally NO `import autogen_agentchat` gate.
// Gating on the framework would silently SKIP the load-bearing cycle test
// wherever AutoGen isn't installed, verifying nothing.

func newAutoGenParser(t *testing.T) *parser.AutoGenParser {
	t.Helper()
	requirePython(t)
	p, err := parser.NewAutoGenParser(parser.WithAutoGenScriptPath(findAutoGenShim(t)))
	if err != nil {
		t.Fatalf("NewAutoGenParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func agHasEdge(g *domain.WorkflowGraph, from, to string) bool {
	for _, e := range g.Edges {
		if e.From == from && e.To == to {
			return true
		}
	}
	return false
}

func agEdgeCondition(g *domain.WorkflowGraph, from, to string) (string, bool) {
	for _, e := range g.Edges {
		if e.From == from && e.To == to {
			return e.Condition, true
		}
	}
	return "", false
}

func agNodeIDs(g *domain.WorkflowGraph) []string {
	ids := make([]string, 0, len(g.Nodes))
	for k := range g.Nodes {
		ids = append(ids, k)
	}
	return ids
}

func TestAutoGenParser_Linear(t *testing.T) {
	p := newAutoGenParser(t)
	dir := findAutoGenTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "linear.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// Nodes are the resolved agent name= strings, not the variable names.
	for _, id := range []string{"researcher", "writer", "editor"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("expected node %q (nodes=%v)", id, agNodeIDs(graph))
		}
	}
	if graph.EntryNodeID != "researcher" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "researcher")
	}
	if !agHasEdge(graph, "researcher", "writer") {
		t.Errorf("expected edge researcher->writer (edges=%v)", graph.Edges)
	}
	if !agHasEdge(graph, "writer", "editor") {
		t.Errorf("expected edge writer->editor (edges=%v)", graph.Edges)
	}
	// No exit sentinel — AutoGen terminates externally. No node carries an
	// exit branch; cycle bounding (where applicable) is structural.
	for _, id := range []string{"researcher", "writer", "editor"} {
		if graph.Nodes[id].HasExitBranch {
			t.Errorf("node %q must NOT have HasExitBranch (AutoGen has no in-graph sentinel)", id)
		}
	}
	// Nodes are emitted as NodeTypeTask (leaf agent units; not a Loop node).
	if graph.Nodes["researcher"].Type != domain.NodeTypeTask {
		t.Errorf("node researcher type = %v, want NodeTypeTask", graph.Nodes["researcher"].Type)
	}
}

func TestAutoGenParser_Branching(t *testing.T) {
	p := newAutoGenParser(t)
	dir := findAutoGenTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "branching.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"router", "billing", "tech"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("expected node %q (nodes=%v)", id, agNodeIDs(graph))
		}
	}
	if graph.EntryNodeID != "router" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "router")
	}
	// Conditional edges still produce A->B edges.
	if !agHasEdge(graph, "router", "billing") {
		t.Errorf("expected edge router->billing (edges=%v)", graph.Edges)
	}
	if !agHasEdge(graph, "router", "tech") {
		t.Errorf("expected edge router->tech (edges=%v)", graph.Edges)
	}
	// The static condition keyword is carried on Edge.Condition.
	if cond, ok := agEdgeCondition(graph, "router", "billing"); !ok || cond != "billing" {
		t.Errorf("edge router->billing condition = %q (ok=%v), want %q", cond, ok, "billing")
	}
	if cond, ok := agEdgeCondition(graph, "router", "tech"); !ok || cond != "technical" {
		t.Errorf("edge router->tech condition = %q (ok=%v), want %q", cond, ok, "technical")
	}
}

// TestAutoGenParser_CycleWithExit_Warning is the load-bearing acceptance test
// (ADR-015). The cycle {generator, critic} has a STRUCTURAL exit edge
// (critic -> finalizer) leaving the cycle to a real node. shingan's
// cycle_detection must downgrade Critical -> Warning via cycleHasExit, WITHOUT
// any fake exit sentinel (AutoGen has none — termination is external).
func TestAutoGenParser_CycleWithExit_Warning(t *testing.T) {
	p := newAutoGenParser(t)
	dir := findAutoGenTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "cycle_with_exit.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Layer 1 — parse structure. Failures here localise parse vs rule bugs.
	for _, id := range []string{"generator", "critic", "finalizer"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Fatalf("expected node %q (nodes=%v)", id, agNodeIDs(graph))
		}
	}
	// The back-edge that closes the cycle.
	if !agHasEdge(graph, "critic", "generator") {
		t.Fatalf("expected back-edge critic->generator (edges=%v)", graph.Edges)
	}
	if !agHasEdge(graph, "generator", "critic") {
		t.Fatalf("expected edge generator->critic (edges=%v)", graph.Edges)
	}
	// The STRUCTURAL exit edge leaving the cycle — load-bearing. Without it
	// the cycle would false-Critical (there is no sentinel to fall back on).
	if !agHasEdge(graph, "critic", "finalizer") {
		t.Fatalf("expected structural exit edge critic->finalizer (edges=%v); "+
			"without it cycle_detection would emit Critical", graph.Edges)
	}
	// No fake sentinel: confirm we did NOT synthesise an exit branch.
	if graph.Nodes["critic"].HasExitBranch {
		t.Fatalf("node critic must NOT have HasExitBranch — the downgrade must come " +
			"from the structural exit edge, not a fake sentinel")
	}
	// set_entry_point(generator) pins the entry even though generator has an
	// incoming edge (critic -> generator).
	if graph.EntryNodeID != "generator" {
		t.Errorf("EntryNodeID = %q, want %q (set_entry_point)", graph.EntryNodeID, "generator")
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
				"want Warning (cycleHasExit must downgrade via critic->finalizer): %+v", f)
		}
		if f.Severity != domain.Warning {
			t.Errorf("cycle_detection severity = %v, want Warning: %+v", f.Severity, f)
		}
	}
}

// TestAutoGenParser_PureLoop_Critical documents the accepted PoC modelling
// boundary: a cycle with NO structural exit edge (relying solely on AutoGen's
// external termination) stays Critical. This is correct — a graph-level
// unbounded loop is worth surfacing.
func TestAutoGenParser_PureLoop_Critical(t *testing.T) {
	p := newAutoGenParser(t)
	dir := findAutoGenTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "pure_loop.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if !agHasEdge(graph, "generator", "critic") || !agHasEdge(graph, "critic", "generator") {
		t.Fatalf("expected a 2-node cycle generator<->critic (edges=%v)", graph.Edges)
	}
	findings := rules.NewCycleDetector().Analyze(graph)
	var sawCritical bool
	for _, f := range findings {
		if f.RuleName == "cycle_detection" && f.Severity == domain.Critical {
			sawCritical = true
		}
	}
	if !sawCritical {
		t.Errorf("expected a Critical cycle_detection finding for the pure loop "+
			"(no structural exit); findings=%v", findings)
	}
}

// TestAutoGenParser_NonAutoGenFile locks in the robustness contract: a .py
// file with no DiGraphBuilder usage (or a syntax error) yields an empty graph,
// never an error / worker crash.
func TestAutoGenParser_NonAutoGenFile(t *testing.T) {
	p := newAutoGenParser(t)

	graph, err := p.Parse([]byte("import os\n\ndef hello():\n    return 1\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(graph.Nodes) != 0 {
		t.Errorf("expected 0 nodes for a non-autogen file, got %d: %v", len(graph.Nodes), agNodeIDs(graph))
	}

	// Syntax error → empty graph, worker stays alive (next call still works).
	bad, err := p.Parse([]byte("def broken(:\n"))
	if err != nil {
		t.Fatalf("Parse(syntax error): %v", err)
	}
	if len(bad.Nodes) != 0 {
		t.Errorf("expected 0 nodes for a syntax-error file, got %d", len(bad.Nodes))
	}

	// Worker survives: a valid parse after the error still works.
	ok, err := p.Parse([]byte(
		"from autogen_agentchat.agents import AssistantAgent\n" +
			"from autogen_agentchat.teams import DiGraphBuilder\n" +
			"a = AssistantAgent(name=\"alpha\")\n" +
			"b = AssistantAgent(name=\"beta\")\n" +
			"builder = DiGraphBuilder()\n" +
			"builder.add_node(a)\n" +
			"builder.add_node(b)\n" +
			"builder.add_edge(a, b)\n",
	))
	if err != nil {
		t.Fatalf("Parse(valid after error): %v", err)
	}
	if _, ok2 := ok.Nodes["alpha"]; !ok2 {
		t.Errorf("worker did not survive prior errors: expected node alpha, nodes=%v", agNodeIDs(ok))
	}
}

// TestAutoGenParser_NetworkXNotAutoGen locks the codex-review P1 fix: a file
// using the same builder-pattern method names on a NON-DiGraphBuilder receiver
// (NetworkX) must NOT be mistaken for an AutoGen graph — no phantom nodes, no
// false cycle.
func TestAutoGenParser_NetworkXNotAutoGen(t *testing.T) {
	p := newAutoGenParser(t)
	dir := findAutoGenTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "networkx_not_autogen.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(graph.Nodes) != 0 {
		t.Errorf("NetworkX code must not yield AutoGen nodes, got %d: %v", len(graph.Nodes), agNodeIDs(graph))
	}
	if len(graph.Edges) != 0 {
		t.Errorf("NetworkX code must not yield AutoGen edges, got %d: %v", len(graph.Edges), graph.Edges)
	}
}

// TestAutoGenParser_LoopAddNode_NoPhantomNode locks the wild-dogfood fix
// (hugocool/FateForger): the “for agent in (n1, n2, ...): builder.add_node(agent)“
// idiom must NOT register a phantom node literally named after the loop
// variable. The loop variable is an iteration placeholder, never bound to an
// agent ctor, so it is dropped — previously it surfaced as a false
// “unreachable_node“ finding.
func TestAutoGenParser_LoopAddNode_NoPhantomNode(t *testing.T) {
	p := newAutoGenParser(t)
	dir := findAutoGenTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "loop_add_node.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// The loop control variable must NOT become a node.
	if _, ok := graph.Nodes["agent"]; ok {
		t.Errorf("phantom loop-variable node \"agent\" must NOT be registered (nodes=%v)", agNodeIDs(graph))
	}
	// The real nodes (added through the loop variable + referenced by edges)
	// are still present.
	for _, id := range []string{"hydrate", "assess", "plan"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("expected real node %q (nodes=%v)", id, agNodeIDs(graph))
		}
	}
	if len(graph.Nodes) != 3 {
		t.Errorf("expected exactly 3 nodes, got %d: %v", len(graph.Nodes), agNodeIDs(graph))
	}
	if !agHasEdge(graph, "hydrate", "assess") || !agHasEdge(graph, "assess", "plan") {
		t.Errorf("expected edges hydrate->assess->plan (edges=%v)", graph.Edges)
	}
	if graph.EntryNodeID != "hydrate" {
		t.Errorf("EntryNodeID = %q, want hydrate (set_entry_point)", graph.EntryNodeID)
	}
}

// TestAutoGenParser_ClassSelfAttr_Recovered locks the wild-dogfood fix
// (Austinggg/CreAgentive): the canonical class-based idiom holds each agent on
// an instance attribute (“self.user_proxy“) and registers it via
// “builder.add_node(self.user_proxy)“. The “self.<attr>“ reference must
// resolve to the trailing attribute name instead of returning None —
// previously every add_node/add_edge early-returned and the ENTIRE class-based
// graph was dropped (false negative). Non-agent attrs (“self.model_client“,
// “self.graph_flow“) that are never graph arguments must NOT become nodes.
func TestAutoGenParser_ClassSelfAttr_Recovered(t *testing.T) {
	p := newAutoGenParser(t)
	dir := findAutoGenTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "class_self_attr.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// The whole graph is recovered from self.<attr> references.
	for _, id := range []string{"user_proxy", "extractor", "validator", "structurer"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("expected node %q recovered from self.<attr> (nodes=%v)", id, agNodeIDs(graph))
		}
	}
	// Non-agent instance attributes never passed to add_node/add_edge must not
	// be fabricated as nodes.
	for _, id := range []string{"model_client", "graph_flow", "graph"} {
		if _, ok := graph.Nodes[id]; ok {
			t.Errorf("non-agent attr %q must NOT become a node (nodes=%v)", id, agNodeIDs(graph))
		}
	}
	if len(graph.Nodes) != 4 {
		t.Errorf("expected exactly 4 nodes, got %d: %v", len(graph.Nodes), agNodeIDs(graph))
	}
	// Edges, including the conditional back-edge that closes the cycle and the
	// structural exit leaving it.
	if !agHasEdge(graph, "user_proxy", "extractor") || !agHasEdge(graph, "extractor", "validator") {
		t.Errorf("expected edges user_proxy->extractor->validator (edges=%v)", graph.Edges)
	}
	if !agHasEdge(graph, "validator", "user_proxy") {
		t.Errorf("expected conditional back-edge validator->user_proxy (edges=%v)", graph.Edges)
	}
	if !agHasEdge(graph, "validator", "structurer") {
		t.Errorf("expected structural exit edge validator->structurer (edges=%v)", graph.Edges)
	}
	if cond, ok := agEdgeCondition(graph, "validator", "user_proxy"); !ok || cond != "incomplete" {
		t.Errorf("edge validator->user_proxy condition = %q (ok=%v), want %q", cond, ok, "incomplete")
	}
	if graph.EntryNodeID != "user_proxy" {
		t.Errorf("EntryNodeID = %q, want user_proxy (set_entry_point)", graph.EntryNodeID)
	}
}

// TestAutoGenParser_KeywordEdges locks the codex-review P2 fix: builder methods
// called with keyword args (add_node(node=..), add_edge(source=.., target=..),
// set_entry_point(node=..)) must still be parsed.
func TestAutoGenParser_KeywordEdges(t *testing.T) {
	p := newAutoGenParser(t)
	dir := findAutoGenTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "kwarg_edges.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"planner", "worker", "reviewer"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("expected node %q from kwarg add_node (nodes=%v)", id, agNodeIDs(graph))
		}
	}
	if !agHasEdge(graph, "planner", "worker") || !agHasEdge(graph, "worker", "reviewer") {
		t.Errorf("expected kwarg edges planner->worker->reviewer (edges=%v)", graph.Edges)
	}
	if graph.EntryNodeID != "planner" {
		t.Errorf("EntryNodeID = %q, want planner (kwarg set_entry_point)", graph.EntryNodeID)
	}
}

// TestAutoGenParser_LoopVarShadowKeepsRealNode guards the codex #36 refinement:
// a for-loop control variable ("for agent in ...:") that is never add_node'd in
// that loop must NOT suppress a real builder node that happens to share the name
// ("agent" added via the bare-name fallback elsewhere). Module-wide loop-target
// suppression would wrongly drop the real node.
func TestAutoGenParser_LoopVarShadowKeepsRealNode(t *testing.T) {
	p := newAutoGenParser(t)
	dir := findAutoGenTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "loop_var_shadow.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"agent", "helper"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("expected node %q to survive (nodes=%v)", id, agNodeIDs(graph))
		}
	}
}
