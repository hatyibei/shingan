package reporter_test

import (
	"encoding/json"
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/infrastructure/reporter"
)

func TestJSONReporter_ContentType(t *testing.T) {
	r := reporter.NewJSONReporter()
	if got := r.ContentType(); got != "application/json" {
		t.Errorf("ContentType() = %q, want %q", got, "application/json")
	}
}

// TestJSONReporter_Empty verifies that an empty findings slice produces a valid
// JSON document with zero counts and an empty findings array.
func TestJSONReporter_Empty(t *testing.T) {
	r := reporter.NewJSONReporter()
	out, err := r.Format(nil)
	if err != nil {
		t.Fatalf("Format(nil) unexpected error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}

	summary, ok := result["summary"].(map[string]interface{})
	if !ok {
		t.Fatal("missing 'summary' key")
	}
	if total := summary["total"].(float64); total != 0 {
		t.Errorf("summary.total = %v, want 0", total)
	}

	findings, ok := result["findings"].([]interface{})
	if !ok {
		t.Fatal("missing 'findings' key")
	}
	if len(findings) != 0 {
		t.Errorf("findings length = %d, want 0", len(findings))
	}
}

// TestJSONReporter_Single verifies a single finding is serialized correctly,
// with severity as a string.
func TestJSONReporter_Single(t *testing.T) {
	r := reporter.NewJSONReporter()
	findings := []domain.Finding{
		{
			RuleName:   "cycle_detection",
			Severity:   domain.Critical,
			NodeID:     "node-1",
			Message:    "Cycle detected",
			Suggestion: "Remove the back-edge from node-1 to node-2",
		},
	}

	out, err := r.Format(findings)
	if err != nil {
		t.Fatalf("Format() unexpected error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}

	fs := result["findings"].([]interface{})
	if len(fs) != 1 {
		t.Fatalf("findings length = %d, want 1", len(fs))
	}

	f := fs[0].(map[string]interface{})
	if f["severity"] != "critical" {
		t.Errorf("severity = %q, want \"critical\"", f["severity"])
	}
	if f["rule"] != "cycle_detection" {
		t.Errorf("rule = %q, want \"cycle_detection\"", f["rule"])
	}
	if f["node_id"] != "node-1" {
		t.Errorf("node_id = %q, want \"node-1\"", f["node_id"])
	}

	summary := result["summary"].(map[string]interface{})
	if summary["total"].(float64) != 1 {
		t.Errorf("summary.total = %v, want 1", summary["total"])
	}
	if summary["critical"].(float64) != 1 {
		t.Errorf("summary.critical = %v, want 1", summary["critical"])
	}
}

// TestJSONReporter_MixedSeverity verifies correct summary counts when findings
// span all three severity levels.
func TestJSONReporter_MixedSeverity(t *testing.T) {
	r := reporter.NewJSONReporter()
	findings := []domain.Finding{
		{RuleName: "rule-a", Severity: domain.Critical, Message: "critical issue"},
		{RuleName: "rule-b", Severity: domain.Warning, Message: "warning issue"},
		{RuleName: "rule-c", Severity: domain.Warning, Message: "another warning"},
		{RuleName: "rule-d", Severity: domain.Info, Message: "info notice"},
	}

	out, err := r.Format(findings)
	if err != nil {
		t.Fatalf("Format() unexpected error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}

	summary := result["summary"].(map[string]interface{})
	cases := []struct {
		key  string
		want float64
	}{
		{"total", 4},
		{"critical", 1},
		{"warning", 2},
		{"info", 1},
	}
	for _, c := range cases {
		if got := summary[c.key].(float64); got != c.want {
			t.Errorf("summary.%s = %v, want %v", c.key, got, c.want)
		}
	}
}

// TestJSONReporter_SeverityStrings verifies all severity values are serialized
// as their string labels, not as integers.
func TestJSONReporter_SeverityStrings(t *testing.T) {
	r := reporter.NewJSONReporter()
	findings := []domain.Finding{
		{RuleName: "r1", Severity: domain.Info},
		{RuleName: "r2", Severity: domain.Warning},
		{RuleName: "r3", Severity: domain.Critical},
	}

	out, err := r.Format(findings)
	if err != nil {
		t.Fatalf("Format() unexpected error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	fs := result["findings"].([]interface{})
	want := []string{"info", "warning", "critical"}
	for i, f := range fs {
		got := f.(map[string]interface{})["severity"].(string)
		if got != want[i] {
			t.Errorf("findings[%d].severity = %q, want %q", i, got, want[i])
		}
	}
}
