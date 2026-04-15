package rules_test

import (
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/domain/testutil"
)

func TestReachabilityChecker_Name(t *testing.T) {
	checker := rules.NewReachabilityChecker()
	if checker.Name() != "unreachable_node" {
		t.Errorf("Name() = %q, want %q", checker.Name(), "unreachable_node")
	}
}

// Case 1: 全ノード到達可能 — findingなし
func TestReachabilityChecker_AllReachable(t *testing.T) {
	graph, err := testutil.NewBuilder().
		AddNode("entry", domain.NodeTypeLLM).
		AddNode("tool", domain.NodeTypeTool).
		AddNode("out", domain.NodeTypeOutput).
		AddEdge("entry", "tool").
		AddEdge("tool", "out").
		Entry("entry").
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	checker := rules.NewReachabilityChecker()
	findings := checker.Analyze(graph)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// Case 2: LLMノードが孤立 → Warning
func TestReachabilityChecker_IsolatedLLMNode(t *testing.T) {
	graph, err := testutil.NewBuilder().
		AddNode("entry", domain.NodeTypeCondition).
		AddNode("orphan_llm", domain.NodeTypeLLM).
		AddNode("out", domain.NodeTypeOutput).
		AddEdge("entry", "out").
		Entry("entry").
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	checker := rules.NewReachabilityChecker()
	findings := checker.Analyze(graph)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.NodeID != "orphan_llm" {
		t.Errorf("NodeID = %q, want %q", f.NodeID, "orphan_llm")
	}
	if f.Severity != domain.Warning {
		t.Errorf("Severity = %v, want Warning", f.Severity)
	}
	if f.RuleName != "unreachable_node" {
		t.Errorf("RuleName = %q, want %q", f.RuleName, "unreachable_node")
	}
}

// Case 3: Toolノードが孤立 → Warning
func TestReachabilityChecker_IsolatedToolNode(t *testing.T) {
	graph, err := testutil.NewBuilder().
		AddNode("entry", domain.NodeTypeLLM).
		AddNode("orphan_tool", domain.NodeTypeTool).
		AddEdge("entry", "entry"). // self-loop to keep entry connected
		Entry("entry").
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	checker := rules.NewReachabilityChecker()
	findings := checker.Analyze(graph)

	var toolFindings []domain.Finding
	for _, f := range findings {
		if f.NodeID == "orphan_tool" {
			toolFindings = append(toolFindings, f)
		}
	}
	if len(toolFindings) != 1 {
		t.Fatalf("expected 1 finding for orphan_tool, got %d (all findings: %+v)", len(toolFindings), findings)
	}
	if toolFindings[0].Severity != domain.Warning {
		t.Errorf("Severity = %v, want Warning", toolFindings[0].Severity)
	}
}

// Case 4: EntryNodeIDが未設定 → Critical 1件
func TestReachabilityChecker_EntryNotSet(t *testing.T) {
	// WorkflowGraphを直接構築してEntryNodeIDを空にする
	graph := &domain.WorkflowGraph{
		Nodes: map[string]*domain.Node{
			"a": {ID: "a", Name: "a", Type: domain.NodeTypeLLM},
		},
		Edges:       nil,
		EntryNodeID: "",
	}

	checker := rules.NewReachabilityChecker()
	findings := checker.Analyze(graph)

	if len(findings) != 1 {
		t.Fatalf("expected 1 Critical finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", findings[0].Severity)
	}
}

// Case 5: EntryNodeIDがグラフに存在しない → Critical 1件
func TestReachabilityChecker_EntryNotInGraph(t *testing.T) {
	graph := &domain.WorkflowGraph{
		Nodes: map[string]*domain.Node{
			"a": {ID: "a", Name: "a", Type: domain.NodeTypeLLM},
		},
		Edges:       nil,
		EntryNodeID: "nonexistent",
	}

	checker := rules.NewReachabilityChecker()
	findings := checker.Analyze(graph)

	if len(findings) != 1 {
		t.Fatalf("expected 1 Critical finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", findings[0].Severity)
	}
	if findings[0].NodeID != "nonexistent" {
		t.Errorf("NodeID = %q, want %q", findings[0].NodeID, "nonexistent")
	}
}

// Case 6: 分岐グラフで両方のパスが到達可能 — findingなし
func TestReachabilityChecker_BranchBothReachable(t *testing.T) {
	// entry → branch → pathA
	//               └→ pathB → out
	graph, err := testutil.NewBuilder().
		AddNode("entry", domain.NodeTypeLLM).
		AddNode("branch", domain.NodeTypeCondition).
		AddNode("pathA", domain.NodeTypeTool).
		AddNode("pathB", domain.NodeTypeTool).
		AddNode("out", domain.NodeTypeOutput).
		AddEdge("entry", "branch").
		AddConditionalEdge("branch", "pathA", "condition==true").
		AddConditionalEdge("branch", "pathB", "condition==false").
		AddEdge("pathB", "out").
		Entry("entry").
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	checker := rules.NewReachabilityChecker()
	findings := checker.Analyze(graph)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// Case 7: Loop/Condition/Human/Outputノードが孤立 → Info（Warningではない）
func TestReachabilityChecker_IsolatedControlHumanOutputNodes(t *testing.T) {
	graph, err := testutil.NewBuilder().
		AddNode("entry", domain.NodeTypeLLM).
		AddNode("orphan_ctrl", domain.NodeTypeLoop).
		AddNode("orphan_human", domain.NodeTypeHuman).
		AddNode("orphan_out", domain.NodeTypeOutput).
		Entry("entry").
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	checker := rules.NewReachabilityChecker()
	findings := checker.Analyze(graph)

	if len(findings) != 3 {
		t.Fatalf("expected 3 findings, got %d: %+v", len(findings), findings)
	}
	for _, f := range findings {
		if f.Severity != domain.Info {
			t.Errorf("node %q: Severity = %v, want Info", f.NodeID, f.Severity)
		}
	}
}

// Case 8: nilグラフ → findingなし（パニックしない）
func TestReachabilityChecker_NilGraph(t *testing.T) {
	checker := rules.NewReachabilityChecker()
	findings := checker.Analyze(nil)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for nil graph, got %d", len(findings))
	}
}

// TestReachabilityChecker_Confidence verifies all findings have Confidence == 1.0.
func TestReachabilityChecker_Confidence(t *testing.T) {
	// Graph with an unreachable node.
	g := mustBuild(t, testutil.NewBuilder().
		AddNode("start", domain.NodeTypeLLM).
		AddNode("orphan", domain.NodeTypeLLM).
		Entry("start"))

	findings := rules.NewReachabilityChecker().Analyze(g)
	if len(findings) == 0 {
		t.Fatal("expected ≥1 finding, got 0")
	}
	for _, f := range findings {
		if f.Confidence != 1.0 {
			t.Errorf("Confidence = %.2f, want 1.0", f.Confidence)
		}
	}
}
