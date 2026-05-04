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

// TestAnalyze_SameSeveritySortsByConfidenceDescending verifies that within the
// same Severity, findings with higher Confidence appear first.
func TestAnalyze_SameSeveritySortsByConfidenceDescending(t *testing.T) {
	o := application.NewAnalysisOrchestrator()

	rules := []domain.AnalysisRule{
		&fakeRule{name: "rule_a", findings: []domain.Finding{
			{RuleName: "rule_a", Severity: domain.Warning, Confidence: 0.5, Message: "low confidence"},
		}},
		&fakeRule{name: "rule_b", findings: []domain.Finding{
			{RuleName: "rule_b", Severity: domain.Warning, Confidence: 0.9, Message: "high confidence"},
		}},
	}

	got := o.Analyze(minimalGraph(), rules)
	if len(got) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(got))
	}
	// Higher confidence should appear first.
	if got[0].Confidence < got[1].Confidence {
		t.Errorf("expected Confidence DESC: got[0]=%.2f should be >= got[1]=%.2f",
			got[0].Confidence, got[1].Confidence)
	}
	if got[0].RuleName != "rule_b" {
		t.Errorf("expected rule_b (high confidence) first, got %q", got[0].RuleName)
	}
}

// TestAnalyze_ZeroConfidenceNormalizedToOne verifies that findings with
// Confidence 0.0 (unset) are normalized to 1.0 by the orchestrator.
func TestAnalyze_ZeroConfidenceNormalizedToOne(t *testing.T) {
	o := application.NewAnalysisOrchestrator()

	rules := []domain.AnalysisRule{
		&fakeRule{name: "old_rule", findings: []domain.Finding{
			// Confidence not set — defaults to 0.0 in Go.
			{RuleName: "old_rule", Severity: domain.Info, Message: "unset confidence"},
		}},
	}

	got := o.Analyze(minimalGraph(), rules)
	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}
	if got[0].Confidence != 1.0 {
		t.Errorf("expected Confidence normalized to 1.0, got %.2f", got[0].Confidence)
	}
}

// TestAnalyzeMulti_StampsSourceFile verifies the ADR-012 contract: each
// finding produced by AnalyzeMulti carries the SourceFile of the graph
// that produced it, so directory-mode analyses can attribute findings
// back to the originating file.
func TestAnalyzeMulti_StampsSourceFile(t *testing.T) {
	rules := []domain.AnalysisRule{
		&fakeRule{name: "rule_a", findings: []domain.Finding{
			{RuleName: "rule_a", NodeID: "n1", Message: "issue", Severity: domain.Warning},
		}},
	}
	inputs := []application.GraphWithSource{
		{Graph: &domain.WorkflowGraph{Nodes: map[string]*domain.Node{"n1": {ID: "n1"}}}, SourceFile: "fileA.go"},
		{Graph: &domain.WorkflowGraph{Nodes: map[string]*domain.Node{"n1": {ID: "n1"}}}, SourceFile: "fileB.go"},
	}

	o := application.NewAnalysisOrchestrator()
	got := o.AnalyzeMulti(inputs, rules)

	if len(got) != 2 {
		t.Fatalf("expected 2 findings (one per graph), got %d", len(got))
	}
	sources := map[string]int{}
	for _, f := range got {
		sources[f.SourceFile]++
	}
	if sources["fileA.go"] != 1 || sources["fileB.go"] != 1 {
		t.Errorf("expected SourceFile distribution 1+1, got %v", sources)
	}
}

// TestAnalyzeMulti_EmptyInputs returns an empty slice without panicking.
func TestAnalyzeMulti_EmptyInputs(t *testing.T) {
	o := application.NewAnalysisOrchestrator()
	got := o.AnalyzeMulti(nil, []domain.AnalysisRule{&fakeRule{name: "x"}})
	if len(got) != 0 {
		t.Errorf("expected empty result for nil inputs, got %d", len(got))
	}
}

// TestAnalyzeMulti_NilGraphSkipped silently skips nil-graph entries
// rather than panicking; this matches the CLI's "warning + continue"
// behaviour for unparseable files.
func TestAnalyzeMulti_NilGraphSkipped(t *testing.T) {
	rules := []domain.AnalysisRule{
		&fakeRule{name: "rule_a", findings: []domain.Finding{
			{RuleName: "rule_a", NodeID: "n1", Message: "issue", Severity: domain.Info},
		}},
	}
	inputs := []application.GraphWithSource{
		{Graph: nil, SourceFile: "broken.go"},
		{Graph: &domain.WorkflowGraph{Nodes: map[string]*domain.Node{"n1": {ID: "n1"}}}, SourceFile: "good.go"},
	}
	o := application.NewAnalysisOrchestrator()
	got := o.AnalyzeMulti(inputs, rules)
	if len(got) != 1 || got[0].SourceFile != "good.go" {
		t.Errorf("expected only good.go's finding, got %+v", got)
	}
}
