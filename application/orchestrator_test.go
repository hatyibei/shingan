package application_test

import (
	"testing"

	"github.com/hatyibei/shingan/application"
	"github.com/hatyibei/shingan/domain"
)

// fakeRule is a test double for domain.AnalysisRule.
type fakeRule struct {
	name     string
	findings []domain.Finding
}

func (f *fakeRule) Name() string { return f.name }
func (f *fakeRule) Analyze(_ *domain.WorkflowGraph) []domain.Finding {
	return f.findings
}

// helper: build a minimal WorkflowGraph for tests that need a non-nil graph.
func minimalGraph() *domain.WorkflowGraph {
	return &domain.WorkflowGraph{
		Nodes: map[string]*domain.Node{
			"n1": {ID: "n1", Name: "start", Type: domain.NodeTypeLLM},
		},
		EntryNodeID: "n1",
	}
}

// TestAnalyze_AggregatesAllFindings verifies that findings from multiple rules
// are all present in the result.
func TestAnalyze_AggregatesAllFindings(t *testing.T) {
	o := application.NewAnalysisOrchestrator()

	rules := []domain.AnalysisRule{
		&fakeRule{name: "rule_a", findings: []domain.Finding{
			{RuleName: "rule_a", Severity: domain.Warning, Message: "warn"},
		}},
		&fakeRule{name: "rule_b", findings: []domain.Finding{
			{RuleName: "rule_b", Severity: domain.Critical, Message: "crit"},
			{RuleName: "rule_b", Severity: domain.Info, Message: "info"},
		}},
	}

	got := o.Analyze(minimalGraph(), rules)
	if len(got) != 3 {
		t.Fatalf("expected 3 findings, got %d", len(got))
	}
}

// TestAnalyze_SortsBySeverityDescending verifies Critical comes before Warning,
// which comes before Info.
func TestAnalyze_SortsBySeverityDescending(t *testing.T) {
	o := application.NewAnalysisOrchestrator()

	rules := []domain.AnalysisRule{
		&fakeRule{name: "rule_z", findings: []domain.Finding{
			{RuleName: "rule_z", Severity: domain.Info, Message: "info"},
			{RuleName: "rule_z", Severity: domain.Warning, Message: "warn"},
		}},
		&fakeRule{name: "rule_a", findings: []domain.Finding{
			{RuleName: "rule_a", Severity: domain.Critical, Message: "crit"},
		}},
	}

	got := o.Analyze(minimalGraph(), rules)
	if len(got) != 3 {
		t.Fatalf("expected 3 findings, got %d", len(got))
	}
	if got[0].Severity != domain.Critical {
		t.Errorf("got[0] severity = %v; want Critical", got[0].Severity)
	}
	if got[1].Severity != domain.Warning {
		t.Errorf("got[1] severity = %v; want Warning", got[1].Severity)
	}
	if got[2].Severity != domain.Info {
		t.Errorf("got[2] severity = %v; want Info", got[2].Severity)
	}
}

// TestAnalyze_SameSeveritySortsByRuleNameAscending verifies the secondary sort
// key (RuleName ascending) within the same Severity level.
func TestAnalyze_SameSeveritySortsByRuleNameAscending(t *testing.T) {
	o := application.NewAnalysisOrchestrator()

	rules := []domain.AnalysisRule{
		&fakeRule{name: "zzz_rule", findings: []domain.Finding{
			{RuleName: "zzz_rule", Severity: domain.Critical, Message: "z"},
		}},
		&fakeRule{name: "aaa_rule", findings: []domain.Finding{
			{RuleName: "aaa_rule", Severity: domain.Critical, Message: "a"},
		}},
	}

	got := o.Analyze(minimalGraph(), rules)
	if len(got) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(got))
	}
	if got[0].RuleName != "aaa_rule" {
		t.Errorf("got[0].RuleName = %q; want aaa_rule", got[0].RuleName)
	}
	if got[1].RuleName != "zzz_rule" {
		t.Errorf("got[1].RuleName = %q; want zzz_rule", got[1].RuleName)
	}
}

// TestAnalyze_EmptyRules verifies that an empty rule slice returns an empty
// (non-nil) slice without panicking.
func TestAnalyze_EmptyRules(t *testing.T) {
	o := application.NewAnalysisOrchestrator()
	got := o.Analyze(minimalGraph(), []domain.AnalysisRule{})
	if got == nil {
		t.Fatal("expected non-nil slice for empty rules")
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(got))
	}
}

// TestAnalyze_RuleReturnsNoFindings verifies that rules returning no findings
// contribute nothing to the aggregate result.
func TestAnalyze_RuleReturnsNoFindings(t *testing.T) {
	o := application.NewAnalysisOrchestrator()

	rules := []domain.AnalysisRule{
		&fakeRule{name: "silent_rule", findings: nil},
		&fakeRule{name: "empty_rule", findings: []domain.Finding{}},
		&fakeRule{name: "noisy_rule", findings: []domain.Finding{
			{RuleName: "noisy_rule", Severity: domain.Warning, Message: "found something"},
		}},
	}

	got := o.Analyze(minimalGraph(), rules)
	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}
	if got[0].RuleName != "noisy_rule" {
		t.Errorf("unexpected finding from rule %q", got[0].RuleName)
	}
}

// TestAnalyze_NilGraph verifies that a nil graph is forwarded to rules without
// panicking (our fakeRule ignores the graph argument).
func TestAnalyze_NilGraph(t *testing.T) {
	o := application.NewAnalysisOrchestrator()

	rules := []domain.AnalysisRule{
		&fakeRule{name: "nil_safe", findings: []domain.Finding{
			{RuleName: "nil_safe", Severity: domain.Info, Message: "ok"},
		}},
	}

	got := o.Analyze(nil, rules)
	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}
}
