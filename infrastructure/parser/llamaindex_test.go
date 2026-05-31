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

// findLlamaIndexShim resolves the bundled Python shim relative to the test
// working directory, walking up. Mirrors findPydanticGraphShim.
func findLlamaIndexShim(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join("infrastructure", "parser", "shims", "export_llamaindex_server.py"),
		filepath.Join("scripts", "export_llamaindex_server.py"),
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
			t.Fatalf("could not locate export_llamaindex_server.py from %q (looked in %v)", dir, candidates)
		}
		dir = parent
	}
}

// findLlamaIndexTestdata returns testdata/llamaindex/, walking up.
func findLlamaIndexTestdata(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		p := filepath.Join(dir, "testdata", "llamaindex")
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate testdata/llamaindex from %q", dir)
		}
		dir = parent
	}
}

func TestLlamaIndexParser_SupportedFormat(t *testing.T) {
	requirePython(t)
	p, err := parser.NewLlamaIndexParser(parser.WithLlamaIndexScriptPath(findLlamaIndexShim(t)))
	if err != nil {
		t.Fatalf("NewLlamaIndexParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if got := p.SupportedFormat(); got != "llamaindex" {
		t.Errorf("SupportedFormat() = %q, want %q", got, "llamaindex")
	}
}

func TestLlamaIndexParser_PythonUnavailable(t *testing.T) {
	_, err := parser.NewLlamaIndexParser(
		parser.WithLlamaIndexScriptPath(findLlamaIndexShim(t)),
		parser.WithLlamaIndexPythonBinary("python_does_not_exist_xyz_42"),
	)
	if err == nil {
		t.Fatal("expected error when python is not available")
	}
	if !strings.Contains(err.Error(), "not found in PATH") {
		t.Errorf("error %q does not mention PATH", err)
	}
}

func TestLlamaIndexParser_LocateShimNamed(t *testing.T) {
	path, err := parser.LocateShimNamed("export_llamaindex_server.py")
	if err != nil {
		t.Fatalf("LocateShimNamed: %v", err)
	}
	if !strings.HasSuffix(path, "shims/export_llamaindex_server.py") &&
		!strings.HasSuffix(path, "scripts/export_llamaindex_server.py") {
		t.Errorf("path %q does not end in shims/ or scripts/ export_llamaindex_server.py", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("located path does not exist: %v", err)
	}
}

// --- integration tests below this point require `python3` on PATH ----------
// The shim is AST-only (it never imports llama_index), so python3 alone is
// sufficient — there is intentionally NO `import llama_index` gate. Gating on
// the framework would silently SKIP the load-bearing cycle test wherever
// llama-index isn't installed, verifying nothing.

func newLIParser(t *testing.T) *parser.LlamaIndexParser {
	t.Helper()
	requirePython(t)
	p, err := parser.NewLlamaIndexParser(parser.WithLlamaIndexScriptPath(findLlamaIndexShim(t)))
	if err != nil {
		t.Fatalf("NewLlamaIndexParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func liHasEdge(g *domain.WorkflowGraph, from, to string) bool {
	for _, e := range g.Edges {
		if e.From == from && e.To == to {
			return true
		}
	}
	return false
}

func liNodeIDs(g *domain.WorkflowGraph) []string {
	ids := make([]string, 0, len(g.Nodes))
	for k := range g.Nodes {
		ids = append(ids, k)
	}
	return ids
}

func TestLlamaIndexParser_Linear(t *testing.T) {
	p := newLIParser(t)
	dir := findLlamaIndexTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "linear.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"retrieve", "synth"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("expected node %q (nodes=%v)", id, liNodeIDs(graph))
		}
	}
	// Entry = the StartEvent consumer.
	if graph.EntryNodeID != "retrieve" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "retrieve")
	}
	// StartEvent / StopEvent / MidEvent are never nodes.
	for _, sentinel := range []string{"StartEvent", "StopEvent", "MidEvent"} {
		if _, ok := graph.Nodes[sentinel]; ok {
			t.Errorf("%q should not be a node", sentinel)
		}
	}
	// retrieve produces MidEvent, synth consumes MidEvent → edge.
	if !liHasEdge(graph, "retrieve", "synth") {
		t.Errorf("expected edge retrieve->synth (edges=%v)", graph.Edges)
	}
	// synth returns StopEvent → exit branch; retrieve does not.
	if !graph.Nodes["synth"].HasExitBranch {
		t.Errorf("node synth should have HasExitBranch (-> StopEvent)")
	}
	if graph.Nodes["retrieve"].HasExitBranch {
		t.Errorf("node retrieve should NOT have HasExitBranch")
	}
	// Steps are emitted as NodeTypeTask.
	if graph.Nodes["retrieve"].Type != domain.NodeTypeTask {
		t.Errorf("node retrieve type = %v, want NodeTypeTask", graph.Nodes["retrieve"].Type)
	}
}

