package rules_test

import (
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/domain/testutil"
)

// buildEval is a tiny helper mirroring the buildPI helper in
// prompt_injection_sink_test.go. It builds a graph from a Builder and fails
// the test on configuration errors.
func buildEval(t *testing.T, b *testutil.Builder) *domain.WorkflowGraph {
	t.Helper()
	g, err := b.Build()
	if err != nil {
		t.Fatalf("testutil.Builder.Build() failed: %v", err)
	}
	return g
}

// ─── Case 1: LLM → code_execution Tool 直接 (Critical) ──────────────────────

func TestEvalMissing_DirectFlow_Critical(t *testing.T) {
	g := buildEval(t, testutil.NewBuilder().
		AddNode("llm_node", domain.NodeTypeLLM).
		AddNodeWithConfig("eval_tool", domain.NodeTypeTool, map[string]any{
			"category": "code_execution",
		}).
		AddEdge("llm_node", "eval_tool").
		Entry("llm_node"))

	findings := rules.NewEvalMissing().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", f.Severity)
	}
	if f.RuleName != "eval_missing" {
		t.Errorf("RuleName = %q, want %q", f.RuleName, "eval_missing")
	}
	if f.Confidence != 0.9 {
		t.Errorf("Confidence = %.2f, want 0.9", f.Confidence)
	}
	if f.ConfidenceReason != domain.ReasonHeuristicPattern {
		t.Errorf("ConfidenceReason = %q, want %q", f.ConfidenceReason, domain.ReasonHeuristicPattern)
	}
}

// ─── Case 2: LLM → Condition → code_execution Tool (Warning, downgrade) ─────

func TestEvalMissing_ConditionDowngrades_Warning(t *testing.T) {
	g := buildEval(t, testutil.NewBuilder().
		AddNode("llm_node", domain.NodeTypeLLM).
		AddConditionNode("validator", "is_safe(input)").
		AddNodeWithConfig("eval_tool", domain.NodeTypeTool, map[string]any{
			"category": "code_execution",
		}).
		AddEdge("llm_node", "validator").
		AddEdge("validator", "eval_tool").
		Entry("llm_node"))

	findings := rules.NewEvalMissing().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Warning {
		t.Errorf("Severity = %v, want Warning (Condition downgrade)", f.Severity)
	}
	if f.Confidence != 0.6 {
		t.Errorf("Confidence = %.2f, want 0.6", f.Confidence)
	}
	if f.ConfidenceReason != domain.ReasonHeuristicPattern {
		t.Errorf("ConfidenceReason = %q, want %q", f.ConfidenceReason, domain.ReasonHeuristicPattern)
	}
}

// ─── Case 3: LLM → Human → code_execution (skipped, Human gate) ─────────────

func TestEvalMissing_HumanGate_NoFinding(t *testing.T) {
	g := buildEval(t, testutil.NewBuilder().
		AddNode("llm_node", domain.NodeTypeLLM).
		AddNode("approver", domain.NodeTypeHuman).
		AddNodeWithConfig("eval_tool", domain.NodeTypeTool, map[string]any{
			"category": "code_execution",
		}).
		AddEdge("llm_node", "approver").
		AddEdge("approver", "eval_tool").
		Entry("llm_node"))

	findings := rules.NewEvalMissing().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (Human gate present), got %d: %+v", len(findings), findings)
	}
}

// ─── Case 4: LLM → non-eval Tool, no finding ────────────────────────────────

func TestEvalMissing_NonEvalSink_NoFinding(t *testing.T) {
	g := buildEval(t, testutil.NewBuilder().
		AddNode("llm_node", domain.NodeTypeLLM).
		AddNodeWithConfig("api_tool", domain.NodeTypeTool, map[string]any{
			"category": "api",
		}).
		AddEdge("llm_node", "api_tool").
		Entry("llm_node"))

	findings := rules.NewEvalMissing().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (non-eval sink), got %d: %+v", len(findings), findings)
	}
}

// ─── Case 5: code_execution Tool but no LLM source — no finding ────────────

