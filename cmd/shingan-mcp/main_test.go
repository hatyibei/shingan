package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hatyibei/shingan/application"
	"github.com/hatyibei/shingan/infrastructure/factory"
)

// newTestDeps wires real factories/orchestrator. Nothing here is mocked; we
// want the tests to exercise the full analysis pipeline the same way a real
// MCP client would.
func newTestDeps() *toolDeps {
	return &toolDeps{
		analyzerFactory: factory.NewAnalyzerFactory(),
		parserFactory:   factory.NewParserFactory(),
		orchestrator:    application.NewAnalysisOrchestrator(),
	}
}

// sampleGraphJSON returns a tiny graph with a classic loop-guard violation:
// a Loop node without max_iterations. This hits at least two rules
// (loop_guard, cycle_detection) so the assertions can be non-trivial.
const sampleGraphJSON = `{
  "nodes": [
    {"id": "start", "name": "start", "type": "llm", "config": {"model": "gpt-4o"}},
    {"id": "loop",  "name": "loop",  "type": "loop", "config": {}},
    {"id": "end",   "name": "end",   "type": "output"}
  ],
  "edges": [
    {"from": "start", "to": "loop"},
    {"from": "loop",  "to": "start"},
    {"from": "loop",  "to": "end"}
  ],
  "entry_node_id": "start"
}`

func TestAnalyzeGraph_ReturnsFindings(t *testing.T) {
	deps := newTestDeps()
	res, out, err := deps.analyzeGraph(context.Background(), nil, AnalyzeGraphArgs{GraphJSON: sampleGraphJSON})
	if err != nil {
		t.Fatalf("analyzeGraph returned error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got IsError=true: %+v", res.Content)
	}
	if out.Count == 0 {
		t.Fatalf("expected findings for cyclic loop-guard graph, got 0")
	}

	// Must detect both loop_guard (missing max_iterations) and cycle_detection.
	seen := map[string]bool{}
	for _, f := range out.Findings {
		seen[f.RuleName] = true
	}
	for _, required := range []string{"loop_guard", "cycle_detection"} {
		if !seen[required] {
			t.Errorf("expected rule %q to fire, got %v", required, keys(seen))
		}
	}
}

func TestAnalyzeGraph_EmptyInput(t *testing.T) {
	deps := newTestDeps()
	res, _, err := deps.analyzeGraph(context.Background(), nil, AnalyzeGraphArgs{GraphJSON: ""})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true for empty input")
	}
}