func TestLlamaIndexParser_Branching(t *testing.T) {
	p := newLIParser(t)
	dir := findLlamaIndexTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "branching.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"classify", "handle_a", "handle_b"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("expected node %q (nodes=%v)", id, liNodeIDs(graph))
		}
	}
	if graph.EntryNodeID != "classify" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "classify")
	}
	// The union return on classify fans out to each handler via its event type.
	if !liHasEdge(graph, "classify", "handle_a") {
		t.Errorf("expected edge classify->handle_a (edges=%v)", graph.Edges)
	}
	if !liHasEdge(graph, "classify", "handle_b") {
		t.Errorf("expected edge classify->handle_b (edges=%v)", graph.Edges)
	}
	// Both handlers return StopEvent → has_exit_branch.
	for _, id := range []string{"handle_a", "handle_b"} {
		if !graph.Nodes[id].HasExitBranch {
			t.Errorf("node %q should have HasExitBranch (-> StopEvent)", id)
		}
	}
}

// TestLlamaIndexParser_CycleWithStop_Warning is the load-bearing acceptance
// test (ADR-015), mirroring TestPydanticGraphParser_CycleWithEnd_Warning. The
// draft/critique/revise loop forms an SCC {critique, revise}; `critique`
// returns a union including StopEvent, so the shim sets HasExitBranch on a node
// that is IN the cycle. cycle_detection must downgrade Critical -> Warning.
func TestLlamaIndexParser_CycleWithStop_Warning(t *testing.T) {
	p := newLIParser(t)
	dir := findLlamaIndexTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "cycle_with_stop.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Layer 1 — parse structure. Failures here localise parse vs rule bugs.
	for _, id := range []string{"setup", "critique", "revise"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Fatalf("expected node %q (nodes=%v)", id, liNodeIDs(graph))
		}
	}
	// The back-edge revise->critique (via DraftEvent) closes the cycle. Without
	// it there is no cycle and the rule fires nothing.
	if !liHasEdge(graph, "critique", "revise") {
		t.Fatalf("expected edge critique->revise (ReviseEvent); edges=%v", graph.Edges)
	}
	if !liHasEdge(graph, "revise", "critique") {
		t.Fatalf("expected back-edge revise->critique (DraftEvent); edges=%v", graph.Edges)
	}
	// StopEvent in critique's union → exit branch on a node that is in the SCC.
	// Without it the cycle false-Criticals.
	if !graph.Nodes["critique"].HasExitBranch {
		t.Fatalf("node critique must have HasExitBranch (StopEvent in return union); " +
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
		t.Fatalf("expected a cycle_detection finding for the loop, got none")
	}
	for _, f := range cycleFindings {
		if f.Severity == domain.Critical {
			t.Errorf("cycle_detection reported Critical for the StopEvent-exit loop; want Warning "+
				"(HasExitBranch parity broken): %+v", f)
		}
		if f.Severity != domain.Warning {
			t.Errorf("cycle_detection severity = %v, want Warning: %+v", f.Severity, f)
		}
	}
}

// TestLlamaIndexParser_AmbiguousEntry locks the entry-ambiguity contract: when
// more than one step consumes StartEvent, the parser leaves the entry unset +
// flags EntryAmbiguous, and reachability SKIPS the graph rather than reporting
// the non-chosen entry as an unreachable false positive.
func TestLlamaIndexParser_AmbiguousEntry(t *testing.T) {
	p := newLIParser(t)
	dir := findLlamaIndexTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "ambiguous_entry.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"ingest_a", "ingest_b"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("expected node %q (nodes=%v)", id, liNodeIDs(graph))
		}
	}
	if !graph.EntryAmbiguous {
		t.Errorf("expected EntryAmbiguous=true for a graph with two StartEvent consumers")
	}
	if graph.EntryNodeID != "" {
		t.Errorf("EntryNodeID must be empty when ambiguous, got %q", graph.EntryNodeID)
	}
	// Reachability must skip — no unreachable_node FP on the non-chosen entry.
	for _, f := range rules.NewReachabilityChecker().Analyze(graph) {
		if f.RuleName == "unreachable_node" {
			t.Errorf("ambiguous-entry graph must not produce unreachable_node findings: %+v", f)
		}
	}
}

// TestLlamaIndexParser_FanIn exercises consumer-side union flattening: a step
// whose event param is `LeftEvent | RightEvent` consumes both, so it gets an
// in-edge from the producer of each (deduped to a single edge).
func TestLlamaIndexParser_FanIn(t *testing.T) {
	p := newLIParser(t)
	dir := findLlamaIndexTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "fan_in.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"split", "collect"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("expected node %q (nodes=%v)", id, liNodeIDs(graph))
		}
	}
	if graph.EntryNodeID != "split" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "split")
	}
	if !liHasEdge(graph, "split", "collect") {
		t.Errorf("expected edge split->collect via union param fan-in (edges=%v)", graph.Edges)
	}
	// Deduped: split->collect appears exactly once despite two shared events.
	n := 0
	for _, e := range graph.Edges {
		if e.From == "split" && e.To == "collect" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected exactly one split->collect edge (deduped), got %d (edges=%v)", n, graph.Edges)
	}
}

