package rules_test

import (
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/domain/testutil"
)

func TestLoopGuardChecker_Name(t *testing.T) {
	lc := rules.NewLoopGuardChecker()
	if got := lc.Name(); got != "loop_guard" {
		t.Errorf("Name() = %q, want %q", got, "loop_guard")
	}
}

// Case 1: Loop node with no max_iterations → Critical
func TestLoopGuardChecker_Critical_MissingMaxIterations(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNode("retry_loop", domain.NodeTypeLoop).
		AddNode("work", domain.NodeTypeLLM).
		AddEdge("retry_loop", "work").
		Entry("retry_loop"))

	findings := rules.NewLoopGuardChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", f.Severity)
	}
	if f.RuleName != "loop_guard" {
		t.Errorf("RuleName = %q, want %q", f.RuleName, "loop_guard")
	}
	if f.NodeID != "retry_loop" {
		t.Errorf("NodeID = %q, want %q", f.NodeID, "retry_loop")
	}
}

// Case 2: Loop node with max_iterations = 5 → no finding
func TestLoopGuardChecker_NoFinding_MaxIterations5(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddLoopNode("retry_loop", 5).
		AddNode("work", domain.NodeTypeLLM).
		AddEdge("retry_loop", "work").
		Entry("retry_loop"))

	findings := rules.NewLoopGuardChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for max_iterations=5, got %d: %+v", len(findings), findings)
	}
}

// Case 3: Loop node with max_iterations = 100 → no finding (LoopGuardChecker only checks presence)
func TestLoopGuardChecker_NoFinding_MaxIterations100(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddLoopNode("big_loop", 100).
		AddNode("work", domain.NodeTypeLLM).
		AddEdge("big_loop", "work").
		Entry("big_loop"))

	findings := rules.NewLoopGuardChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for max_iterations=100 (LoopGuardChecker only checks presence), got %d: %+v", len(findings), findings)
	}
}

// Case 4: Non-Loop node → no finding
func TestLoopGuardChecker_NoFinding_NonLoopNode(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNode("llm_agent", domain.NodeTypeLLM).
		AddNode("tool_node", domain.NodeTypeTool).
		AddEdge("llm_agent", "tool_node").
		Entry("llm_agent"))

	findings := rules.NewLoopGuardChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for non-Loop nodes, got %d: %+v", len(findings), findings)
	}
}

// Case 4b: Condition node (NodeTypeCondition) → no finding (max_iterations not required)
func TestLoopGuardChecker_NoFinding_ConditionNode(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddConditionNode("if_error", "err != nil").
		AddNode("success", domain.NodeTypeOutput).
		AddNode("failure", domain.NodeTypeOutput).
		AddConditionalEdge("if_error", "success", "err == nil").
		AddConditionalEdge("if_error", "failure", "err != nil").
		Entry("if_error"))

	findings := rules.NewLoopGuardChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for Condition node, got %d: %+v", len(findings), findings)
	}
}

// Case 5: max_iterations is non-numeric string → Critical (treated as missing)
func TestLoopGuardChecker_Critical_NonNumericMaxIterations(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("loop", domain.NodeTypeLoop, map[string]any{"max_iterations": "unlimited"}).
		AddNode("work", domain.NodeTypeLLM).
		AddEdge("loop", "work").
		Entry("loop"))

	findings := rules.NewLoopGuardChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for non-numeric max_iterations, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", findings[0].Severity)
	}
}

// Case 6: nil graph → no panic, 0 findings
func TestLoopGuardChecker_NilGraph(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Analyze(nil) panicked: %v", r)
		}
	}()
	findings := rules.NewLoopGuardChecker().Analyze(nil)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for nil graph, got %d", len(findings))
	}
}

// Case 7: Multiple Loop nodes, only one missing max_iterations → 1 finding
func TestLoopGuardChecker_MultipleLoops_OneMissing(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddLoopNode("safe_loop", 10).
		AddNode("unsafe_loop", domain.NodeTypeLoop).
		AddNode("work_a", domain.NodeTypeLLM).
		AddNode("work_b", domain.NodeTypeLLM).
		AddEdge("safe_loop", "work_a").
		AddEdge("unsafe_loop", "work_b").
		Entry("safe_loop"))

	findings := rules.NewLoopGuardChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (only unsafe_loop), got %d: %+v", len(findings), findings)
	}
	if findings[0].NodeID != "unsafe_loop" {
		t.Errorf("NodeID = %q, want %q", findings[0].NodeID, "unsafe_loop")
	}
}

// Case 8: Deprecated NodeTypeControl (backward compat) without max_iterations → Critical
func TestLoopGuardChecker_BackwardCompat_ControlIsLoop(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNode("old_ctrl", domain.NodeTypeControl).
		AddNode("work", domain.NodeTypeLLM).
		AddEdge("old_ctrl", "work").
		Entry("old_ctrl"))

	findings := rules.NewLoopGuardChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for deprecated NodeTypeControl without max_iterations, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", findings[0].Severity)
	}
}