func TestAnalyzeGraph_InvalidJSON(t *testing.T) {
	deps := newTestDeps()
	res, _, err := deps.analyzeGraph(context.Background(), nil, AnalyzeGraphArgs{GraphJSON: "not json"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true for malformed JSON")
	}
}

func TestAnalyzeFile_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.json")
	if err := os.WriteFile(path, []byte(sampleGraphJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	deps := newTestDeps()
	res, out, err := deps.analyzeFile(context.Background(), nil, AnalyzeFileArgs{Path: path, Framework: "json"})
	if err != nil {
		t.Fatalf("analyzeFile returned error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected IsError=true: %+v", res.Content)
	}
	if out.Count == 0 {
		t.Fatal("expected findings from sample graph, got 0")
	}
}

func TestAnalyzeFile_BadFramework(t *testing.T) {
	deps := newTestDeps()
	res, _, err := deps.analyzeFile(context.Background(), nil, AnalyzeFileArgs{Path: "/tmp/irrelevant", Framework: "terraform"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true for unknown framework")
	}
}

func TestAnalyzeFile_MissingPath(t *testing.T) {
	deps := newTestDeps()
	res, _, _ := deps.analyzeFile(context.Background(), nil, AnalyzeFileArgs{Path: "", Framework: "json"})
	if !res.IsError {
		t.Fatal("expected IsError=true for missing path")
	}
}

func TestExplainRule_AllKnown(t *testing.T) {
	deps := newTestDeps()

	// Parity check: factory.CreateAll() must exactly equal our explanation map.
	// Keeps explain.go and analyzer.go in lock-step.
	factoryNames := map[string]bool{}
	for _, r := range deps.analyzerFactory.CreateAll() {
		factoryNames[r.Name()] = true
	}
	for name := range factoryNames {
		if _, ok := ruleExplanations[name]; !ok {
			t.Errorf("rule %q has no explanation", name)
		}
	}
	for name := range ruleExplanations {
		if !factoryNames[name] {
			t.Errorf("explanation for %q has no matching rule in factory", name)
		}
	}

	// Smoke-test one explanation end-to-end.
	res, out, err := deps.explainRule(context.Background(), nil, ExplainRuleArgs{RuleName: "loop_guard"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got IsError: %+v", res.Content)
	}
	if out.RuleName != "loop_guard" {
		t.Errorf("expected rule_name=loop_guard, got %q", out.RuleName)
	}
	if !strings.Contains(out.Explanation, "max_iterations") {
		t.Errorf("loop_guard explanation should mention max_iterations, got:\n%s", out.Explanation)
	}
}

func TestExplainRule_Unknown(t *testing.T) {
	deps := newTestDeps()
	res, _, _ := deps.explainRule(context.Background(), nil, ExplainRuleArgs{RuleName: "does_not_exist"})
	if !res.IsError {
		t.Fatal("expected IsError for unknown rule")
	}
}

func TestSuggestModel_Heuristics(t *testing.T) {
	deps := newTestDeps()
	cases := []struct {
		name         string
		args         SuggestModelArgs
		wantModelSub string
	}{
		{"heavy reasoning", SuggestModelArgs{NodeDescription: "complex multi-step reasoning over JSON", InputTokenEstimate: 5000}, "sonnet"},
		{"classification", SuggestModelArgs{NodeDescription: "extract entities from a short sentence", InputTokenEstimate: 200}, "mini"},
		{"short prompt", SuggestModelArgs{NodeDescription: "generate a short reply", InputTokenEstimate: 300}, "mini"},
		{"default", SuggestModelArgs{NodeDescription: "do a thing", InputTokenEstimate: 4000}, "gpt-4o"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, rec, err := deps.suggestModel(context.Background(), nil, tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if res.IsError {
				t.Fatalf("unexpected IsError: %+v", res.Content)
			}
			if !strings.Contains(rec.Model, tc.wantModelSub) {
				t.Errorf("model=%q, want substring %q", rec.Model, tc.wantModelSub)
			}
			if rec.EstimatedCostPerCallUSD <= 0 {
				t.Errorf("expected positive cost estimate, got %f", rec.EstimatedCostPerCallUSD)
			}
		})
	}
}

// TestInitializeHandshake exercises the MCP SDK end-to-end over an in-memory
// transport, confirming that our registerTools wiring is reachable and the
// server survives a full initialize → ListTools → CallTool cycle.
func TestInitializeHandshake(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clientT, serverT := mcp.NewInMemoryTransports()

	server := mcp.NewServer(&mcp.Implementation{Name: "shingan", Version: "test"}, nil)
	registerTools(server, newTestDeps())

	serverSession, err := server.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	tools, err := clientSession.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	wantTools := map[string]bool{
		"shingan_analyze_graph": false,
		"shingan_analyze_file":  false,
		"shingan_explain_rule":  false,
		"shingan_suggest_model": false,
	}
	for _, tool := range tools.Tools {
		if _, ok := wantTools[tool.Name]; ok {
			wantTools[tool.Name] = true
		}
	}
	for name, found := range wantTools {
		if !found {
			t.Errorf("tool %q not registered with server", name)
		}
	}

	// Round-trip one CallTool for good measure.
	call, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "shingan_explain_rule",
		Arguments: map[string]any{"rule_name": "cycle_detection"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if call.IsError {
		t.Fatalf("CallTool error: %+v", call.Content)
	}
	if len(call.Content) == 0 {
		t.Fatal("expected content in CallTool result")
	}
	text, ok := call.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", call.Content[0])
	}
	if !strings.Contains(text.Text, "DFS") {
		t.Errorf("expected cycle_detection explanation to mention DFS, got: %s", text.Text)
	}
}

// TestRecoverHandler_TurnsPanicIntoErrorResult confirms that a panic inside
// a tool handler is caught and translated into a clean MCP error response,
// instead of crashing the stdio server and leaving the client with EOF.
func TestRecoverHandler_TurnsPanicIntoErrorResult(t *testing.T) {
	var res *mcp.CallToolResult
	func() {
		defer recoverHandler("shingan_test_tool", &res)
		panic("synthetic boom")
	}()

	if res == nil {
		t.Fatal("recoverHandler did not set a result on panic")
	}
	if !res.IsError {
		t.Fatal("expected IsError=true after panic recovery")
	}
	if len(res.Content) == 0 {
		t.Fatal("expected error content on recovery")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	if !strings.Contains(text.Text, "shingan_test_tool") || !strings.Contains(text.Text, "synthetic boom") {
		t.Errorf("recovery message missing tool name or panic value: %q", text.Text)
	}
}

// TestFindingDTO_JSONShape guarantees the wire format stays stable for
// clients that parse the findings as JSON rather than trusting structured
// output.
func TestFindingDTO_JSONShape(t *testing.T) {
	dto := FindingDTO{
		RuleName: "loop_guard", Severity: "critical", NodeID: "loop",
		Message: "missing max_iterations", Suggestion: "set config.max_iterations=3",
		Confidence: 1.0,
	}
	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"rule_name"`, `"severity"`, `"node_id"`, `"message"`, `"confidence"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("JSON missing %s: %s", key, raw)
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