// TestLlamaIndexParser_BareEvent locks the omit-don't-invent contract: steps
// annotated with the base `Event` type carry no routable type info, so NO
// inter-step edges may be fabricated — otherwise a `b -> b` self-loop would
// false-Critical via cycle_detection.
func TestLlamaIndexParser_BareEvent(t *testing.T) {
	p := newLIParser(t)
	dir := findLlamaIndexTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "bare_event.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"a", "b", "c"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("expected node %q (nodes=%v)", id, liNodeIDs(graph))
		}
	}
	// Bare Event must NOT create any inter-step edge.
	if len(graph.Edges) != 0 {
		t.Errorf("expected 0 edges for bare-Event steps (omit-don't-invent), got %v", graph.Edges)
	}
	// The StopEvent on c is still recognised as an exit sentinel.
	if !graph.Nodes["c"].HasExitBranch {
		t.Errorf("node c should have HasExitBranch (-> StopEvent)")
	}
	// No fabricated self-loop → cycle_detection must report nothing.
	for _, f := range rules.NewCycleDetector().Analyze(graph) {
		if f.RuleName == "cycle_detection" {
			t.Errorf("bare-Event graph must not produce a cycle_detection finding: %+v", f)
		}
	}
}

// TestLlamaIndexParser_NonWorkflowFile locks in the robustness contract: a .py
// file with no Workflow subclass (or a syntax error) yields an empty graph,
// never an error / worker crash.
func TestLlamaIndexParser_NonWorkflowFile(t *testing.T) {
	p := newLIParser(t)

	graph, err := p.Parse([]byte("import os\n\ndef hello():\n    return 1\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(graph.Nodes) != 0 {
		t.Errorf("expected 0 nodes for a non-workflow file, got %d: %v", len(graph.Nodes), liNodeIDs(graph))
	}

	// Syntax error → empty graph, worker stays alive (next call still works).
	bad, err := p.Parse([]byte("def broken(:\n"))
	if err != nil {
		t.Fatalf("Parse(syntax error): %v", err)
	}
	if len(bad.Nodes) != 0 {
		t.Errorf("expected 0 nodes for a syntax-error file, got %d", len(bad.Nodes))
	}

	// Worker survived: a real workflow still parses after the bad inputs.
	good, err := p.Parse([]byte(
		"from llama_index.core.workflow import StartEvent, StopEvent, Workflow, step\n" +
			"class W(Workflow):\n" +
			"    @step\n" +
			"    async def run(self, ev: StartEvent) -> StopEvent:\n" +
			"        return StopEvent(result=1)\n",
	))
	if err != nil {
		t.Fatalf("Parse(valid after errors): %v", err)
	}
	if _, ok := good.Nodes["run"]; !ok {
		t.Errorf("expected node run after worker recovered (nodes=%v)", liNodeIDs(good))
	}
}

// TestLlamaIndexParser_AliasedImports locks the codex-review P2 fix: aliased
// framework imports (Workflow as WF, step as li_step, StartEvent/StopEvent
// aliased) must still be recognised — they previously yielded an empty graph.
func TestLlamaIndexParser_AliasedImports(t *testing.T) {
	p := newLIParser(t)
	dir := findLlamaIndexTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "aliased_imports.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"begin", "finish"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Fatalf("expected node %q despite aliased imports (nodes=%v)", id, liNodeIDs(graph))
		}
	}
	if !liHasEdge(graph, "begin", "finish") {
		t.Errorf("expected edge begin->finish via the (non-aliased) MidEvent (edges=%v)", graph.Edges)
	}
	if graph.EntryNodeID != "begin" {
		t.Errorf("EntryNodeID = %q, want begin (consumes aliased StartEvent)", graph.EntryNodeID)
	}
	if graph.Nodes["finish"] == nil || !graph.Nodes["finish"].HasExitBranch {
		t.Errorf("finish should have HasExitBranch (returns aliased StopEvent)")
	}
}

// TestLlamaIndexParser_KeywordOnlyEvent locks the codex-review P2 fix: a step
// declaring its event keyword-only (`async def run(self, *, ev: StartEvent)`)
// must still be located, so the entry resolves and is not ambiguous.
func TestLlamaIndexParser_KeywordOnlyEvent(t *testing.T) {
	p := newLIParser(t)
	dir := findLlamaIndexTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "kwonly_event.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if graph.EntryNodeID != "run" {
		t.Errorf("EntryNodeID = %q, want run (keyword-only StartEvent param)", graph.EntryNodeID)
	}
	if graph.EntryAmbiguous {
		t.Errorf("entry must not be ambiguous when the kwonly StartEvent consumer is found")
	}
	if graph.Nodes["run"] == nil || !graph.Nodes["run"].HasExitBranch {
		t.Errorf("run should have HasExitBranch (returns StopEvent)")
	}
}
