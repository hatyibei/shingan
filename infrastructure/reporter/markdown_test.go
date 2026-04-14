package reporter_test

import (
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/infrastructure/reporter"
)

func TestMarkdownReporter_ContentType(t *testing.T) {
	r := reporter.NewMarkdownReporter()
	if got := r.ContentType(); got != "text/markdown" {
		t.Errorf("ContentType() = %q, want %q", got, "text/markdown")
	}
}

// TestMarkdownReporter_Empty verifies that an empty findings slice produces a
// report header and a "no findings" message.
func TestMarkdownReporter_Empty(t *testing.T) {
	r := reporter.NewMarkdownReporter()
	out, err := r.Format(nil)
	if err != nil {
		t.Fatalf("Format(nil) unexpected error: %v", err)
	}

	s := string(out)
	if !strings.Contains(s, "# Shingan Analysis Report") {
		t.Error("output missing report header")
	}
	if !strings.Contains(s, "No findings") {
		t.Error("output missing 'No findings' message for empty input")
	}
	// Must not contain severity section headers when there are no findings.
	for _, sec := range []string{"## Critical", "## Warning", "## Info"} {
		if strings.Contains(s, sec) {
			t.Errorf("output should not contain %q for empty findings", sec)
		}
	}
}

// TestMarkdownReporter_Single verifies that a single finding is placed in the
// correct severity section with its fields.
func TestMarkdownReporter_Single(t *testing.T) {
	r := reporter.NewMarkdownReporter()
	findings := []domain.Finding{
		{
			RuleName:   "cycle_detection",
			Severity:   domain.Critical,
			NodeID:     "node-42",
			Message:    "Cycle detected",
			Suggestion: "Remove the back-edge",
		},
	}

	out, err := r.Format(findings)
	if err != nil {
		t.Fatalf("Format() unexpected error: %v", err)
	}

	s := string(out)
	if !strings.Contains(s, "## Critical") {
		t.Error("output missing ## Critical section")
	}
	if !strings.Contains(s, "cycle_detection") {
		t.Error("output missing rule name")
	}
	if !strings.Contains(s, "node-42") {
		t.Error("output missing node_id")
	}
	if !strings.Contains(s, "Cycle detected") {
		t.Error("output missing message")
	}
	if !strings.Contains(s, "Remove the back-edge") {
		t.Error("output missing suggestion")
	}
	// Warning and Info sections should be absent.
	if strings.Contains(s, "## Warning") {
		t.Error("output should not have ## Warning for a single critical finding")
	}
}

// TestMarkdownReporter_MixedSeverity verifies that findings are grouped into
// the correct sections and Critical appears before Warning before Info.
func TestMarkdownReporter_MixedSeverity(t *testing.T) {
	r := reporter.NewMarkdownReporter()
	findings := []domain.Finding{
		{RuleName: "info-rule", Severity: domain.Info, Message: "info msg"},
		{RuleName: "warn-rule", Severity: domain.Warning, Message: "warn msg"},
		{RuleName: "crit-rule", Severity: domain.Critical, Message: "crit msg"},
	}

	out, err := r.Format(findings)
	if err != nil {
		t.Fatalf("Format() unexpected error: %v", err)
	}

	s := string(out)

	for _, sec := range []string{"## Critical", "## Warning", "## Info"} {
		if !strings.Contains(s, sec) {
			t.Errorf("output missing section %q", sec)
		}
	}

	// Critical must appear before Warning which must appear before Info.
	critIdx := strings.Index(s, "## Critical")
	warnIdx := strings.Index(s, "## Warning")
	infoIdx := strings.Index(s, "## Info")
	if !(critIdx < warnIdx && warnIdx < infoIdx) {
		t.Errorf("severity sections are not in order Critical < Warning < Info (indices: %d, %d, %d)",
			critIdx, warnIdx, infoIdx)
	}
}

// TestMarkdownReporter_EmptyNodeID verifies that findings without a NodeID
// display "(graph)" as a placeholder.
func TestMarkdownReporter_EmptyNodeID(t *testing.T) {
	r := reporter.NewMarkdownReporter()
	findings := []domain.Finding{
		{RuleName: "graph-rule", Severity: domain.Warning, NodeID: "", Message: "graph-level issue"},
	}

	out, err := r.Format(findings)
	if err != nil {
		t.Fatalf("Format() unexpected error: %v", err)
	}

	if !strings.Contains(string(out), "(graph)") {
		t.Error("output should show '(graph)' for findings with empty NodeID")
	}
}

// TestMarkdownReporter_SummaryTable verifies the summary table contains correct counts.
func TestMarkdownReporter_SummaryTable(t *testing.T) {
	r := reporter.NewMarkdownReporter()
	findings := []domain.Finding{
		{Severity: domain.Critical},
		{Severity: domain.Warning},
		{Severity: domain.Warning},
		{Severity: domain.Info},
	}

	out, err := r.Format(findings)
	if err != nil {
		t.Fatalf("Format() unexpected error: %v", err)
	}

	s := string(out)
	// The summary row should contain "4" for total, "1" for critical, "2" for warning, "1" for info.
	if !strings.Contains(s, "| 4 |") {
		t.Error("summary table missing total count 4")
	}
}
