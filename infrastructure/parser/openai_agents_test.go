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

// findOpenAIAgentsShim resolves the bundled Node shim relative to the test
// working directory, walking up. Mirrors findMastraShim.
func findOpenAIAgentsShim(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("infrastructure", "parser", "shims", "export_openai_agents_server.mjs")
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
			t.Fatalf("could not locate export_openai_agents_server.mjs from %q", dir)
		}
		dir = parent
	}
}

// findOpenAIAgentsTestdata returns testdata/openaiagents/, walking up.
func findOpenAIAgentsTestdata(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		p := filepath.Join(dir, "testdata", "openaiagents")
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate testdata/openaiagents from %q", dir)
		}
		dir = parent
	}
}

// requireNodeOpenAIAgents skips the test when `node` is not on PATH. The shim is
// AST-only (it never imports @openai/agents), so node alone is enough.
func requireNodeOpenAIAgents(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skipf("node not found in PATH: %v", err)
	}
}

func TestOpenAIAgentsParser_SupportedFormat(t *testing.T) {
	requireNodeOpenAIAgents(t)
	p, err := parser.NewOpenAIAgentsParser(parser.WithOpenAIAgentsScriptPath(findOpenAIAgentsShim(t)))
	if err != nil {
		t.Fatalf("NewOpenAIAgentsParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if got := p.SupportedFormat(); got != "openai-agents" {
		t.Errorf("SupportedFormat() = %q, want %q", got, "openai-agents")
	}
}

func TestOpenAIAgentsParser_NodeUnavailable(t *testing.T) {
	_, err := parser.NewOpenAIAgentsParser(
		parser.WithOpenAIAgentsScriptPath(findOpenAIAgentsShim(t)),
		parser.WithOpenAIAgentsNodeBinary("node_does_not_exist_xyz_42"),
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

func TestOpenAIAgentsParser_LocateShimNamed(t *testing.T) {
	path, err := parser.LocateShimNamed("export_openai_agents_server.mjs")
	if err != nil {
		t.Fatalf("LocateShimNamed: %v", err)
	}
	if !strings.HasSuffix(path, "shims/export_openai_agents_server.mjs") {
		t.Errorf("path %q does not end in shims/export_openai_agents_server.mjs", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("located path does not exist: %v", err)
	}
}

// --- integration tests below require `node` on PATH (real shim) -------------

func newOAParser(t *testing.T) *parser.OpenAIAgentsParser {
	t.Helper()
	requireNodeOpenAIAgents(t)
	p, err := parser.NewOpenAIAgentsParser(parser.WithOpenAIAgentsScriptPath(findOpenAIAgentsShim(t)))
	if err != nil {
		t.Fatalf("NewOpenAIAgentsParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// TestOpenAIAgentsParser_Linear: a Triage agent (via Agent.create) handing off
// to two specialists. Node ids come from each agent's `name` config (normalised:
// "Triage Agent" -> "Triage_Agent"); the entry is the zero-in-degree Triage
// agent; edges Triage->Booking and Triage->Refund (the `handoff()` wrapper is
// unwrapped). No exit sentinel: HasExitBranch must stay false.
func TestOpenAIAgentsParser_Linear(t *testing.T) {
	p := newOAParser(t)
	dir := findOpenAIAgentsTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "linear.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"Triage_Agent", "Booking_Agent", "Refund_Agent"} {
		n, ok := graph.Nodes[id]
		if !ok {
			t.Fatalf("expected node %q (nodes=%v)", id, oaNodeIDs(graph))
		}
		if n.Type != domain.NodeTypeTask {
			t.Errorf("node %q type = %q, want task", id, n.Type)
		}
		if n.HasExitBranch {
			t.Errorf("node %q must NOT have HasExitBranch (OpenAI Agents has no exit sentinel)", id)
		}
	}
	if graph.EntryNodeID != "Triage_Agent" {
		t.Errorf("EntryNodeID = %q, want %q (the zero-in-degree triage agent)", graph.EntryNodeID, "Triage_Agent")
	}
	for _, target := range []string{"Booking_Agent", "Refund_Agent"} {
		if !oaHasEdge(graph, "Triage_Agent", target) {
			t.Errorf("expected handoff edge Triage_Agent->%s (edges=%v)", target, graph.Edges)
		}
	}
}

// TestOpenAIAgentsParser_CircularCritical is a load-bearing acceptance test
// (ADR-015): a PURE handoff loop A<->B with no agent leaving the cycle. OpenAI
// Agents has no exit sentinel, so there is nothing to downgrade the cycle —
// cycle_detection must report CRITICAL (an unbounded handoff loop), and no node
// may carry a fabricated HasExitBranch.
func TestOpenAIAgentsParser_CircularCritical(t *testing.T) {
	p := newOAParser(t)
	dir := findOpenAIAgentsTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "circular_critical.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	for _, id := range []string{"A", "B"} {
		n, ok := graph.Nodes[id]
		if !ok {
			t.Fatalf("expected node %q (nodes=%v)", id, oaNodeIDs(graph))
		}
		if n.HasExitBranch {
			t.Fatalf("node %q must NOT have HasExitBranch — a pure handoff loop has no exit "+
				"and no sentinel exists to invent one", id)
		}
	}
	// Both handoff directions resolved (forward reference is fine for the AST).
	if !oaHasEdge(graph, "A", "B") || !oaHasEdge(graph, "B", "A") {
		t.Fatalf("expected mutual edges A<->B (edges=%v)", graph.Edges)
	}

	findings := rules.NewCycleDetector().Analyze(graph)
	cycleFindings := oaCycleFindings(findings)
	if len(cycleFindings) == 0 {
		t.Fatalf("expected a cycle_detection finding for the pure loop, got none")
	}
	sawCritical := false
	for _, f := range cycleFindings {
		if f.Severity == domain.Critical {
			sawCritical = true
		}
	}
	if !sawCritical {
		t.Errorf("pure exit-less handoff loop must stay Critical; got %+v", cycleFindings)
	}
}

// TestOpenAIAgentsParser_CircularWarning is the discriminating counterpart: a
// handoff loop A<->B where the in-cycle agent A ALSO hands off OUT of the cycle
// to a terminal agent C. The structural exit edge A->C lets cycleHasExit
// downgrade Critical -> Warning, WITHOUT any exit sentinel (has_exit_branch
// stays false). This locks the no-sentinel cycle semantics for the framework.
func TestOpenAIAgentsParser_CircularWarning(t *testing.T) {
	p := newOAParser(t)
	dir := findOpenAIAgentsTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "circular_warning.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	for _, id := range []string{"A", "B", "C"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Fatalf("expected node %q (nodes=%v)", id, oaNodeIDs(graph))
		}
	}
	// The cycle plus the structural exit edge.
	if !oaHasEdge(graph, "A", "B") || !oaHasEdge(graph, "B", "A") {
		t.Fatalf("expected mutual edges A<->B (edges=%v)", graph.Edges)
	}
	if !oaHasEdge(graph, "A", "C") {
		t.Fatalf("expected structural exit edge A->C leaving the cycle (edges=%v); "+
			"without it cycle_detection would emit Critical", graph.Edges)
	}
	// No fake sentinel: the downgrade must come from the structural edge.
	for _, id := range []string{"A", "B", "C"} {
		if graph.Nodes[id].HasExitBranch {
			t.Fatalf("node %q must NOT have HasExitBranch — the downgrade must come from the "+
				"structural exit edge A->C, not a fake sentinel", id)
		}
	}

	findings := rules.NewCycleDetector().Analyze(graph)
	cycleFindings := oaCycleFindings(findings)
	if len(cycleFindings) == 0 {
		t.Fatalf("expected a cycle_detection finding for the loop, got none")
	}
	for _, f := range cycleFindings {
		if f.Severity == domain.Critical {
			t.Errorf("cycle_detection reported Critical for the structurally-exited loop; "+
				"want Warning (cycleHasExit must downgrade via A->C): %+v", f)
		}
		if f.Severity != domain.Warning {
			t.Errorf("cycle_detection severity = %v, want Warning: %+v", f.Severity, f)
		}
	}
}

// TestOpenAIAgentsParser_AliasedImports locks the alias-aware import handling:
// `Agent as Ag` / `handoff as h` must still be recognised (both `new Ag(...)`
// and `Ag.create(...)`), yielding Triage -> Specialist.
func TestOpenAIAgentsParser_AliasedImports(t *testing.T) {
	p := newOAParser(t)
	dir := findOpenAIAgentsTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "aliased_imports.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"Triage", "Specialist"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Fatalf("expected node %q from aliased imports (nodes=%v)", id, oaNodeIDs(graph))
		}
	}
	if !oaHasEdge(graph, "Triage", "Specialist") {
		t.Errorf("expected edge Triage->Specialist from the aliased handoff (edges=%v)", graph.Edges)
	}
	if graph.EntryNodeID != "Triage" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "Triage")
	}
}