func TestEvalMissing_NoLLMSource_NoFinding(t *testing.T) {
	g := buildEval(t, testutil.NewBuilder().
		AddNodeWithConfig("eval_tool", domain.NodeTypeTool, map[string]any{
			"category": "code_execution",
		}).
		AddNode("output", domain.NodeTypeOutput).
		AddEdge("eval_tool", "output").
		Entry("eval_tool"))

	findings := rules.NewEvalMissing().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (no LLM source), got %d: %+v", len(findings), findings)
	}
}

// ─── Case 6: sink classified by Config["tool"] ──────────────────────────────

func TestEvalMissing_SinkByToolField(t *testing.T) {
	cases := []string{"eval", "exec", "code_interpreter", "python_runner", "shell"}
	for _, tool := range cases {
		t.Run(tool, func(t *testing.T) {
			g := buildEval(t, testutil.NewBuilder().
				AddNode("llm_node", domain.NodeTypeLLM).
				AddNodeWithConfig("sink", domain.NodeTypeTool, map[string]any{
					"tool": tool,
				}).
				AddEdge("llm_node", "sink").
				Entry("llm_node"))

			findings := rules.NewEvalMissing().Analyze(g)
			if len(findings) != 1 {
				t.Fatalf("expected 1 finding for tool=%q, got %d", tool, len(findings))
			}
			if findings[0].Severity != domain.Critical {
				t.Errorf("Severity = %v, want Critical", findings[0].Severity)
			}
		})
	}
}

// ─── Case 7: sink classified by name pattern ─────────────────────────────────

func TestEvalMissing_SinkByNamePattern(t *testing.T) {
	cases := []string{"eval_runner", "shell_exec", "PythonRunner", "Bash_node", "code_runner"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			g := buildEval(t, testutil.NewBuilder().
				AddNode("llm_node", domain.NodeTypeLLM).
				AddNode(name, domain.NodeTypeTool).
				AddEdge("llm_node", name).
				Entry("llm_node"))

			findings := rules.NewEvalMissing().Analyze(g)
			if len(findings) != 1 {
				t.Fatalf("expected 1 finding for name=%q, got %d", name, len(findings))
			}
		})
	}
}

// ─── Case 8: sink by Config["category"] == "code_eval" ──────────────────────

func TestEvalMissing_SinkByCodeEvalCategory(t *testing.T) {
	g := buildEval(t, testutil.NewBuilder().
		AddNode("llm_node", domain.NodeTypeLLM).
		AddNodeWithConfig("eval_tool", domain.NodeTypeTool, map[string]any{
			"category": "code_eval",
		}).
		AddEdge("llm_node", "eval_tool").
		Entry("llm_node"))

	findings := rules.NewEvalMissing().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for category=code_eval, got %d", len(findings))
	}
	if findings[0].Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", findings[0].Severity)
	}
}

// ─── Case 9: nil graph does not panic ───────────────────────────────────────

func TestEvalMissing_NilGraph(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Analyze(nil) panicked: %v", r)
		}
	}()
	findings := rules.NewEvalMissing().Analyze(nil)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for nil graph, got %d", len(findings))
	}
}

// ─── Case 10: Name() and Meta() ─────────────────────────────────────────────

func TestEvalMissing_NameAndMeta(t *testing.T) {
	r := rules.NewEvalMissing()
	if got := r.Name(); got != "eval_missing" {
		t.Errorf("Name() = %q, want %q", got, "eval_missing")
	}
	m := r.Meta()
	if m.Name != "eval_missing" {
		t.Errorf("Meta().Name = %q, want %q", m.Name, "eval_missing")
	}
	if m.Severity != domain.Critical {
		t.Errorf("Meta().Severity = %v, want Critical (default)", m.Severity)
	}
}

// ─── Case 11: implements PathRule and AnalysisRule ──────────────────────────

func TestEvalMissing_InterfaceAssertion(t *testing.T) {
	var r any = rules.NewEvalMissing()
	if _, ok := r.(domain.PathRule); !ok {
		t.Errorf("EvalMissing does not implement domain.PathRule")
	}
	if _, ok := r.(domain.AnalysisRule); !ok {
		t.Errorf("EvalMissing does not implement domain.AnalysisRule")
	}
}

