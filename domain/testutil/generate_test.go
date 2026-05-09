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
		rules.NewSecretExposureScanner(),
		rules.NewDeprecatedModelChecker(),
		rules.NewMaxParallelBranchesChecker(),
		rules.NewPromptInjectionSink(),
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
	// GenerateCycleGraph creates a cycle PLUS an exit edge to an output
	// sink (matching the canonical LangGraph "tool-calling agent" shape).
	// Per the v0.8.5 cycle_detection refinement (langchain factory.py +
	// agno cookbook dogfood), bounded cycles with an exit branch
	// downgrade Critical → Warning.
	if !hasFinding(findings, "cycle_detection", domain.Warning) {
		t.Error("expected cycle_detection Warning finding (cycle has an exit edge)")
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

// ---- GenerateSecretExposureGraph ----

func TestGenerateSecretExposureGraph_ReturnsValidGraph(t *testing.T) {
	g := testutil.GenerateSecretExposureGraph(42)
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

func TestGenerateSecretExposureGraph_TriggersSecretExposureCritical(t *testing.T) {
	g := testutil.GenerateSecretExposureGraph(42)
	findings := rules.NewSecretExposureScanner().Analyze(g)
	if !hasFinding(findings, "secret_exposure_scanner", domain.Critical) {
		t.Errorf("expected secret_exposure_scanner Critical finding, got %d findings: %+v", len(findings), findings)
	}
}

func TestGenerateSecretExposureGraph_OtherRulesClean(t *testing.T) {
	g := testutil.GenerateSecretExposureGraph(42)
	// All rules except secret_exposure_scanner should produce 0 findings
	otherRules := []domain.AnalysisRule{
		rules.NewCycleDetector(),
		rules.NewLoopGuardChecker(),
		rules.NewReachabilityChecker(),
		rules.NewCostAnalyzer(),
		rules.NewRedundantLLMDetector(),
		rules.NewPIILeakScanner(),
	}
	for _, r := range otherRules {
		if fs := r.Analyze(g); len(fs) != 0 {
			t.Errorf("rule %q: expected 0 findings on secret-exposure graph, got %d: %+v", r.Name(), len(fs), fs)
		}
	}
}

// ---- GenerateDeprecatedModelGraph ----

func TestGenerateDeprecatedModelGraph_ReturnsValidGraph(t *testing.T) {
	g := testutil.GenerateDeprecatedModelGraph(42)
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

func TestGenerateDeprecatedModelGraph_TriggersDeprecatedModelCritical(t *testing.T) {
	g := testutil.GenerateDeprecatedModelGraph(42)
	findings := rules.NewDeprecatedModelChecker().Analyze(g)
	if !hasFinding(findings, "deprecated_model", domain.Critical) {
		t.Errorf("expected deprecated_model Critical finding, got %d findings: %+v", len(findings), findings)
	}
}

func TestGenerateDeprecatedModelGraph_OtherRulesClean(t *testing.T) {
	g := testutil.GenerateDeprecatedModelGraph(42)
	// Rules other than deprecated_model should produce 0 findings
	otherRules := []domain.AnalysisRule{
		rules.NewCycleDetector(),
		rules.NewLoopGuardChecker(),
		rules.NewReachabilityChecker(),
		rules.NewRedundantLLMDetector(),
		rules.NewPIILeakScanner(),
		rules.NewSecretExposureScanner(),
	}
	for _, r := range otherRules {
		if fs := r.Analyze(g); len(fs) != 0 {
			t.Errorf("rule %q: expected 0 findings on deprecated-model graph, got %d: %+v", r.Name(), len(fs), fs)
		}
	}
}

// ---- GenerateTemperatureMisuseGraph ----

func TestGenerateTemperatureMisuseGraph_ReturnsValidGraph(t *testing.T) {
	g := testutil.GenerateTemperatureMisuseGraph(42)
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

func TestGenerateTemperatureMisuseGraph_TriggersTemperatureMisuseWarning(t *testing.T) {
	g := testutil.GenerateTemperatureMisuseGraph(42)
	findings := rules.NewTemperatureMisuseChecker().Analyze(g)
	if !hasFinding(findings, "temperature_misuse", domain.Warning) {
		t.Errorf("expected temperature_misuse Warning finding, got %d findings: %+v", len(findings), findings)
	}
}

func TestGenerateTemperatureMisuseGraph_OtherRulesClean(t *testing.T) {
	g := testutil.GenerateTemperatureMisuseGraph(42)
	otherRules := []domain.AnalysisRule{
		rules.NewCycleDetector(),
		rules.NewLoopGuardChecker(),
		rules.NewReachabilityChecker(),
		rules.NewRedundantLLMDetector(),
		rules.NewPIILeakScanner(),
		rules.NewSecretExposureScanner(),
		rules.NewDeprecatedModelChecker(),
	}
	for _, r := range otherRules {
		if fs := r.Analyze(g); len(fs) != 0 {
			t.Errorf("rule %q: expected 0 findings on temperature-misuse graph, got %d: %+v", r.Name(), len(fs), fs)
		}
	}
}

// ---- GenerateModelCardMismatchGraph ----

func TestGenerateModelCardMismatchGraph_ReturnsValidGraph(t *testing.T) {
	g := testutil.GenerateModelCardMismatchGraph(42)
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

func TestGenerateModelCardMismatchGraph_TriggersModelCardMismatchCritical(t *testing.T) {
	g := testutil.GenerateModelCardMismatchGraph(42)
	findings := rules.NewModelCardMismatchChecker().Analyze(g)
	if !hasFinding(findings, "model_card_mismatch", domain.Critical) {
		t.Errorf("expected model_card_mismatch Critical finding, got %d findings: %+v", len(findings), findings)
	}
}

func TestGenerateModelCardMismatchGraph_OtherRulesClean(t *testing.T) {
	g := testutil.GenerateModelCardMismatchGraph(42)
	otherRules := []domain.AnalysisRule{
		rules.NewCycleDetector(),
		rules.NewLoopGuardChecker(),
		rules.NewReachabilityChecker(),
		rules.NewRedundantLLMDetector(),
		rules.NewPIILeakScanner(),
		rules.NewSecretExposureScanner(),
		rules.NewDeprecatedModelChecker(),
		rules.NewTemperatureMisuseChecker(),
	}
	for _, r := range otherRules {
		if fs := r.Analyze(g); len(fs) != 0 {
			t.Errorf("rule %q: expected 0 findings on model-mismatch graph, got %d: %+v", r.Name(), len(fs), fs)
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

// ---- GenerateHighFanOutGraph ----

func TestGenerateHighFanOutGraph_ReturnsValidGraph(t *testing.T) {
	g := testutil.GenerateHighFanOutGraph(42, 100)
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

func TestGenerateHighFanOutGraph_FanOut100_TriggersCritical(t *testing.T) {
	g := testutil.GenerateHighFanOutGraph(42, 100)
	findings := rules.NewMaxParallelBranchesChecker().Analyze(g)
	if !hasFinding(findings, "max_parallel_branches", domain.Critical) {
		t.Errorf("expected max_parallel_branches Critical finding for fan-out=100, got %d findings: %+v", len(findings), findings)
	}
}

func TestGenerateHighFanOutGraph_FanOut20_TriggersWarning(t *testing.T) {
	g := testutil.GenerateHighFanOutGraph(42, 20)
	findings := rules.NewMaxParallelBranchesChecker().Analyze(g)
	if !hasFinding(findings, "max_parallel_branches", domain.Warning) {
		t.Errorf("expected max_parallel_branches Warning finding for fan-out=20, got %d findings: %+v", len(findings), findings)
	}
}

func TestGenerateHighFanOutGraph_FanOut5_NoFindings(t *testing.T) {
	g := testutil.GenerateHighFanOutGraph(42, 5)
	findings := rules.NewMaxParallelBranchesChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("fan-out=5: expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestGenerateHighFanOutGraph_OtherRulesClean(t *testing.T) {
	// fan-out=100グラフ: max_parallel_branches以外のルールは0件であること
	g := testutil.GenerateHighFanOutGraph(42, 100)
	otherRules := []domain.AnalysisRule{
		rules.NewCycleDetector(),
		rules.NewLoopGuardChecker(),
		rules.NewReachabilityChecker(),
		rules.NewRedundantLLMDetector(),
		rules.NewPIILeakScanner(),
		rules.NewSecretExposureScanner(),
	}
	for _, r := range otherRules {
		if fs := r.Analyze(g); len(fs) != 0 {
			t.Errorf("rule %q: expected 0 findings on high-fanout graph, got %d: %+v", r.Name(), len(fs), fs)
		}
	}
}

// ---- GeneratePromptInjectionSinkGraph ----

func TestGeneratePromptInjectionSinkGraph_ReturnsValidGraph(t *testing.T) {
	g := testutil.GeneratePromptInjectionSinkGraph(42)
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

func TestGeneratePromptInjectionSinkGraph_TriggersCriticalFinding(t *testing.T) {
	g := testutil.GeneratePromptInjectionSinkGraph(42)
	findings := rules.NewPromptInjectionSink().Analyze(g)
	if len(findings) == 0 {
		t.Fatal("expected at least one prompt_injection_sink finding")
	}
	if !hasFinding(findings, "prompt_injection_sink", domain.Critical) {
		t.Errorf("expected prompt_injection_sink Critical finding, got %+v", findings)
	}
}

func TestGeneratePromptInjectionSinkGraph_OtherRulesClean(t *testing.T) {
	g := testutil.GeneratePromptInjectionSinkGraph(42)
	// Independent rules that should NOT fire on this minimal injection graph.
	other := []domain.AnalysisRule{
		rules.NewCycleDetector(),
		rules.NewLoopGuardChecker(),
		rules.NewReachabilityChecker(),
		rules.NewRedundantLLMDetector(),
		rules.NewPIILeakScanner(),
		rules.NewSecretExposureScanner(),
		rules.NewDeprecatedModelChecker(),
		rules.NewMaxParallelBranchesChecker(),
	}
	for _, r := range other {
		if fs := r.Analyze(g); len(fs) != 0 {
			t.Errorf("rule %q: expected 0 findings on prompt-injection graph, got %d: %+v", r.Name(), len(fs), fs)
		}
	}
}

// ---- GenerateEvalMissingGraph ----

func TestGenerateEvalMissingGraph_ReturnsValidGraph(t *testing.T) {
	g := testutil.GenerateEvalMissingGraph(42)
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

func TestGenerateEvalMissingGraph_TriggersCriticalFinding(t *testing.T) {
	g := testutil.GenerateEvalMissingGraph(42)
	findings := rules.NewEvalMissing().Analyze(g)
	if len(findings) == 0 {
		t.Fatal("expected at least one eval_missing finding")
	}
	if !hasFinding(findings, "eval_missing", domain.Critical) {
		t.Errorf("expected eval_missing Critical finding, got %+v", findings)
	}
}

func TestGenerateEvalMissingGraph_OtherRulesClean(t *testing.T) {
	g := testutil.GenerateEvalMissingGraph(42)
	// Independent rules that should NOT fire on this minimal eval graph.
	other := []domain.AnalysisRule{
		rules.NewCycleDetector(),
		rules.NewLoopGuardChecker(),
		rules.NewReachabilityChecker(),
		rules.NewRedundantLLMDetector(),
		rules.NewPIILeakScanner(),
		rules.NewSecretExposureScanner(),
		rules.NewDeprecatedModelChecker(),
		rules.NewMaxParallelBranchesChecker(),
		rules.NewPromptInjectionSink(),
	}
	for _, r := range other {
		if fs := r.Analyze(g); len(fs) != 0 {
			t.Errorf("rule %q: expected 0 findings on eval-missing graph, got %d: %+v", r.Name(), len(fs), fs)
		}
	}
}

// ---- GenerateDynamicNodeConstructionGraph ----

func TestGenerateDynamicNodeConstructionGraph_ReturnsValidGraph(t *testing.T) {
	g := testutil.GenerateDynamicNodeConstructionGraph(42)
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

func TestGenerateDynamicNodeConstructionGraph_TriggersCriticalFinding(t *testing.T) {
	g := testutil.GenerateDynamicNodeConstructionGraph(42)
	findings := rules.NewDynamicNodeConstruction().Analyze(g)
	if len(findings) == 0 {
		t.Fatal("expected at least one dynamic_node_construction finding")
	}
	if !hasFinding(findings, "dynamic_node_construction", domain.Critical) {
		t.Errorf("expected dynamic_node_construction Critical finding, got %+v", findings)
	}
}

func TestGenerateDynamicNodeConstructionGraph_OtherRulesClean(t *testing.T) {
	g := testutil.GenerateDynamicNodeConstructionGraph(42)
	// Independent rules that should NOT fire on this minimal dynamic graph.
	// secret_exposure_scanner is excluded because the eval body could in
	// principle trip a secret pattern depending on the literal content;
	// here it does not, but we keep coverage tight.
	other := []domain.AnalysisRule{
		rules.NewCycleDetector(),
		rules.NewLoopGuardChecker(),
		rules.NewReachabilityChecker(),
		rules.NewRedundantLLMDetector(),
		rules.NewPIILeakScanner(),
		rules.NewSecretExposureScanner(),
		rules.NewDeprecatedModelChecker(),
		rules.NewMaxParallelBranchesChecker(),
		rules.NewPromptInjectionSink(),
		rules.NewEvalMissing(),
	}
	for _, r := range other {
		if fs := r.Analyze(g); len(fs) != 0 {
			t.Errorf("rule %q: expected 0 findings on dynamic-construction graph, got %d: %+v", r.Name(), len(fs), fs)
		}
	}
}

func TestGenerateN8nGraph_ReturnsValidGraph(t *testing.T) {
	g := testutil.GenerateN8nGraph(42)
	if g == nil {
		t.Fatal("GenerateN8nGraph returned nil")
	}
	if len(g.Nodes) != 3 {
		t.Errorf("len(Nodes) = %d, want 3", len(g.Nodes))
	}
	if len(g.Edges) != 2 {
		t.Errorf("len(Edges) = %d, want 2", len(g.Edges))
	}
	if g.EntryNodeID != "Webhook" {
		t.Errorf("EntryNodeID = %q, want \"Webhook\"", g.EntryNodeID)
	}
}

func TestGenerateN8nGraph_NodeTypes(t *testing.T) {
	g := testutil.GenerateN8nGraph(42)
	if g.Nodes["Webhook"].Type != domain.NodeTypeTool {
		t.Errorf("Webhook type = %v, want NodeTypeTool", g.Nodes["Webhook"].Type)
	}
	if g.Nodes["ChatGPT"].Type != domain.NodeTypeLLM {
		t.Errorf("ChatGPT type = %v, want NodeTypeLLM", g.Nodes["ChatGPT"].Type)
	}
	if g.Nodes["HTTP Request"].Type != domain.NodeTypeTool {
		t.Errorf("HTTP Request type = %v, want NodeTypeTool", g.Nodes["HTTP Request"].Type)
	}
	// Webhook has trigger category — used by the parser's entry-detection.
	if cat, _ := g.Nodes["Webhook"].Config["category"].(string); cat != "trigger" {
		t.Errorf("Webhook category = %q, want \"trigger\"", cat)
	}
}

func TestGenerateN8nGraph_Reproducible(t *testing.T) {
	a := testutil.GenerateN8nGraph(42)
	b := testutil.GenerateN8nGraph(42)
	if len(a.Nodes) != len(b.Nodes) || len(a.Edges) != len(b.Edges) {
		t.Errorf("seed=42 produced different graphs: %v vs %v", a, b)
	}
}