// TestOpenAIAgentsParser_DestNotDeclared locks the dest-must-be-declared gate: a
// handoff to an agent imported from another module yields NO edge and NO phantom
// node. Only the local declared handoff survives.
func TestOpenAIAgentsParser_DestNotDeclared(t *testing.T) {
	p := newOAParser(t)
	dir := findOpenAIAgentsTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "dest_not_declared.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// Exactly the two declared agents — no phantom node for externalAgent.
	if len(graph.Nodes) != 2 {
		t.Errorf("expected exactly 2 nodes (no phantom), got %d: %v", len(graph.Nodes), oaNodeIDs(graph))
	}
	for _, id := range []string{"Triage", "Local"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Fatalf("expected node %q (nodes=%v)", id, oaNodeIDs(graph))
		}
	}
	if _, ok := graph.Nodes["externalAgent"]; ok {
		t.Errorf("phantom node externalAgent must NOT exist")
	}
	// The declared handoff survives.
	if !oaHasEdge(graph, "Local", "Triage") {
		t.Errorf("expected edge Local->Triage (edges=%v)", graph.Edges)
	}
	// No edge to the unresolved imported target.
	for _, e := range graph.Edges {
		if e.To == "externalAgent" || e.To == "external_agent" {
			t.Errorf("fabricated edge to unresolved import: %v", e)
		}
	}
	if len(graph.Edges) != 1 {
		t.Errorf("expected exactly 1 edge (Local->Triage), got %d: %v", len(graph.Edges), graph.Edges)
	}
}

