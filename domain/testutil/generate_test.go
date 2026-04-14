package testutil_test

import (
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/domain/testutil"
)

// ---- helpers ----

func findingsByRule(findings []domain.Finding, ruleName string) []domain.Finding {
	var out []domain.Finding
	for _, f := range findings {
		if f.RuleName == ruleName {
			out = append(out, f)
		}
	}
	return out
}

func hasFinding(findings []domain.Finding, ruleName string, sev domain.Severity) bool {
	for _, f := range findings {
		if f.RuleName == ruleName && f.Severity == sev {
			return true
		}
	}
	return false
}

func allRules() []domain.AnalysisRule {
	return []domain.AnalysisRule{
		rules.NewCycleDetector(),
		rules.NewLoopGuardChecker(),
		rules.NewReachabilityChecker(),
		rules.NewErrorHandlerChecker(),
		rules.NewCostAnalyzer(),
		rules.NewRedundantLLMDetector(),
		rules.NewPIILeakScanner(),
	}
}

func runAllRules(g *domain.WorkflowGraph) []domain.Finding {
	var all []domain.Finding
	for _, r := range allRules() {
		all = append(all, r.Analyze(g)...)
	}
	return all
}

// ---- GenerateCleanGraph ----

func TestGenerateCleanGraph_ReturnsValidGraph(t *testing.T) {
	g := testutil.GenerateCleanGraph(10, 42)
	if g == nil {
		t.Fatal("expected non-nil graph")
	}
	if len(g.Nodes) == 0 {
		t.Error("expected at least one node")
	}
	if g.EntryNodeID == "" {
		t.Error("expected EntryNodeID to be set")
	}
	if _, ok := g.Nodes[g.EntryNodeID]; !ok {
		t.Errorf("entry node %q not found in Nodes map", g.EntryNodeID)
	}
}

func TestGenerateCleanGraph_NoFindings(t *testing.T) {
	g := testutil.GenerateCleanGraph(5, 42)
	findings := runAllRules(g)
	if len(findings) > 0 {
		t.Errorf("expected 0 findings from clean graph, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  [%s] %s: %s", f.Severity, f.RuleName, f.Message)
		}
	}
}

func TestGenerateCleanGraph_Reproducible(t *testing.T) {
	g1 := testutil.GenerateCleanGraph(8, 99)
	g2 := testutil.GenerateCleanGraph(8, 99)
	if len(g1.Nodes) != len(g2.Nodes) {
		t.Errorf("seed-identical calls should produce same node count: %d vs %d", len(g1.Nodes), len(g2.Nodes))
	}
	if g1.EntryNodeID != g2.EntryNodeID {
		t.Errorf("seed-identical calls should produce same entry: %q vs %q", g1.EntryNodeID, g2.EntryNodeID)
	}
}

// ---- GenerateInfiniteLoopGraph ----

func TestGenerateInfiniteLoopGraph_ReturnsValidGraph(t *testing.T) {
	g := testutil.GenerateInfiniteLoopGraph(42)
	if g == nil {
		t.Fatal("expected non-nil graph")
	}
	if len(g.Nodes) == 0 {
		t.Error("expected at least one node")
	}
}

func TestGenerateInfiniteLoopGraph_TriggersLoopGuard(t *testing.T) {
	g := testutil.GenerateInfiniteLoopGraph(42)
	findings := runAllRules(g)
	if !hasFinding(findings, "loop_guard", domain.Critical) {
		t.Error("expected loop_guard Critical finding")
	}
}

func TestGenerateInfiniteLoopGraph_TriggersCycleDetection(t *testing.T) {
	g := testutil.GenerateInfiniteLoopGraph(42)
	findings := runAllRules(g)
	if !hasFinding(findings, "cycle_detection", domain.Critical) {
		t.Error("expected cycle_detection Critical finding")
	}
}

// ---- GenerateUnreachableGraph ----

