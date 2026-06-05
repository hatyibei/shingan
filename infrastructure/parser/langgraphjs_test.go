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

// TestLangGraphJSParser_FluentChainSplit covers gap 1: a StateGraph chain that
// is the initializer of a variable (`const g = new StateGraph(...).addNode(...)`)
// continued by `g.addNode(...)` must yield ONE complete graph, not a split where
// the chained half is recorded under a synthetic <anon> builder and dropped.
func TestLangGraphJSParser_FluentChainSplit(t *testing.T) {
	p := newJSParser(t)
	dir := findLangGraphJSTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "fluent_chain_split.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// Both halves of the split chain must land in the single selected graph.
	for _, id := range []string{"a", "b"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("expected node %q in the unified graph (nodes=%v)", id, nodeIDsJS(graph))
		}
	}
	if len(graph.Nodes) != 2 {
		t.Errorf("expected exactly 2 nodes (no split), got %d: %v", len(graph.Nodes), nodeIDsJS(graph))
	}
	if graph.EntryNodeID != "a" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "a")
	}
	// The a->b edge is declared via the `g` receiver; the START->a edge and the
	// "a" node via the fluent chain. All must coexist in one graph.
	if !hasEdgeJS(graph, "a", "b") {
		t.Errorf("expected edge a->b in the unified graph (edges=%v)", graph.Edges)
	}
	if !graph.Nodes["b"].HasExitBranch {
		t.Errorf("node b should have HasExitBranch (b -> END)")
	}
}

// TestLangGraphJSParser_CommandGoto covers gap 2: node handlers that return
// `new Command({goto: X})` route dynamically. goto END marks has_exit_branch
// (downgrading a cycle to Warning); goto "<node>" synthesises an edge.
func TestLangGraphJSParser_CommandGoto(t *testing.T) {
	p := newJSParser(t)
	dir := findLangGraphJSTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "command_goto.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"worker", "other"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Fatalf("expected node %q (nodes=%v)", id, nodeIDsJS(graph))
		}
	}
	// Command(goto: "other") on worker and Command(goto: "worker") on other
	// synthesise the cycle edges (no static addEdge declares them).
	if !hasEdgeJS(graph, "worker", "other") {
		t.Errorf("expected synthesised edge worker->other from Command goto (edges=%v)", graph.Edges)
	}
	if !hasEdgeJS(graph, "other", "worker") {
		t.Errorf("expected synthesised edge other->worker from Command goto (edges=%v)", graph.Edges)
	}
	// Command(goto: END) on worker is the cycle's exit branch.
	if !graph.Nodes["worker"].HasExitBranch {
		t.Fatalf("node worker must have HasExitBranch (Command goto END)")
	}
	// Acceptance: the worker<->other cycle must downgrade to Warning, not Critical.
	findings := rules.NewCycleDetector().Analyze(graph)
	var cycleFindings []domain.Finding
	for _, f := range findings {
		if f.RuleName == "cycle_detection" {
			cycleFindings = append(cycleFindings, f)
		}
	}
	if len(cycleFindings) == 0 {
		t.Fatalf("expected a cycle_detection finding for the Command-goto cycle, got none")
	}
	for _, f := range cycleFindings {
		if f.Severity != domain.Warning {
			t.Errorf("cycle_detection severity = %v, want Warning (Command goto END exit): %+v", f.Severity, f)
		}
	}
}

// TestLangGraphJSParser_CommandGotoNotReturned locks the codex-review P2 fix:
// a Command that is constructed but never returned (a local, a nested helper)
// must NOT synthesise a control-flow edge. Only returned Commands route.
func TestLangGraphJSParser_CommandGotoNotReturned(t *testing.T) {
	p := newJSParser(t)
	dir := findLangGraphJSTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "command_goto_unused.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if hasEdgeJS(graph, "a", "b") {
		t.Errorf("a constructs but never returns Command(goto: \"b\") — no a->b edge should be synthesised (edges=%v)", graph.Edges)
	}
	if graph.Nodes["a"] != nil && graph.Nodes["a"].HasExitBranch {
		t.Errorf("node a has no returned Command(goto: END) — HasExitBranch must stay false")
	}
}

