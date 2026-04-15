package rules_test

import (
	"fmt"
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
)

// buildFanOutGraph creates a WorkflowGraph with a single source node that has `fanout`
// outgoing edges, each pointing to a distinct LLM child node.
func buildFanOutGraph(sourceID string, fanout int, config map[string]any) *domain.WorkflowGraph {
	nodes := make(map[string]*domain.Node, fanout+1)
	edges := make([]domain.Edge, 0, fanout)

	if config == nil {
		config = map[string]any{}
	}

	nodes[sourceID] = &domain.Node{
		ID:     sourceID,
		Name:   sourceID,
		Type:   domain.NodeTypeLLM,
		Config: config,
	}

	for i := 0; i < fanout; i++ {
		childID := fmt.Sprintf("child_%03d", i)
		nodes[childID] = &domain.Node{
			ID:   childID,
			Name: childID,
			Type: domain.NodeTypeLLM,
			Config: map[string]any{
				"model": "gpt-4o-mini",
			},
		}
		edges = append(edges, domain.Edge{From: sourceID, To: childID})
	}

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: sourceID,
	}
}

func TestMaxParallelBranches_FanOut5_NoFinding(t *testing.T) {
	graph := buildFanOutGraph("source", 5, nil)
	checker := rules.NewMaxParallelBranchesChecker()
	findings := checker.Analyze(graph)
	if len(findings) != 0 {
		t.Errorf("fan-out=5: expected 0 findings, got %d", len(findings))
	}
}

func TestMaxParallelBranches_FanOut10_Info(t *testing.T) {
	graph := buildFanOutGraph("source", 10, nil)
	checker := rules.NewMaxParallelBranchesChecker()
	findings := checker.Analyze(graph)
	if len(findings) != 1 {
		t.Fatalf("fan-out=10: expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Severity != domain.Info {
		t.Errorf("fan-out=10: expected Info severity, got %s", f.Severity)
	}
	if f.Confidence != 0.7 {
		t.Errorf("fan-out=10: expected Confidence=0.7, got %f", f.Confidence)
	}
	if f.RuleName != "max_parallel_branches" {
		t.Errorf("unexpected RuleName: %s", f.RuleName)
	}
	if f.NodeID != "source" {
		t.Errorf("expected NodeID=source, got %s", f.NodeID)
	}
}

func TestMaxParallelBranches_FanOut20_Warning(t *testing.T) {
	graph := buildFanOutGraph("source", 20, nil)
	checker := rules.NewMaxParallelBranchesChecker()
	findings := checker.Analyze(graph)
	if len(findings) != 1 {
		t.Fatalf("fan-out=20: expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Severity != domain.Warning {
		t.Errorf("fan-out=20: expected Warning severity, got %s", f.Severity)
	}
	if f.Confidence != 0.9 {
		t.Errorf("fan-out=20: expected Confidence=0.9, got %f", f.Confidence)
	}
}

func TestMaxParallelBranches_FanOut100_Critical(t *testing.T) {
	graph := buildFanOutGraph("source", 100, nil)
	checker := rules.NewMaxParallelBranchesChecker()
	findings := checker.Analyze(graph)
	if len(findings) != 1 {
		t.Fatalf("fan-out=100: expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Severity != domain.Critical {
		t.Errorf("fan-out=100: expected Critical severity, got %s", f.Severity)
	}
	if f.Confidence != 1.0 {
		t.Errorf("fan-out=100: expected Confidence=1.0, got %f", f.Confidence)
	}
}

func TestMaxParallelBranches_MaxConcurrencySet_NoFinding(t *testing.T) {
	// max_concurrency が設定されていれば検出しない
	config := map[string]any{
		"max_concurrency": 5,
	}
	graph := buildFanOutGraph("source", 100, config)
	checker := rules.NewMaxParallelBranchesChecker()
	findings := checker.Analyze(graph)
	if len(findings) != 0 {
		t.Errorf("max_concurrency set: expected 0 findings, got %d", len(findings))
	}
}

func TestMaxParallelBranches_MultipleNodesFanOut_MultipleFindings(t *testing.T) {
	// 2ノードが同時にfan-out超過 → 2件のFinding
	nodes := make(map[string]*domain.Node)
	var edges []domain.Edge

	// source_a: fan-out 20 → Warning
	nodes["source_a"] = &domain.Node{
		ID:     "source_a",
		Name:   "source_a",
		Type:   domain.NodeTypeLLM,
		Config: map[string]any{},
	}
	// source_b: fan-out 100 → Critical
	nodes["source_b"] = &domain.Node{
		ID:     "source_b",
		Name:   "source_b",
		Type:   domain.NodeTypeLLM,
		Config: map[string]any{},
	}

	for i := 0; i < 20; i++ {
		childID := fmt.Sprintf("child_a_%03d", i)
		nodes[childID] = &domain.Node{ID: childID, Name: childID, Type: domain.NodeTypeLLM, Config: map[string]any{}}
		edges = append(edges, domain.Edge{From: "source_a", To: childID})
	}
	for i := 0; i < 100; i++ {
		childID := fmt.Sprintf("child_b_%03d", i)
		nodes[childID] = &domain.Node{ID: childID, Name: childID, Type: domain.NodeTypeLLM, Config: map[string]any{}}
		edges = append(edges, domain.Edge{From: "source_b", To: childID})
	}

	graph := &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: "source_a",
	}

	checker := rules.NewMaxParallelBranchesChecker()
	findings := checker.Analyze(graph)

	if len(findings) != 2 {
		t.Fatalf("expected 2 findings (2 nodes with fan-out>=10), got %d", len(findings))
	}

	// 検出されたノードIDのセット確認
	foundIDs := make(map[string]domain.Severity)
	for _, f := range findings {
		foundIDs[f.NodeID] = f.Severity
	}
	if sev, ok := foundIDs["source_a"]; !ok || sev != domain.Warning {
		t.Errorf("expected source_a with Warning, got ok=%v sev=%v", ok, sev)
	}
	if sev, ok := foundIDs["source_b"]; !ok || sev != domain.Critical {
		t.Errorf("expected source_b with Critical, got ok=%v sev=%v", ok, sev)
	}
}