func TestGenerateUnreachableGraph_ReturnsValidGraph(t *testing.T) {
	g := testutil.GenerateUnreachableGraph(10, 42)
	if g == nil {
		t.Fatal("expected non-nil graph")
	}
	if len(g.Nodes) == 0 {
		t.Error("expected at least one node")
	}
}

func TestGenerateUnreachableGraph_TriggersUnreachableNode(t *testing.T) {
	g := testutil.GenerateUnreachableGraph(10, 42)
	findings := runAllRules(g)
	unreachable := findingsByRule(findings, "unreachable_node")
	if len(unreachable) == 0 {
		t.Error("expected at least one unreachable_node finding")
	}
}

func TestGenerateUnreachableGraph_HasDanglingNodes(t *testing.T) {
	g := testutil.GenerateUnreachableGraph(10, 42)
	// dangling_llm and dangling_tool should be present
	if _, ok := g.Nodes["dangling_llm"]; !ok {
		t.Error("expected dangling_llm node")
	}
	if _, ok := g.Nodes["dangling_tool"]; !ok {
		t.Error("expected dangling_tool node")
	}
}

// ---- GeneratePIILeakGraph ----

func TestGeneratePIILeakGraph_ReturnsValidGraph(t *testing.T) {
	g := testutil.GeneratePIILeakGraph(42)
	if g == nil {
		t.Fatal("expected non-nil graph")
	}
	if len(g.Nodes) == 0 {
		t.Error("expected at least one node")
	}
}

func TestGeneratePIILeakGraph_TriggersPIILeak(t *testing.T) {
	g := testutil.GeneratePIILeakGraph(42)
	findings := runAllRules(g)
	if !hasFinding(findings, "pii_leak_scanner", domain.Warning) {
		t.Error("expected pii_leak_scanner Warning finding")
	}
}

func TestGeneratePIILeakGraph_NoHumanGate(t *testing.T) {
	g := testutil.GeneratePIILeakGraph(42)
	// Verify no Human node exists (which would block the pii leak)
	for _, n := range g.Nodes {
		if n.Type == domain.NodeTypeHuman {
			t.Error("expected no Human gate in pii-leak graph")
		}
	}
}

// ---- GenerateCycleGraph ----

func TestGenerateCycleGraph_ReturnsValidGraph(t *testing.T) {
	g := testutil.GenerateCycleGraph(4, 42)
	if g == nil {
		t.Fatal("expected non-nil graph")
	}
	if len(g.Nodes) == 0 {
		t.Error("expected at least one node")
	}
}

func TestGenerateCycleGraph_TriggersCycleDetection(t *testing.T) {
	g := testutil.GenerateCycleGraph(4, 42)
	findings := runAllRules(g)
	if !hasFinding(findings, "cycle_detection", domain.Critical) {
		t.Error("expected cycle_detection Critical finding")
	}
}

func TestGenerateCycleGraph_NoLoopNode(t *testing.T) {
	g := testutil.GenerateCycleGraph(4, 42)
	for _, n := range g.Nodes {
		if n.Type == domain.NodeTypeLoop || n.Type == domain.NodeTypeControl {
			t.Errorf("unexpected Loop/Control node %q in cycle graph — cycle should be raw (no loop guard)", n.ID)
		}
	}
}

// ---- GenerateBuggyGraph ----

func TestGenerateBuggyGraph_ReturnsValidGraph(t *testing.T) {
	g := testutil.GenerateBuggyGraph(42)
	if g == nil {
		t.Fatal("expected non-nil graph")
	}
	if len(g.Nodes) == 0 {
		t.Error("expected at least one node")
	}
}

func TestGenerateBuggyGraph_TriggersLoopGuard(t *testing.T) {
	g := testutil.GenerateBuggyGraph(42)
	findings := runAllRules(g)
	if !hasFinding(findings, "loop_guard", domain.Critical) {
		t.Error("expected loop_guard Critical finding from buggy graph")
	}
}

