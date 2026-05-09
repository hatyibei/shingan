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

// Case 2: LoopAgent (Loop) + max_iterations < 100 → 正常ループ、検出ゼロ
func TestCycleDetector_NoFindings_SafeLoopAgent(t *testing.T) {
	// entry → loop (Control, max_iterations=5) → work → loop (back edge)
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("loop", domain.NodeTypeLoop, map[string]any{"max_iterations": 5}).
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

// Case 3: LoopAgent (NodeTypeLoop) + max_iterations 未設定 → Critical
func TestCycleDetector_Critical_LoopAgentNoMaxIterations(t *testing.T) {
	// loop (Control, no max_iterations) ← self-loop
	g := mustBuild(t, testutil.NewBuilder().
		AddNode("loop", domain.NodeTypeLoop).
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
		AddNodeWithConfig("loop", domain.NodeTypeLoop, map[string]any{"max_iterations": 1000}).
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
		AddNodeWithConfig("loop", domain.NodeTypeLoop, map[string]any{"max_iterations": 99}).
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
		AddNodeWithConfig("loop", domain.NodeTypeLoop, map[string]any{"max_iterations": 100}).
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
		AddNodeWithConfig("loop", domain.NodeTypeLoop, map[string]any{"max_iterations": float64(200)}).
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

// Case 10: LoopAgent管理下サブエージェントのサイクル（max_iter未設定）→ Critical
// entry → loop (Loop, no max_iter) → classifier (LLM) → classifier (back-edge)
// DFSパスに Loop ノード "loop" が存在するが max_iterations 未設定 → Critical（無制限ループリスク）
func TestCycleDetector_Critical_SubAgentCycleUnderLoopAgentNoMaxIter(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNode("loop", domain.NodeTypeLoop).
		AddNode("entry", domain.NodeTypeLLM).
		AddNode("classifier", domain.NodeTypeLLM).
		AddEdge("entry", "loop").
		AddEdge("loop", "classifier").
		AddEdge("classifier", "classifier"). // self-loop on LLM sub-agent
		Entry("entry"))

	findings := rules.NewCycleDetector().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (Critical for sub-agent under LoopAgent with no max_iter), got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical (sub-agent cycle inside Loop node with no max_iterations)", f.Severity)
	}
	if f.NodeID != "classifier" {
		t.Errorf("NodeID = %q, want %q", f.NodeID, "classifier")
	}
}

// Case 11: LoopAgent管理下サブエージェントのサイクル（max_iter >= 100）→ Info
func TestCycleDetector_Info_SubAgentCycleUnderLoopAgentHighMaxIter(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("loop", domain.NodeTypeLoop, map[string]any{"max_iterations": 200}).
		AddNode("entry", domain.NodeTypeLLM).
		AddNode("classifier", domain.NodeTypeLLM).
		AddEdge("entry", "loop").
		AddEdge("loop", "classifier").
		AddEdge("classifier", "classifier"). // self-loop on LLM sub-agent
		Entry("entry"))

	findings := rules.NewCycleDetector().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (Info for sub-agent under high-iter LoopAgent), got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Info {
		t.Errorf("Severity = %v, want Info (Control has max_iterations >= 100)", f.Severity)
	}
}

// Case 12: LoopAgent管理下サブエージェントのサイクル（max_iter < 100）→ 検出ゼロ（安全）
func TestCycleDetector_NoFinding_SubAgentCycleUnderSafeLoopAgent(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("loop", domain.NodeTypeLoop, map[string]any{"max_iterations": 5}).
		AddNode("entry", domain.NodeTypeLLM).
		AddNode("classifier", domain.NodeTypeLLM).
		AddEdge("entry", "loop").
		AddEdge("loop", "classifier").
		AddEdge("classifier", "classifier"). // self-loop on LLM sub-agent
		Entry("entry"))

	findings := rules.NewCycleDetector().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for safe loop (max_iter < 100), got %d: %+v", len(findings), findings)
	}
}

// TestCycleDetector_Confidence verifies that all CycleDetector findings have Confidence == 1.0.
// TestCycleDetector_DedupeBackEdges verifies that when several distinct
// back-edges close on the same cycle entry node, the detector emits
// **one** Finding rather than N copies. Real-world repro: LangGraph
// `add_conditional_edges("agent", route, ...)` where `route` returns
// any of {"tool", "agent", "reflect"} produces three back-edges to
// `agent`, all of which are part of the same logical cycle. Engineers
// reading SARIF reports treat duplicates as report churn.
//
// Dogfood: data-enrichment/src/enrichment_agent/graph.py
// (LangChain-AI examples) — emitted 3 identical cycle_detection
// warnings on `call_agent_model` before this dedupe.
func TestCycleDetector_DedupeBackEdges(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNode("agent", domain.NodeTypeLLM).
		AddNode("tool", domain.NodeTypeLLM).
		AddNode("reflect", domain.NodeTypeLLM).
		// All three branches route back to `agent`, closing distinct
		// back-edges through 3 different ancestor stacks.
		AddEdge("agent", "tool").
		AddEdge("agent", "reflect").
		AddEdge("tool", "agent").
		AddEdge("reflect", "agent").
		AddEdge("agent", "agent"). // direct self-loop branch
		Entry("agent"))

	findings := rules.NewCycleDetector().Analyze(g)
	cycleFindings := 0
	for _, f := range findings {
		if f.RuleName == "cycle_detection" && f.NodeID == "agent" {
			cycleFindings++
		}
	}
	if cycleFindings != 1 {
		t.Errorf("expected exactly 1 cycle_detection finding on `agent`, got %d (duplicates leaked through dedupe)", cycleFindings)
	}
}

func TestCycleDetector_Confidence(t *testing.T) {
	// Graph with an unguarded cycle (Loop node, no max_iterations) → Critical finding.
	g := mustBuild(t, testutil.NewBuilder().
		AddNode("loop", domain.NodeTypeLoop).
		AddNode("work", domain.NodeTypeLLM).
		AddEdge("loop", "work").
		AddEdge("work", "loop").
		Entry("loop"))

	findings := rules.NewCycleDetector().Analyze(g)
	if len(findings) == 0 {
		t.Fatal("expected ≥1 finding, got 0")
	}
	for _, f := range findings {
		if f.Confidence != 1.0 {
			t.Errorf("finding %q: Confidence = %.2f, want 1.0", f.RuleName, f.Confidence)
		}
	}
}
