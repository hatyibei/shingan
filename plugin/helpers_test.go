package plugin_test

import (
	"sort"
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/plugin"
)

// TestNewFinding_SaneDefaults pins the contract that NewFinding
// supplies high-confidence defaults appropriate for "exact static
// match" detections (the 80% case for plugin rules). Plugin authors
// override these on the returned struct only when needed.
func TestNewFinding_SaneDefaults(t *testing.T) {
	f := plugin.NewFinding("experimental:r", "node-1", domain.Warning, "hello")
	if f.RuleName != "experimental:r" {
		t.Errorf("RuleName: got %q", f.RuleName)
	}
	if f.NodeID != "node-1" {
		t.Errorf("NodeID: got %q", f.NodeID)
	}
	if f.Severity != domain.Warning {
		t.Errorf("Severity: got %v", f.Severity)
	}
	if f.Confidence != 1.0 {
		t.Errorf("Confidence: got %v, want 1.0", f.Confidence)
	}
	if f.ConfidenceReason != domain.ReasonExactStaticMatch {
		t.Errorf("ConfidenceReason: got %v", f.ConfidenceReason)
	}
	if f.Message != "hello" {
		t.Errorf("Message: got %q", f.Message)
	}
}

// TestNewFinding_OverrideAfterConstruct asserts the struct is plain
// data and callers can lower the confidence for heuristic detections.
// This codifies the documented usage pattern.
func TestNewFinding_OverrideAfterConstruct(t *testing.T) {
	f := plugin.NewFinding("experimental:r", "x", domain.Info, "m")
	f.Confidence = 0.3
	f.ConfidenceReason = domain.ReasonHeuristicPattern
	if f.Confidence != 0.3 {
		t.Errorf("override Confidence failed")
	}
	if f.ConfidenceReason != domain.ReasonHeuristicPattern {
		t.Errorf("override ConfidenceReason failed")
	}
}

// TestNodesOfType_FiltersByType is the happy path: only matching
// nodes come back.
func TestNodesOfType_FiltersByType(t *testing.T) {
	g := &domain.WorkflowGraph{Nodes: map[string]*domain.Node{
		"a": {ID: "a", Type: domain.NodeTypeLLM},
		"b": {ID: "b", Type: domain.NodeTypeTool},
		"c": {ID: "c", Type: domain.NodeTypeLLM},
	}}
	got := plugin.NodesOfType(g, domain.NodeTypeLLM)
	if len(got) != 2 {
		t.Fatalf("want 2 LLM nodes, got %d", len(got))
	}
	ids := []string{got[0].ID, got[1].ID}
	sort.Strings(ids)
	if ids[0] != "a" || ids[1] != "c" {
		t.Errorf("unexpected ids: %v", ids)
	}
}

// TestNodesOfType_NilGraphReturnsEmpty avoids nil-deref crashes in
// plugin code that doesn't pre-check the graph (the orchestrator
// won't pass nil today, but plugin authors writing isolated test
// harnesses might).
func TestNodesOfType_NilGraphReturnsEmpty(t *testing.T) {
	got := plugin.NodesOfType(nil, domain.NodeTypeLLM)
	if got == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(got))
	}
}

// TestNodesOfType_SkipsNilNode: defensive — a graph carrying a nil
// pointer in its Nodes map (corrupt fixture, partial fallback)
// shouldn't crash the rule.
func TestNodesOfType_SkipsNilNode(t *testing.T) {
	g := &domain.WorkflowGraph{Nodes: map[string]*domain.Node{
		"a":   nil,
		"b":   {ID: "b", Type: domain.NodeTypeLLM},
		"nil": nil,
	}}
	got := plugin.NodesOfType(g, domain.NodeTypeLLM)
	if len(got) != 1 {
		t.Fatalf("want 1 node (only the non-nil match), got %d", len(got))
	}
	if got[0].ID != "b" {
		t.Errorf("got id %q, want b", got[0].ID)
	}
}