// ─── Case 12: Human gate beats Condition (skip wins over downgrade) ─────────

func TestEvalMissing_HumanBeforeConditionStillSkips(t *testing.T) {
	// LLM → Condition → Human → eval. The Human node should make the rule skip
	// even though there's a Condition on the path.
	g := buildEval(t, testutil.NewBuilder().
		AddNode("llm_node", domain.NodeTypeLLM).
		AddConditionNode("cond", "is_safe(x)").
		AddNode("approver", domain.NodeTypeHuman).
		AddNodeWithConfig("eval_tool", domain.NodeTypeTool, map[string]any{
			"category": "code_execution",
		}).
		AddEdge("llm_node", "cond").
		AddEdge("cond", "approver").
		AddEdge("approver", "eval_tool").
		Entry("llm_node"))

	findings := rules.NewEvalMissing().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (Human gate beats Condition), got %d: %+v", len(findings), findings)
	}
}

// ─── Case 13: every Finding has ConfidenceReason ────────────────────────────

func TestEvalMissing_ConfidenceReasonStamped(t *testing.T) {
	g := buildEval(t, testutil.NewBuilder().
		// Critical path: direct LLM → eval
		AddNode("llm_a", domain.NodeTypeLLM).
		AddNodeWithConfig("eval_a", domain.NodeTypeTool, map[string]any{
			"category": "code_execution",
		}).
		AddEdge("llm_a", "eval_a").
		// Warning path: LLM → Condition → eval
		AddNode("llm_b", domain.NodeTypeLLM).
		AddConditionNode("cond_b", "ok(x)").
		AddNodeWithConfig("eval_b", domain.NodeTypeTool, map[string]any{
			"category": "code_execution",
		}).
		AddEdge("llm_b", "cond_b").
		AddEdge("cond_b", "eval_b").
		Entry("llm_a"))

	findings := rules.NewEvalMissing().Analyze(g)
	if len(findings) == 0 {
		t.Fatalf("expected ≥1 finding, got 0")
	}
	for _, f := range findings {
		if f.ConfidenceReason == "" {
			t.Errorf("finding for %q missing ConfidenceReason: %+v", f.NodeID, f)
		}
	}
	severities := map[domain.Severity]int{}
	for _, f := range findings {
		severities[f.Severity]++
	}
	if severities[domain.Critical] == 0 {
		t.Errorf("expected at least one Critical, got %v", severities)
	}
	if severities[domain.Warning] == 0 {
		t.Errorf("expected at least one Warning, got %v", severities)
	}
}

// ─── Case 14: suggestion mentions sandbox/parameterized tool-call ──────────

func TestEvalMissing_SuggestionMentionsSandbox(t *testing.T) {
	g := buildEval(t, testutil.NewBuilder().
		AddNode("llm_node", domain.NodeTypeLLM).
		AddNodeWithConfig("eval_tool", domain.NodeTypeTool, map[string]any{
			"category": "code_execution",
		}).
		AddEdge("llm_node", "eval_tool").
		Entry("llm_node"))

	findings := rules.NewEvalMissing().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if !strings.Contains(strings.ToLower(findings[0].Suggestion), "sandbox") &&
		!strings.Contains(strings.ToLower(findings[0].Suggestion), "validate") {
		t.Errorf("Suggestion should mention sandbox/validate, got: %s", findings[0].Suggestion)
	}
}

// ─── Case 15: legacy AnalysisRule path (Analyze) returns same as Propagate ─

func TestEvalMissing_LegacyAnalyzePath(t *testing.T) {
	g := buildEval(t, testutil.NewBuilder().
		AddNode("llm_node", domain.NodeTypeLLM).
		AddNodeWithConfig("eval_tool", domain.NodeTypeTool, map[string]any{
			"category": "code_execution",
		}).
		AddEdge("llm_node", "eval_tool").
		Entry("llm_node"))

	r := rules.NewEvalMissing()
	findings := r.Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("Analyze: expected 1 finding, got %d", len(findings))
	}
	if findings[0].NodeID != "eval_tool" {
		t.Errorf("Analyze: NodeID = %q, want %q", findings[0].NodeID, "eval_tool")
	}
}