// TestLangGraphJSParser_RouterUndeclaredDest locks the node-gate on no-pathMap
// router materialization: a router naming an undeclared destination must not
// synthesise a phantom edge to a non-existent node (only the END exit lands as
// has_exit_branch).
func TestLangGraphJSParser_RouterUndeclaredDest(t *testing.T) {
	p := newJSParser(t)
	dir := findLangGraphJSTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "router_undeclared_dest.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, ghost := range []string{"ghost_a", "ghost_b"} {
		if hasEdgeJS(graph, "x", ghost) {
			t.Errorf("phantom edge x->%s to an undeclared node must not be synthesised (edges=%v)", ghost, graph.Edges)
		}
		if _, ok := graph.Nodes[ghost]; ok {
			t.Errorf("undeclared destination %q must not become a node", ghost)
		}
	}
	// The END exit (via the return-type annotation) still lands on x.
	if graph.Nodes["x"] == nil || !graph.Nodes["x"].HasExitBranch {
		t.Errorf("node x should have HasExitBranch from the `typeof END` router annotation")
	}
}

// TestLangGraphJSParser_AnnotatedRouterEnd covers gap 3a: a react loop whose
// router is a separately-declared function whose END exit is visible ONLY via
// its return-type annotation (`function route(s): "tools" | typeof END`) — no
// pathMap, opaque body. The annotation must still set has_exit_branch on the
// conditional source so the agent<->tools cycle stays Warning.
func TestLangGraphJSParser_AnnotatedRouterEnd(t *testing.T) {
	p := newJSParser(t)
	dir := findLangGraphJSTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "react_loop_annotated_router.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"agent", "tools"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Fatalf("expected node %q (nodes=%v)", id, nodeIDsJS(graph))
		}
	}
	// The annotation's "tools" literal materialises the agent->tools edge.
	if !hasEdgeJS(graph, "agent", "tools") {
		t.Errorf("expected edge agent->tools from router annotation (edges=%v)", graph.Edges)
	}
	if !hasEdgeJS(graph, "tools", "agent") {
		t.Errorf("expected edge tools->agent (edges=%v)", graph.Edges)
	}
	// The annotation's `typeof END` member must mark the conditional source.
	if !graph.Nodes["agent"].HasExitBranch {
		t.Fatalf("node agent must have HasExitBranch (annotation typeof END); " +
			"without it cycle_detection would emit Critical")
	}
	findings := rules.NewCycleDetector().Analyze(graph)
	for _, f := range findings {
		if f.RuleName == "cycle_detection" && f.Severity == domain.Critical {
			t.Errorf("cycle_detection reported Critical for annotated-router react loop; want Warning: %+v", f)
		}
	}
}

// TestLangGraphJSParser_NodeTypes covers gap 4: handler-aware node typing. Node
// names are chosen to NOT match the tool name regex, so each "tool" result here
// is decided purely by construct/body inspection (the new logic), not the name:
//   - "step"   inline `new ToolNode(...)`              -> tool (construct)
//   - "exec"   var bound to `new ToolNode(...)`        -> tool (varInits)
//   - "agent"  ChatOpenAI + bindTools/invoke + tools   -> llm  (model wins tie)
//   - "runner" body references the tools array only    -> tool (body signal)
//   - "plain"  opaque passthrough                      -> llm  (default)
//
// Under the pre-change name-only heuristic step/exec/runner would all be llm,
// so these assertions are load-bearing.
func TestLangGraphJSParser_NodeTypes(t *testing.T) {
	p := newJSParser(t)
	dir := findLangGraphJSTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "node_types.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	cases := map[string]domain.NodeType{
		"step":   domain.NodeTypeTool, // inline new ToolNode(...)
		"exec":   domain.NodeTypeTool, // var -> new ToolNode(...) (varInits path)
		"agent":  domain.NodeTypeLLM,  // model + bindTools/invoke (model wins tie)
		"runner": domain.NodeTypeTool, // tools-array body, no model signal
		"plain":  domain.NodeTypeLLM,  // opaque passthrough -> conservative default
	}
	for id, want := range cases {
		n, ok := graph.Nodes[id]
		if !ok {
			t.Fatalf("expected node %q (nodes=%v)", id, nodeIDsJS(graph))
		}
		if n.Type != want {
			t.Errorf("node %q type = %q, want %q", id, n.Type, want)
		}
	}
}