func TestGenerateBuggyGraph_TriggersCycleDetection(t *testing.T) {
	g := testutil.GenerateBuggyGraph(42)
	findings := runAllRules(g)
	if !hasFinding(findings, "cycle_detection", domain.Critical) {
		t.Error("expected cycle_detection Critical finding from buggy graph")
	}
}

func TestGenerateBuggyGraph_TriggersUnreachableNode(t *testing.T) {
	g := testutil.GenerateBuggyGraph(42)
	findings := runAllRules(g)
	if !hasFinding(findings, "unreachable_node", domain.Warning) {
		t.Error("expected unreachable_node Warning finding from buggy graph")
	}
}

func TestGenerateBuggyGraph_TriggersErrorHandler(t *testing.T) {
	g := testutil.GenerateBuggyGraph(42)
	findings := runAllRules(g)
	eh := findingsByRule(findings, "error_handler_checker")
	if len(eh) == 0 {
		t.Error("expected error_handler_checker finding from buggy graph")
	}
}

func TestGenerateBuggyGraph_TriggersCostEstimation(t *testing.T) {
	g := testutil.GenerateBuggyGraph(42)
	findings := runAllRules(g)
	if !hasFinding(findings, "cost_estimation", domain.Warning) {
		t.Error("expected cost_estimation Warning finding from buggy graph")
	}
}

func TestGenerateBuggyGraph_TriggersRedundantLLM(t *testing.T) {
	g := testutil.GenerateBuggyGraph(42)
	findings := runAllRules(g)
	if !hasFinding(findings, "redundant_llm_call", domain.Warning) {
		t.Error("expected redundant_llm_call Warning finding from buggy graph")
	}
}

func TestGenerateBuggyGraph_TriggersPIILeak(t *testing.T) {
	g := testutil.GenerateBuggyGraph(42)
	findings := runAllRules(g)
	if !hasFinding(findings, "pii_leak_scanner", domain.Warning) {
		t.Error("expected pii_leak_scanner Warning finding from buggy graph")
	}
}

func TestGenerateBuggyGraph_AllSevenRulesFire(t *testing.T) {
	g := testutil.GenerateBuggyGraph(42)
	findings := runAllRules(g)

	expectedRules := []string{
		"cycle_detection",
		"loop_guard",
		"unreachable_node",
		"error_handler_checker",
		"cost_estimation",
		"redundant_llm_call",
		"pii_leak_scanner",
	}

	firedRules := make(map[string]bool)
	for _, f := range findings {
		firedRules[f.RuleName] = true
	}

	for _, rule := range expectedRules {
		if !firedRules[rule] {
			t.Errorf("expected rule %q to fire, but got no findings for it", rule)
		}
	}
}

// ---- GenerateRandomGraph (existing) ----

func TestGenerateRandomGraph_ReturnsValidGraph(t *testing.T) {
	g := testutil.GenerateRandomGraph(20, 42)
	if g == nil {
		t.Fatal("expected non-nil graph")
	}
	if len(g.Nodes) != 20 {
		t.Errorf("expected 20 nodes, got %d", len(g.Nodes))
	}
}

func TestGenerateRandomGraph_Reproducible(t *testing.T) {
	g1 := testutil.GenerateRandomGraph(15, 7)
	g2 := testutil.GenerateRandomGraph(15, 7)
	if len(g1.Nodes) != len(g2.Nodes) {
		t.Errorf("same seed should produce same node count: %d vs %d", len(g1.Nodes), len(g2.Nodes))
	}
	if len(g1.Edges) != len(g2.Edges) {
		t.Errorf("same seed should produce same edge count: %d vs %d", len(g1.Edges), len(g2.Edges))
	}
}

func TestGenerateRandomGraph_ZeroN(t *testing.T) {
	g := testutil.GenerateRandomGraph(0, 42)
	if g == nil {
		t.Fatal("expected non-nil graph for n=0")
	}
	if len(g.Nodes) != 0 {
		t.Errorf("expected 0 nodes for n=0, got %d", len(g.Nodes))
	}
}
