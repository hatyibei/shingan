package examplerule

import (
	"testing"

	"github.com/hatyibei/shingan/domain"
)

// TestRule_FlagsBannedPrefix runs the rule against a synthetic graph
// with one banned node and one clean node, asserting exactly one
// finding emerges with the expected NodeID, RuleName, and Severity.
func TestRule_FlagsBannedPrefix(t *testing.T) {
	g := &domain.WorkflowGraph{
		Nodes: map[string]*domain.Node{
			"TODO_finalise_plan": {ID: "TODO_finalise_plan", Type: domain.NodeTypeLLM},
			"shipped_node":       {ID: "shipped_node", Type: domain.NodeTypeLLM},
		},
	}
	findings := Rule{}.Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.NodeID != "TODO_finalise_plan" {
		t.Errorf("NodeID: got %q, want %q", f.NodeID, "TODO_finalise_plan")
	}
	want := (Rule{}).Name()
	if f.RuleName != want {
		t.Errorf("RuleName: got %q, want %q", f.RuleName, want)
	}
	if f.Severity != domain.Warning {
		t.Errorf("Severity: got %v, want Warning", f.Severity)
	}
}

// TestRule_NilGraphSafe asserts the rule doesn't panic on a nil
// WorkflowGraph — the orchestrator never passes nil today but defensive
// behaviour is contractually expected for AnalysisRule.
func TestRule_NilGraphSafe(t *testing.T) {
	findings := Rule{}.Analyze(nil)
	if findings != nil && len(findings) > 0 {
		t.Errorf("expected nil/empty findings for nil graph, got %+v", findings)
	}
}

// TestRule_NameUsesExperimentalPrefix locks in the v0.x convention
// that plugin rule names begin with "experimental:". If this test
// fires the plugin will refuse to register at init() time, breaking
// any binary that imports the example.
func TestRule_NameUsesExperimentalPrefix(t *testing.T) {
	name := Rule{}.Name()
	if len(name) < 13 || name[:13] != "experimental:" {
		t.Errorf("rule name must start with %q (v0.x convention); got %q", "experimental:", name)
	}
}
