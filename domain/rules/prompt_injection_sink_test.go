package rules_test

import (
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/domain/testutil"
)

// buildPI is a small helper mirroring the buildPII helper in pii_leak_test.go.
// It builds a graph from a Builder and fails the test on configuration errors.
func buildPI(t *testing.T, b *testutil.Builder) *domain.WorkflowGraph {
	t.Helper()
	g, err := b.Build()
	if err != nil {
		t.Fatalf("testutil.Builder.Build() failed: %v", err)
	}
	return g
}

// ─── Case 1: user_input → LLM with system_prompt template (Critical) ────────

func TestPromptInjectionSink_DirectFlow(t *testing.T) {
	g := buildPI(t, testutil.NewBuilder().
		AddNodeWithConfig("user_query", domain.NodeTypeTool, map[string]any{
			"source": "user_input",
		}).
		AddNodeWithConfig("llm_node", domain.NodeTypeLLM, map[string]any{
			"model":         "gpt-4o-mini",
			"system_prompt": "You are an assistant. Context: {{user_query}}.",
		}).
		AddEdge("user_query", "llm_node").
		Entry("user_query"))

	findings := rules.NewPromptInjectionSink().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", f.Severity)
	}
	if f.NodeID != "llm_node" {
		t.Errorf("NodeID = %q, want %q", f.NodeID, "llm_node")
	}
	if f.RuleName != "prompt_injection_sink" {
		t.Errorf("RuleName = %q, want %q", f.RuleName, "prompt_injection_sink")
	}
	if f.Confidence != 0.9 {
		t.Errorf("Confidence = %.2f, want 0.9", f.Confidence)
	}
	if f.ConfidenceReason != domain.ReasonHeuristicPattern {
		t.Errorf("ConfidenceReason = %q, want %q", f.ConfidenceReason, domain.ReasonHeuristicPattern)
	}
}

// ─── Case 2: user input present but does NOT reach an LLM template (0) ──────

func TestPromptInjectionSink_NoSink(t *testing.T) {
	g := buildPI(t, testutil.NewBuilder().
		AddNodeWithConfig("user_query", domain.NodeTypeTool, map[string]any{
			"source": "user_input",
		}).
		AddNode("output", domain.NodeTypeOutput).
		AddEdge("user_query", "output").
		Entry("user_query"))

	findings := rules.NewPromptInjectionSink().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (no LLM sink), got %d: %+v", len(findings), findings)
	}
}

// ─── Case 3: LLM template exists but no user input source (0) ───────────────

func TestPromptInjectionSink_NoSource(t *testing.T) {
	g := buildPI(t, testutil.NewBuilder().
		AddNode("entry_llm", domain.NodeTypeLLM).
		AddNodeWithConfig("llm_template", domain.NodeTypeLLM, map[string]any{
			"model":         "gpt-4o-mini",
			"system_prompt": "Process the data: {{ctx}}.",
		}).
		AddEdge("entry_llm", "llm_template").
		Entry("entry_llm"))

	findings := rules.NewPromptInjectionSink().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (no user input source), got %d: %+v", len(findings), findings)
	}
}

// ─── Case 4: user_input → Tool → LLM (indirect path via tool) ───────────────

func TestPromptInjectionSink_IndirectViaTool(t *testing.T) {
	g := buildPI(t, testutil.NewBuilder().
		AddNodeWithConfig("user_request", domain.NodeTypeTool, map[string]any{
			"source": "user_input",
		}).
		AddNodeWithConfig("preprocess", domain.NodeTypeTool, map[string]any{
			"category": "transform",
		}).
		AddNodeWithConfig("llm_node", domain.NodeTypeLLM, map[string]any{
			"model":         "gpt-4o-mini",
			"system_prompt": "Answer based on: {{request}}",
		}).
		AddEdge("user_request", "preprocess").
		AddEdge("preprocess", "llm_node").
		Entry("user_request"))

	findings := rules.NewPromptInjectionSink().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (indirect path), got %d: %+v", len(findings), findings)
	}
	if findings[0].NodeID != "llm_node" {
		t.Errorf("NodeID = %q, want %q", findings[0].NodeID, "llm_node")
	}
	if findings[0].Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", findings[0].Severity)
	}
}

// ─── Case 5: every Finding has a non-empty ConfidenceReason ─────────────────

func TestPromptInjectionSink_ConfidenceReasonStamped(t *testing.T) {
	g := buildPI(t, testutil.NewBuilder().
		// Critical: system_prompt with substitution.
		AddNodeWithConfig("user_in_a", domain.NodeTypeTool, map[string]any{
			"source": "user_input",
		}).
		AddNodeWithConfig("llm_critical", domain.NodeTypeLLM, map[string]any{
			"model":         "gpt-4o-mini",
			"system_prompt": "System: {{user_in_a}}",
		}).
		AddEdge("user_in_a", "llm_critical").
		// Warning: system_prompt without substitution.
		AddNodeWithConfig("user_in_b", domain.NodeTypeTool, map[string]any{
			"source": "user_input",
		}).
		AddNodeWithConfig("llm_warning", domain.NodeTypeLLM, map[string]any{
			"model":         "gpt-4o-mini",
			"system_prompt": "You are a helpful assistant.",
		}).
		AddEdge("user_in_b", "llm_warning").
		// Info: prompt_template with substitution (not system).
		AddNodeWithConfig("user_in_c", domain.NodeTypeTool, map[string]any{
			"source": "user_input",
		}).
		AddNodeWithConfig("llm_info", domain.NodeTypeLLM, map[string]any{
			"model":           "gpt-4o-mini",
			"prompt_template": "Q: {{user_in_c}}",
		}).
		AddEdge("user_in_c", "llm_info").
		Entry("user_in_a"))

	findings := rules.NewPromptInjectionSink().Analyze(g)
	if len(findings) == 0 {
		t.Fatalf("expected ≥1 finding, got 0")
	}
	for _, f := range findings {
		if f.ConfidenceReason == "" {
			t.Errorf("finding %q missing ConfidenceReason: %+v", f.NodeID, f)
		}
	}
	// Verify all three severities materialised.
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
	if severities[domain.Info] == 0 {
		t.Errorf("expected at least one Info, got %v", severities)
	}
}