// TestLangGraphJSParser_BindEndsRouting covers the wild .bind(this)-handler +
// addNode `{ ends: [...] }` shape (agentailor fullstack-langgraph-nextjs-agent).
// Two gaps converge on one node: `tool_approval` is added as
//
//	addNode("tool_approval", this.approveToolCall.bind(this), { ends: ["tools","agent"] })
//
// (1) the handler is a `.bind()` CallExpression on a `this.method` access, and
// (2) the 3rd-arg `{ ends }` routing option was never parsed. Together the node
// got ZERO outgoing edges, so "tools" (reachable only via tool_approval) fired a
// FALSE unreachable_node at confidence 1.0. The fix restores tool_approval's
// outgoing edges (via BOTH the `ends` list and the unwrapped Command gotos,
// deduped), so "tools" is reachable and the ReAct cycle stays Warning.
func TestLangGraphJSParser_BindEndsRouting(t *testing.T) {
	p := newJSParser(t)
	dir := findLangGraphJSTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "bind_ends_routing.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"agent", "tools", "tool_approval"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Fatalf("expected node %q (nodes=%v)", id, nodeIDsJS(graph))
		}
	}
	if graph.EntryNodeID != "agent" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "agent")
	}
	// The routing node's outgoing edges must exist — from the `ends` list and/or
	// the unwrapped .bind(this) handler's Command gotos. Without them "tools" is
	// unreachable.
	if !hasEdgeJS(graph, "tool_approval", "tools") {
		t.Errorf("expected edge tool_approval->tools (ends / command goto), edges=%v", graph.Edges)
	}
	if !hasEdgeJS(graph, "tool_approval", "agent") {
		t.Errorf("expected edge tool_approval->agent (ends / command goto), edges=%v", graph.Edges)
	}
	// Dedup: the `ends` list and the .bind handler's Command gotos name the same
	// two destinations; exactly one edge per (from,to) must be materialised.
	if n := countEdgesJS(graph, "tool_approval", "tools"); n != 1 {
		t.Errorf("expected exactly 1 tool_approval->tools edge, got %d (edges=%v)", n, graph.Edges)
	}
	if n := countEdgesJS(graph, "tool_approval", "agent"); n != 1 {
		t.Errorf("expected exactly 1 tool_approval->agent edge, got %d (edges=%v)", n, graph.Edges)
	}
	// agent carries the structural END exit (the array pathMap ["tool_approval",
	// END] on addConditionalEdges) so the cycle downgrades to Warning.
	if !graph.Nodes["agent"].HasExitBranch {
		t.Fatalf("node agent must have HasExitBranch (conditional END exit)")
	}

	// The load-bearing acceptance: "tools" must NOT be flagged unreachable. This
	// is the exact wild false positive (confidence-1.0 unreachable_node on tools).
	reach := rules.NewReachabilityChecker().Analyze(graph)
	for _, f := range reach {
		if f.RuleName == "unreachable_node" && f.NodeID == "tools" {
			t.Errorf("FALSE unreachable_node for reachable \"tools\": %+v (edges=%v)", f, graph.Edges)
		}
	}

	// And no Critical cycle: agent is in the SCC and carries the exit branch.
	cyc := rules.NewCycleDetector().Analyze(graph)
	for _, f := range cyc {
		if f.RuleName == "cycle_detection" && f.Severity == domain.Critical {
			t.Errorf("cycle_detection reported Critical for the bounded ReAct loop; want Warning: %+v", f)
		}
	}
}

// TestLangGraphJSParser_StartFanOut covers the START fan-out false positive
// (dogfood: linancn/tiangong-ai-langgraph-server learning_path_agent.ts +
// single_question_agent.ts). When START routes to MULTIPLE entry nodes (plain
// `addEdge(START, X)` edges AND/OR a conditional-from-START), LangGraph.js runs
// them all in parallel. The pre-fix shim kept ONE as the entry and dropped the
// rest, so the others fired a FALSE unreachable_node @1.0. The fix models
// `__start__` as a synthetic Control entry node with an edge to each successor,
// so reachability flows to every parallel entry.
func TestLangGraphJSParser_StartFanOut(t *testing.T) {
	p := newJSParser(t)
	dir := findLangGraphJSTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "start_fanout.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"getGraph", "getRefs", "getPortrait", "getKnowledge"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Fatalf("expected node %q (nodes=%v)", id, nodeIDsJS(graph))
		}
	}
	// >=2 START successors -> synthetic __start__ Control entry node, typed so
	// no per-type rule (loop_guard etc.) fires on it.
	start, ok := graph.Nodes["__start__"]
	if !ok {
		t.Fatalf("expected synthetic __start__ entry node for a START fan-out (nodes=%v)", nodeIDsJS(graph))
	}
	if start.Type == domain.NodeTypeLoop || start.Type == domain.NodeTypeControl {
		t.Errorf("synthetic __start__ must not be a Loop/Control type (trips loop_guard); got %q", start.Type)
	}
	if start.Type == domain.NodeTypeLLM {
		t.Errorf("synthetic __start__ must not be LLM-typed (trips cost/prompt rules); got %q", start.Type)
	}
	if graph.EntryNodeID != "__start__" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "__start__")
	}
	// __start__ must have an edge to EVERY parallel entry (the 3 plain + 1
	// conditional successors).
	for _, id := range []string{"getGraph", "getRefs", "getPortrait", "getKnowledge"} {
		if !hasEdgeJS(graph, "__start__", id) {
			t.Errorf("expected synthetic edge __start__->%s (edges=%v)", id, graph.Edges)
		}
	}

	// Load-bearing acceptance: NONE of the parallel entries may be flagged
	// unreachable — that is the exact wild false positive.
	reach := rules.NewReachabilityChecker().Analyze(graph)
	for _, f := range reach {
		if f.RuleName == "unreachable_node" {
			t.Errorf("FALSE unreachable_node in a START fan-out graph: %+v (edges=%v)", f, graph.Edges)
		}
	}
	// And the synthetic __start__ node must itself trip nothing across the full
	// registered rule suite (the OnNode grep misses global/dataflow rules, so
	// exercise every builtin against the real graph).
	for _, rule := range rules.AllBuiltins() {
		for _, f := range rule.Analyze(graph) {
			if f.NodeID == "__start__" {
				t.Errorf("synthetic __start__ node must trip no rule, got %s: %+v", f.RuleName, f)
			}
		}
	}
}