// TestOpenAIAgentsParser_AgentsAsTools locks the optional agents-as-tools edges:
// `child.asTool({...})` in a parent's `tools` array, when the receiver resolves
// to a declared agent, becomes a directed edge parent->child.
func TestOpenAIAgentsParser_AgentsAsTools(t *testing.T) {
	p := newOAParser(t)
	dir := findOpenAIAgentsTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "agents_as_tools.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"Orchestrator", "Spanish", "French"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Fatalf("expected node %q (nodes=%v)", id, oaNodeIDs(graph))
		}
	}
	for _, target := range []string{"Spanish", "French"} {
		if !oaHasEdge(graph, "Orchestrator", target) {
			t.Errorf("expected agents-as-tools edge Orchestrator->%s (edges=%v)", target, graph.Edges)
		}
	}
	if graph.EntryNodeID != "Orchestrator" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "Orchestrator")
	}
}

// TestOpenAIAgentsParser_NonAgentsFile locks the robustness contract: a .ts file
// with a LOCAL `Agent` class (not imported from @openai/agents) yields an empty
// graph, and a syntax-error file does too — the worker survives and parses a
// valid file next.
func TestOpenAIAgentsParser_NonAgentsFile(t *testing.T) {
	p := newOAParser(t)
	dir := findOpenAIAgentsTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "not_openai_agents.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(graph.Nodes) != 0 {
		t.Errorf("expected 0 nodes for a local-Agent-class file, got %d: %v", len(graph.Nodes), oaNodeIDs(graph))
	}

	// Syntax error -> empty graph, worker stays alive.
	bad, err := p.Parse([]byte("const x = (=> {\n"))
	if err != nil {
		t.Fatalf("Parse(syntax error): %v", err)
	}
	if len(bad.Nodes) != 0 {
		t.Errorf("expected 0 nodes for a syntax-error file, got %d", len(bad.Nodes))
	}

	// Worker survives: a valid parse after the error still works.
	ok, err := p.Parse([]byte(
		"import { Agent } from \"@openai/agents\";\n" +
			"const child = new Agent({ name: \"Child\" });\n" +
			"export const root = new Agent({ name: \"Root\", handoffs: [child] });\n",
	))
	if err != nil {
		t.Fatalf("Parse(valid after error): %v", err)
	}
	if !oaHasEdge(ok, "Root", "Child") {
		t.Errorf("worker did not recover: expected edge Root->Child (edges=%v)", ok.Edges)
	}
}