// ─── Case 6: rule implements the PathRule contract ──────────────────────────

func TestPromptInjectionSink_RequiresGraph(t *testing.T) {
	var r any = rules.NewPromptInjectionSink()
	if _, ok := r.(domain.PathRule); !ok {
		t.Errorf("PromptInjectionSink does not implement domain.PathRule")
	}
	// Also assert AnalysisRule for legacy callers.
	if _, ok := r.(domain.AnalysisRule); !ok {
		t.Errorf("PromptInjectionSink does not implement domain.AnalysisRule")
	}
}

// ─── Case 7: name-pattern source (no Config["source"] flag) ─────────────────

func TestPromptInjectionSink_NameHintSource(t *testing.T) {
	g := buildPI(t, testutil.NewBuilder().
		// "user_query" name matches the user_* pattern.
		AddNode("user_query", domain.NodeTypeTool).
		AddNodeWithConfig("llm_node", domain.NodeTypeLLM, map[string]any{
			"model":         "gpt-4o-mini",
			"system_prompt": "You are an assistant. Context: {{q}}.",
		}).
		AddEdge("user_query", "llm_node").
		Entry("user_query"))

	findings := rules.NewPromptInjectionSink().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (name-hint source), got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", findings[0].Severity)
	}
}

// ─── Case 8: Warning severity for system_prompt without substitution ────────

func TestPromptInjectionSink_WarningWithoutSubstitution(t *testing.T) {
	g := buildPI(t, testutil.NewBuilder().
		AddNodeWithConfig("user_query", domain.NodeTypeTool, map[string]any{
			"source": "user_input",
		}).
		AddNodeWithConfig("llm_node", domain.NodeTypeLLM, map[string]any{
			"model":         "gpt-4o-mini",
			"system_prompt": "You are a helpful assistant.", // no template substitution
		}).
		AddEdge("user_query", "llm_node").
		Entry("user_query"))

	findings := rules.NewPromptInjectionSink().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Warning {
		t.Errorf("Severity = %v, want Warning", findings[0].Severity)
	}
	if findings[0].Confidence != 0.7 {
		t.Errorf("Confidence = %.2f, want 0.7", findings[0].Confidence)
	}
}

// ─── Case 9: Info severity for non-system template with substitution ────────

func TestPromptInjectionSink_InfoForUserMessageTemplate(t *testing.T) {
	g := buildPI(t, testutil.NewBuilder().
		AddNodeWithConfig("user_query", domain.NodeTypeTool, map[string]any{
			"source": "user_input",
		}).
		AddNodeWithConfig("llm_node", domain.NodeTypeLLM, map[string]any{
			"model":           "gpt-4o-mini",
			"prompt_template": "Q: {{user_query}}",
		}).
		AddEdge("user_query", "llm_node").
		Entry("user_query"))

	findings := rules.NewPromptInjectionSink().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Info {
		t.Errorf("Severity = %v, want Info", findings[0].Severity)
	}
	if findings[0].Confidence != 0.5 {
		t.Errorf("Confidence = %.2f, want 0.5", findings[0].Confidence)
	}
}

// ─── Case 10: nil graph does not panic ──────────────────────────────────────

func TestPromptInjectionSink_NilGraph(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Analyze(nil) panicked: %v", r)
		}
	}()
	findings := rules.NewPromptInjectionSink().Analyze(nil)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for nil graph, got %d", len(findings))
	}
}

// ─── Case 11: Name() and Meta() ─────────────────────────────────────────────

func TestPromptInjectionSink_NameAndMeta(t *testing.T) {
	r := rules.NewPromptInjectionSink()
	if got := r.Name(); got != "prompt_injection_sink" {
		t.Errorf("Name() = %q, want %q", got, "prompt_injection_sink")
	}
	if r.Meta().Name != "prompt_injection_sink" {
		t.Errorf("Meta().Name = %q, want %q", r.Meta().Name, "prompt_injection_sink")
	}
}

// ─── Case 12: ${var} and {var} substitution patterns are recognised ─────────

func TestPromptInjectionSink_DollarAndBraceSubstitution(t *testing.T) {
	cases := []struct {
		name     string
		template string
	}{
		{"dollar_brace", "System: ${user_q}"},
		{"single_brace", "System: {user_q}"},
		{"double_brace", "System: {{user_q}}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := buildPI(t, testutil.NewBuilder().
				AddNodeWithConfig("user_query", domain.NodeTypeTool, map[string]any{
					"source": "user_input",
				}).
				AddNodeWithConfig("llm_node", domain.NodeTypeLLM, map[string]any{
					"model":         "gpt-4o-mini",
					"system_prompt": tc.template,
				}).
				AddEdge("user_query", "llm_node").
				Entry("user_query"))

			findings := rules.NewPromptInjectionSink().Analyze(g)
			if len(findings) != 1 {
				t.Fatalf("expected 1 finding for %s, got %d", tc.name, len(findings))
			}
			if findings[0].Severity != domain.Critical {
				t.Errorf("Severity = %v, want Critical", findings[0].Severity)
			}
		})
	}
}
