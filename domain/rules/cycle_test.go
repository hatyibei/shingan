package rules_test

import (
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/domain/testutil"
)

// helper: build a graph and fail fast on error.
func mustBuild(t *testing.T, b *testutil.Builder) *domain.WorkflowGraph {
	t.Helper()
	g, err := b.Build()
	if err != nil {
		t.Fatalf("testutil.Builder.Build() error: %v", err)
	}
	return g
}

func TestCycleDetector_Name(t *testing.T) {
	d := rules.NewCycleDetector()
	if got := d.Name(); got != "cycle_detection" {
		t.Errorf("Name() = %q, want %q", got, "cycle_detection")
	}
}

// Case 1: 正常グラフ（サイクルなし）→ 検出ゼロ
func TestCycleDetector_NoFindings_LinearGraph(t *testing.T) {
	// a → b → c (no cycle)
	g := mustBuild(t, testutil.NewBuilder().
		AddNode("a", domain.NodeTypeLLM).
		AddNode("b", domain.NodeTypeTool).
		AddNode("c", domain.NodeTypeOutput).
		AddEdge("a", "b").
		AddEdge("b", "c").
		Entry("a"))

	findings := rules.NewCycleDetector().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// Case 2: LoopAgent (Control) + max_iterations < 100 → 正常ループ、検出ゼロ
func TestCycleDetector_NoFindings_SafeLoopAgent(t *testing.T) {
	// entry → loop (Control, max_iterations=5) → work → loop (back edge)
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("loop", domain.NodeTypeControl, map[string]any{"max_iterations": 5}).
		AddNode("work", domain.NodeTypeLLM).
		AddNode("entry", domain.NodeTypeLLM).
		AddEdge("entry", "loop").
		AddEdge("loop", "work").
		AddEdge("work", "loop"). // back edge: creates cycle at "loop"
		Entry("entry"))

	findings := rules.NewCycleDetector().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for safe loop, got %d: %+v", len(findings), findings)
	}
}

// Case 3: LoopAgent + max_iterations 未設定 → Critical
func TestCycleDetector_Critical_LoopAgentNoMaxIterations(t *testing.T) {
	// loop (Control, no max_iterations) ← self-loop
	g := mustBuild(t, testutil.NewBuilder().
		AddNode("loop", domain.NodeTypeControl).
		AddNode("entry", domain.NodeTypeLLM).
		AddEdge("entry", "loop").
		AddEdge("loop", "loop"). // self-cycle
		Entry("entry"))

	findings := rules.NewCycleDetector().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", f.Severity)
	}
	if f.NodeID != "loop" {
		t.Errorf("NodeID = %q, want %q", f.NodeID, "loop")
	}
	if f.RuleName != "cycle_detection" {
		t.Errorf("RuleName = %q, want %q", f.RuleName, "cycle_detection")
	}
}

// Case 4: LoopAgent + max_iterations >= 100 → Warning
func TestCycleDetector_Warning_LoopAgentHighMaxIterations(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("loop", domain.NodeTypeControl, map[string]any{"max_iterations": 1000}).
		AddNode("work", domain.NodeTypeTool).
		AddNode("entry", domain.NodeTypeLLM).
		AddEdge("entry", "loop").
		AddEdge("loop", "work").
		AddEdge("work", "loop").
		Entry("entry"))

	findings := rules.NewCycleDetector().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Warning {
		t.Errorf("Severity = %v, want Warning", f.Severity)
	}
	if f.NodeID != "loop" {
		t.Errorf("NodeID = %q, want %q", f.NodeID, "loop")
	}
}

// Case 5: 非Controlノードがサイクルを形成 → Critical (グラフ定義誤り)
func TestCycleDetector_Critical_NonControlCycle(t *testing.T) {
	// a (LLM) → b (LLM) → a  (LLMどうしの直接サイクル = 定義誤り)
	g := mustBuild(t, testutil.NewBuilder().
		AddNode("a", domain.NodeTypeLLM).
		AddNode("b", domain.NodeTypeLLM).
		AddEdge("a", "b").
		AddEdge("b", "a"). // back edge to non-Control node
		Entry("a"))

	findings := rules.NewCycleDetector().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", f.Severity)
	}
}

// Case 6: max_iterations が境界値 99 → 正常（検出ゼロ）
func TestCycleDetector_NoFindings_MaxIterations99(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("loop", domain.NodeTypeControl, map[string]any{"max_iterations": 99}).
		AddNode("work", domain.NodeTypeLLM).
		AddEdge("loop", "work").
		AddEdge("work", "loop").
		Entry("loop"))

	findings := rules.NewCycleDetector().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for max_iterations=99, got %d: %+v", len(findings), findings)
	}
}

// Case 7: max_iterations が境界値 100 → Warning
func TestCycleDetector_Warning_MaxIterations100(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("loop", domain.NodeTypeControl, map[string]any{"max_iterations": 100}).
		AddNode("work", domain.NodeTypeLLM).
		AddEdge("loop", "work").
		AddEdge("work", "loop").
		Entry("loop"))

	findings := rules.NewCycleDetector().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for max_iterations=100, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Warning {
		t.Errorf("Severity = %v, want Warning", findings[0].Severity)
	}
}

// Case 8: nil グラフ → パニックせず findings ゼロ
func TestCycleDetector_NilGraph(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Analyze(nil) panicked: %v", r)
		}
	}()
	findings := rules.NewCycleDetector().Analyze(nil)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for nil graph, got %d", len(findings))
	}
}

// Case 9: max_iterations が float64 (JSON unmarshal 由来) → 正しく扱う
func TestCycleDetector_MaxIterationsFloat64(t *testing.T) {
	// JSON unmarshal では数値が float64 になることがある
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("loop", domain.NodeTypeControl, map[string]any{"max_iterations": float64(200)}).
		AddNode("work", domain.NodeTypeLLM).
		AddEdge("loop", "work").
		AddEdge("work", "loop").
		Entry("loop"))

	findings := rules.NewCycleDetector().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Warning {
		t.Errorf("Severity = %v, want Warning", findings[0].Severity)
	}
}
