package rules_test

import (
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/domain/testutil"
)

func TestRedundantLLMDetector_Name(t *testing.T) {
	r := rules.NewRedundantLLMDetector()
	if got := r.Name(); got != "redundant_llm_call" {
		t.Errorf("Name() = %q, want %q", got, "redundant_llm_call")
	}
}

// Case 1: 同一model+prompt_templateのノードが2個 → Warning×2
func TestRedundantLLMDetector_Warning_TwoDuplicates(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("llm1", domain.NodeTypeLLM, map[string]any{
			"model":           "gpt-4o",
			"prompt_template": "Summarize: {{input}}",
		}).
		AddNodeWithConfig("llm2", domain.NodeTypeLLM, map[string]any{
			"model":           "gpt-4o",
			"prompt_template": "Summarize: {{input}}",
		}).
		AddNode("out", domain.NodeTypeOutput).
		AddEdge("llm1", "out").
		AddEdge("llm2", "out").
		Entry("llm1"))

	findings := rules.NewRedundantLLMDetector().Analyze(g)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d: %+v", len(findings), findings)
	}
	for _, f := range findings {
		if f.Severity != domain.Warning {
			t.Errorf("Severity = %v, want Warning", f.Severity)
		}
		if f.RuleName != "redundant_llm_call" {
			t.Errorf("RuleName = %q, want %q", f.RuleName, "redundant_llm_call")
		}
	}
}

// Case 2: modelが異なる → 検出なし
func TestRedundantLLMDetector_NoFindings_DifferentModel(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("llm1", domain.NodeTypeLLM, map[string]any{
			"model":           "gpt-4o",
			"prompt_template": "Summarize: {{input}}",
		}).
		AddNodeWithConfig("llm2", domain.NodeTypeLLM, map[string]any{
			"model":           "gpt-4o-mini",
			"prompt_template": "Summarize: {{input}}",
		}).
		AddNode("out", domain.NodeTypeOutput).
		AddEdge("llm1", "out").
		AddEdge("llm2", "out").
		Entry("llm1"))

	findings := rules.NewRedundantLLMDetector().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for different models, got %d: %+v", len(findings), findings)
	}
}

// Case 3: prompt_templateが異なる → 検出なし
func TestRedundantLLMDetector_NoFindings_DifferentPrompt(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("llm1", domain.NodeTypeLLM, map[string]any{
			"model":           "gpt-4o",
			"prompt_template": "Summarize: {{input}}",
		}).
		AddNodeWithConfig("llm2", domain.NodeTypeLLM, map[string]any{
			"model":           "gpt-4o",
			"prompt_template": "Translate: {{input}}",
		}).
		AddNode("out", domain.NodeTypeOutput).
		AddEdge("llm1", "out").
		AddEdge("llm2", "out").
		Entry("llm1"))

	findings := rules.NewRedundantLLMDetector().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for different prompts, got %d: %+v", len(findings), findings)
	}
}

// Case 4: 同一キーのノードが3個 → Warning×3
func TestRedundantLLMDetector_Warning_ThreeDuplicates(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("llm1", domain.NodeTypeLLM, map[string]any{
			"model":           "claude-3-haiku",
			"prompt_template": "Classify: {{text}}",
		}).
		AddNodeWithConfig("llm2", domain.NodeTypeLLM, map[string]any{
			"model":           "claude-3-haiku",
			"prompt_template": "Classify: {{text}}",
		}).
		AddNodeWithConfig("llm3", domain.NodeTypeLLM, map[string]any{
			"model":           "claude-3-haiku",
			"prompt_template": "Classify: {{text}}",
		}).
		AddNode("out", domain.NodeTypeOutput).
		AddEdge("llm1", "out").
		AddEdge("llm2", "out").
		AddEdge("llm3", "out").
		Entry("llm1"))

	findings := rules.NewRedundantLLMDetector().Analyze(g)
	if len(findings) != 3 {
		t.Fatalf("expected 3 findings, got %d: %+v", len(findings), findings)
	}
	for _, f := range findings {
		if f.Severity != domain.Warning {
			t.Errorf("Severity = %v, want Warning", f.Severity)
		}
	}
}

// Case 5: prompt_template未設定のLLMノードはスキップ
func TestRedundantLLMDetector_NoFindings_NoPromptTemplate(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("llm1", domain.NodeTypeLLM, map[string]any{
			"model": "gpt-4o",
		}).
		AddNodeWithConfig("llm2", domain.NodeTypeLLM, map[string]any{
			"model": "gpt-4o",
		}).
		AddNode("out", domain.NodeTypeOutput).
		AddEdge("llm1", "out").
		AddEdge("llm2", "out").
		Entry("llm1"))

	findings := rules.NewRedundantLLMDetector().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when prompt_template is not set, got %d: %+v", len(findings), findings)
	}
}

// Case 6: LLMノード以外は無視される
func TestRedundantLLMDetector_IgnoresNonLLMNodes(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("tool1", domain.NodeTypeTool, map[string]any{
			"model":           "gpt-4o",
			"prompt_template": "Do thing",
		}).
		AddNodeWithConfig("tool2", domain.NodeTypeTool, map[string]any{
			"model":           "gpt-4o",
			"prompt_template": "Do thing",
		}).
		AddNode("out", domain.NodeTypeOutput).
		AddEdge("tool1", "out").
		AddEdge("tool2", "out").
		Entry("tool1"))

	findings := rules.NewRedundantLLMDetector().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for non-LLM nodes, got %d: %+v", len(findings), findings)
	}
}

// Case 7: nil グラフ → パニックせず findings ゼロ
func TestRedundantLLMDetector_NilGraph(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Analyze(nil) panicked: %v", r)
		}
	}()
	findings := rules.NewRedundantLLMDetector().Analyze(nil)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for nil graph, got %d", len(findings))
	}
}