// TestLangGraphJSParser_StartConditionalSingle locks the single-successor path:
// a conditional-from-START with exactly ONE branch target (the wild
// kg_textbook_agent.ts shape, `addConditionalEdges(START, router,
// ["getChapters"])`). The pre-fix shim early-returned on a START source, so the
// lone successor never became the entry and fired a FALSE unreachable. With
// exactly one START successor the entry resolves to that node and NO synthetic
// __start__ node is materialised (single-entry behaviour is byte-identical to a
// plain `addEdge(START, x)`).
func TestLangGraphJSParser_StartConditionalSingle(t *testing.T) {
	p := newJSParser(t)
	dir := findLangGraphJSTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "start_conditional_single.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"getChapters", "getContents", "generateKG"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Fatalf("expected node %q (nodes=%v)", id, nodeIDsJS(graph))
		}
	}
	// Single START successor: entry IS that node, and no synthetic __start__.
	if graph.EntryNodeID != "getChapters" {
		t.Errorf("EntryNodeID = %q, want %q (single conditional-from-START successor)", graph.EntryNodeID, "getChapters")
	}
	if _, ok := graph.Nodes["__start__"]; ok {
		t.Errorf("a single START successor must NOT materialise a synthetic __start__ node (nodes=%v)", nodeIDsJS(graph))
	}
	if graph.EntryAmbiguous {
		t.Errorf("single-graph file must not be EntryAmbiguous")
	}
	// No false unreachable: getChapters (the real entry) was the pre-fix FP.
	reach := rules.NewReachabilityChecker().Analyze(graph)
	for _, f := range reach {
		if f.RuleName == "unreachable_node" {
			t.Errorf("FALSE unreachable_node in single-conditional-START graph: %+v (edges=%v)", f, graph.Edges)
		}
	}
}

