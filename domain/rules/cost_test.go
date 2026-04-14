package rules_test

import (
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/domain/testutil"
)

func TestCostAnalyzer_Name(t *testing.T) {
	c := rules.NewCostAnalyzer()
	if got := c.Name(); got != "cost_estimation" {
		t.Errorf("Name() = %q, want %q", got, "cost_estimation")
	}
}

// Case 1: 高額モデル(gpt-4o)がループ内 → Warning
func TestCostAnalyzer_Warning_HighCostModelInLoop(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("loop", domain.NodeTypeLoop, map[string]any{"max_iterations": 5}).
		AddNodeWithConfig("llm", domain.NodeTypeLLM, map[string]any{"model": "gpt-4o"}).
		AddNode("entry", domain.NodeTypeLLM).
		AddEdge("entry", "loop").
		AddEdge("loop", "llm").
		AddEdge("llm", "loop"). // creates cycle: loop and llm are both in the loop
		Entry("entry"))

	findings := rules.NewCostAnalyzer().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Warning {
		t.Errorf("Severity = %v, want Warning", f.Severity)
	}
	if f.NodeID != "llm" {
		t.Errorf("NodeID = %q, want %q", f.NodeID, "llm")
	}
	if f.RuleName != "cost_estimation" {
		t.Errorf("RuleName = %q, want %q", f.RuleName, "cost_estimation")
	}
}

// Case 2: 高額モデル(claude-3-opus)がループ外 + task_complexity=simple → Info
func TestCostAnalyzer_Info_HighCostModelSimpleTask(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("llm", domain.NodeTypeLLM, map[string]any{
			"model":           "claude-3-opus",
			"task_complexity": "simple",
		}).
		AddNode("out", domain.NodeTypeOutput).
		AddEdge("llm", "out").
		Entry("llm"))

	findings := rules.NewCostAnalyzer().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Info {
		t.Errorf("Severity = %v, want Info", f.Severity)
	}
	if f.NodeID != "llm" {
		t.Errorf("NodeID = %q, want %q", f.NodeID, "llm")
	}
}

// Case 3: 低額モデル(gpt-4o-mini)がループ内 → 検出なし
func TestCostAnalyzer_NoFindings_LowCostModelInLoop(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("loop", domain.NodeTypeLoop, map[string]any{"max_iterations": 5}).
		AddNodeWithConfig("llm", domain.NodeTypeLLM, map[string]any{"model": "gpt-4o-mini"}).
		AddEdge("loop", "llm").
		AddEdge("llm", "loop").
		Entry("loop"))

	findings := rules.NewCostAnalyzer().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// Case 4: 未知モデルがループ内 → 中額扱いなので検出なし
func TestCostAnalyzer_NoFindings_UnknownModelInLoop(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("loop", domain.NodeTypeLoop, map[string]any{"max_iterations": 3}).
		AddNodeWithConfig("llm", domain.NodeTypeLLM, map[string]any{"model": "some-unknown-model"}).
		AddEdge("loop", "llm").
		AddEdge("llm", "loop").
		Entry("loop"))

	findings := rules.NewCostAnalyzer().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for unknown model (mid-tier), got %d: %+v", len(findings), findings)
	}
}

// Case 5: model未設定(空文字列)のLLMノード → 中額扱い、検出なし
func TestCostAnalyzer_NoFindings_NoModel(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNode("llm", domain.NodeTypeLLM).
		AddNode("out", domain.NodeTypeOutput).
		AddEdge("llm", "out").
		Entry("llm"))

	findings := rules.NewCostAnalyzer().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for node with no model, got %d: %+v", len(findings), findings)
	}
}

// Case 6: 高額モデル(gemini-1.5-pro) + task_complexity=complex → 検出なし
func TestCostAnalyzer_NoFindings_HighCostModelComplexTask(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("llm", domain.NodeTypeLLM, map[string]any{
			"model":           "gemini-1.5-pro",
			"task_complexity": "complex",
		}).
		AddNode("out", domain.NodeTypeOutput).
		AddEdge("llm", "out").
		Entry("llm"))

	findings := rules.NewCostAnalyzer().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for complex task with high-cost model, got %d: %+v", len(findings), findings)
	}
}

// Case 7: 高額モデルがループ外 + task_complexity未設定 → 検出なし
func TestCostAnalyzer_NoFindings_HighCostNoComplexity(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("llm", domain.NodeTypeLLM, map[string]any{
			"model": "claude-3-5-sonnet",
		}).
		AddNode("out", domain.NodeTypeOutput).
		AddEdge("llm", "out").
		Entry("llm"))

	findings := rules.NewCostAnalyzer().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (no task_complexity set), got %d: %+v", len(findings), findings)
	}
}

// Case 8: nil グラフ → パニックせずfindings ゼロ
func TestCostAnalyzer_NilGraph(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Analyze(nil) panicked: %v", r)
		}
	}()
	findings := rules.NewCostAnalyzer().Analyze(nil)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for nil graph, got %d", len(findings))
	}
}