// --- small assertion helpers ------------------------------------------------

func oaNodeIDs(g *domain.WorkflowGraph) []string {
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	return ids
}

func oaHasEdge(g *domain.WorkflowGraph, from, to string) bool {
	for _, e := range g.Edges {
		if e.From == from && e.To == to {
			return true
		}
	}
	return false
}

func oaCycleFindings(findings []domain.Finding) []domain.Finding {
	var out []domain.Finding
	for _, f := range findings {
		if f.RuleName == "cycle_detection" {
			out = append(out, f)
		}
	}
	return out
}

// TestOpenAIAgentsParser_UnrelatedAgentCtorIgnored guards codex #45: a
// `new foo.Agent(...)` whose `foo` is NOT an @openai/agents namespace import must
// not be treated as an OpenAI Agent, even when `{ Agent }` is imported by name.
func TestOpenAIAgentsParser_UnrelatedAgentCtorIgnored(t *testing.T) {
	p := newOAParser(t)
	dir := findOpenAIAgentsTestdata(t)
	graph, err := p.ParseFile(filepath.Join(dir, "unrelated_agent_ctor.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if _, ok := graph.Nodes["Triage"]; !ok {
		t.Errorf("real `new Agent` node \"Triage\" missing (nodes=%v)", oaNodeIDs(graph))
	}
	if _, ok := graph.Nodes["db_worker"]; ok {
		t.Errorf("unrelated `new db.Agent` must NOT be a node (nodes=%v)", oaNodeIDs(graph))
	}
}

// TestOpenAIAgentsParser_NamespaceImport guards codex #45: `import * as oa from
// "@openai/agents"` then `oa.Agent` / `oa.Agent.create` / `oa.handoff` must be
// recognized as Agent constructors/helpers.
func TestOpenAIAgentsParser_NamespaceImport(t *testing.T) {
	p := newOAParser(t)
	dir := findOpenAIAgentsTestdata(t)
	graph, err := p.ParseFile(filepath.Join(dir, "namespace_import.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"Specialist", "Triage"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("expected namespace-ctor node %q (nodes=%v)", id, oaNodeIDs(graph))
		}
	}
	if !oaHasEdge(graph, "Triage", "Specialist") {
		t.Errorf("expected edge Triage->Specialist (oa.handoff / handoffs), edges=%v", graph.Edges)
	}
	if graph.EntryNodeID != "Triage" {
		t.Errorf("EntryNodeID = %q, want Triage", graph.EntryNodeID)
	}
}