// TestLangGraphJSParser_MultiStateGraph covers the multi-graph collapse false
// positive (dogfood: LinMoQC/Magic-Resume graphs.ts). When one file declares
// MULTIPLE `new StateGraph(...).compile()` graphs that REUSE the same variable
// name, the per-varname builder merges them under one root with one entry, so
// nodes of graphs 2..N fire FALSE unreachable. The fix detects the disjoint
// per-root node sets and flags entry_ambiguous (entry empty), so reachability
// skips the graph. Mirrors the pydantic-graph multi-`Graph()` fix (#36).
func TestLangGraphJSParser_MultiStateGraph(t *testing.T) {
	p := newJSParser(t)
	dir := findLangGraphJSTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "multi_state_graph.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// Every graph's nodes are preserved in the catalog (we suppress the entry,
	// not the nodes).
	for _, id := range []string{"preparer", "researcher", "analyzer", "combiner", "rewriter", "finalizer"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("expected node %q preserved in catalog (nodes=%v)", id, nodeIDsJS(graph))
		}
	}
	// Disjoint multi-graph -> EntryAmbiguous + empty entry.
	if !graph.EntryAmbiguous {
		t.Errorf("expected EntryAmbiguous=true for a multi-StateGraph file with disjoint node sets")
	}
	if graph.EntryNodeID != "" {
		t.Errorf("EntryNodeID = %q, want \"\" (ambiguous across disjoint graphs)", graph.EntryNodeID)
	}
	// Load-bearing acceptance: reachability SKIPS the graph (no false
	// unreachable on graphs 2..N), exactly as the EntryAmbiguous && empty-entry
	// branch in reachability.go specifies.
	reach := rules.NewReachabilityChecker().Analyze(graph)
	for _, f := range reach {
		if f.RuleName == "unreachable_node" {
			t.Errorf("multi-StateGraph file must not produce unreachable_node FPs: %+v", f)
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

func countEdgesJS(g *domain.WorkflowGraph, from, to string) int {
	n := 0
	for _, e := range g.Edges {
		if e.From == from && e.To == to {
			n++
		}
	}
	return n
}

func hasConditionalEdgeJS(g *domain.WorkflowGraph, from, to, cond string) bool {
	for _, e := range g.Edges {
		if e.From == from && e.To == to && e.Condition == cond {
			return true
		}
	}
	return false
}

// TestLangGraphJSParser_AmbiguousMethodHandler guards the codex #36 refinement:
// when two classes define a same-named method ("route"), a this.route.bind(this)
// handler must NOT resolve to an arbitrary class's body — otherwise the decoy
// class's Command goto is grafted onto this graph. The shim omits rather than
// invents, so no wrong-class edge appears.
func TestLangGraphJSParser_AmbiguousMethodHandler(t *testing.T) {
	p := newJSParser(t)
	dir := findLangGraphJSTestdata(t)

	graph, err := p.ParseFile(filepath.Join(dir, "dup_method_handlers.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"router", "real_target", "wrong_target"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Fatalf("expected node %q (nodes=%v)", id, nodeIDsJS(graph))
		}
	}
	if hasEdgeJS(graph, "router", "wrong_target") {
		t.Errorf("ambiguous this.route must not graft the decoy class's goto: router->wrong_target (edges=%v)", graph.Edges)
	}
}

// TestLangGraphJSParser_MultiStateGraphIdentifierStyle guards the codex #49 fix:
// multiple StateGraph graphs via a reused variable name + identifier-style
// addNode + mixed addEdge(START)/setEntryPoint are detected as multi-graph via
// the file-global StateGraph-root count (the per-builder fluent disjoint check
// misses this style), so no node is falsely unreachable across the graphs.
func TestLangGraphJSParser_MultiStateGraphIdentifierStyle(t *testing.T) {
	p := newJSParser(t)
	dir := findLangGraphJSTestdata(t)
	graph, err := p.ParseFile(filepath.Join(dir, "multi_state_graph_identifier.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if !graph.EntryAmbiguous || graph.EntryNodeID != "" {
		t.Errorf("expected EntryAmbiguous + empty entry for an identifier-style multi-StateGraph file, got ambiguous=%v entry=%q", graph.EntryAmbiguous, graph.EntryNodeID)
	}
	for _, f := range rules.NewReachabilityChecker().Analyze(graph) {
		if f.RuleName == "unreachable_node" {
			t.Errorf("identifier-style multi-StateGraph must not produce unreachable_node FPs: %+v", f)
		}
	}
}

// TestLangGraphJSParser_TernaryRouter guards the ternary-return-router fix: a
// path-map-less addConditionalEdges whose (concise-arrow) router returns a
// ternary must have BOTH branch destinations harvested, so neither "answer" nor
// "rewrite" is falsely unreachable.
func TestLangGraphJSParser_TernaryRouter(t *testing.T) {
	p := newJSParser(t)
	dir := findLangGraphJSTestdata(t)
	graph, err := p.ParseFile(filepath.Join(dir, "ternary_router.ts"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, id := range []string{"grade", "answer", "rewrite"} {
		if _, ok := graph.Nodes[id]; !ok {
			t.Fatalf("expected node %q (nodes=%v)", id, nodeIDsJS(graph))
		}
	}
	if !hasEdgeJS(graph, "grade", "answer") || !hasEdgeJS(graph, "grade", "rewrite") {
		t.Errorf("ternary router branches must both be edges (grade->answer, grade->rewrite), edges=%v", graph.Edges)
	}
	for _, f := range rules.NewReachabilityChecker().Analyze(graph) {
		if f.RuleName == "unreachable_node" {
			t.Errorf("ternary router must not leave a node falsely unreachable: %+v", f)
		}
	}
}
