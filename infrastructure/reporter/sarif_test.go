package reporter_test

import (
	"encoding/json"
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/infrastructure/reporter"
)

// parseSARIF is a helper that unmarshals raw SARIF bytes into a generic map.
func parseSARIF(t *testing.T, data []byte) map[string]interface{} {
	t.Helper()
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("SARIF output is not valid JSON: %v\n%s", err, data)
	}
	return doc
}

// sarifRuns extracts runs[0] from a parsed SARIF document.
func sarifRun0(t *testing.T, doc map[string]interface{}) map[string]interface{} {
	t.Helper()
	runs, ok := doc["runs"].([]interface{})
	if !ok || len(runs) == 0 {
		t.Fatal("SARIF: missing or empty 'runs' array")
	}
	run, ok := runs[0].(map[string]interface{})
	if !ok {
		t.Fatal("SARIF: runs[0] is not an object")
	}
	return run
}

// TestSARIFReporter_ContentType verifies the MIME type.
func TestSARIFReporter_ContentType(t *testing.T) {
	r := reporter.NewSARIFReporter()
	if got := r.ContentType(); got != "application/sarif+json" {
		t.Errorf("ContentType() = %q, want %q", got, "application/sarif+json")
	}
}

// TestSARIFReporter_Empty verifies empty findings produce valid SARIF with empty
// results and rules arrays (not null).
func TestSARIFReporter_Empty(t *testing.T) {
	r := reporter.NewSARIFReporter()
	out, err := r.Format(nil)
	if err != nil {
		t.Fatalf("Format(nil) unexpected error: %v", err)
	}

	doc := parseSARIF(t, out)

	// Top-level schema fields.
	if doc["$schema"] != "https://json.schemastore.org/sarif-2.1.0.json" {
		t.Errorf("$schema = %q", doc["$schema"])
	}
	if doc["version"] != "2.1.0" {
		t.Errorf("version = %q, want \"2.1.0\"", doc["version"])
	}

	run := sarifRun0(t, doc)

	// results must be an empty array, not null.
	results, ok := run["results"].([]interface{})
	if !ok {
		t.Fatalf("runs[0].results is not an array (got %T)", run["results"])
	}
	if len(results) != 0 {
		t.Errorf("results length = %d, want 0", len(results))
	}

	// rules must be an empty array, not null.
	driver := run["tool"].(map[string]interface{})["driver"].(map[string]interface{})
	rules, ok := driver["rules"].([]interface{})
	if !ok {
		t.Fatalf("driver.rules is not an array (got %T)", driver["rules"])
	}
	if len(rules) != 0 {
		t.Errorf("rules length = %d, want 0", len(rules))
	}
}

// TestSARIFReporter_Critical verifies a Critical finding maps to level "error".
func TestSARIFReporter_Critical(t *testing.T) {
	r := reporter.NewSARIFReporter()
	findings := []domain.Finding{
		{
			RuleName: "cycle_detection",
			Severity: domain.Critical,
			NodeID:   "node-loop",
			Message:  "Cycle detected involving node-loop",
		},
	}

	out, err := r.Format(findings)
	if err != nil {
		t.Fatalf("Format() unexpected error: %v", err)
	}

	doc := parseSARIF(t, out)
	run := sarifRun0(t, doc)

	results := run["results"].([]interface{})
	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1", len(results))
	}
	res := results[0].(map[string]interface{})
	if res["level"] != "error" {
		t.Errorf("level = %q, want \"error\"", res["level"])
	}
	if res["ruleId"] != "cycle_detection" {
		t.Errorf("ruleId = %q, want \"cycle_detection\"", res["ruleId"])
	}

	// location URI should embed the node ID.
	locations := res["locations"].([]interface{})
	loc := locations[0].(map[string]interface{})
	phys := loc["physicalLocation"].(map[string]interface{})
	artifact := phys["artifactLocation"].(map[string]interface{})
	if artifact["uri"] != "workflow://nodes/node-loop" {
		t.Errorf("uri = %q, want \"workflow://nodes/node-loop\"", artifact["uri"])
	}
}

// TestSARIFReporter_WarningAndInfo verifies Warning→"warning" and Info→"note" mapping.
func TestSARIFReporter_WarningAndInfo(t *testing.T) {
	r := reporter.NewSARIFReporter()
	findings := []domain.Finding{
		{RuleName: "rule-warn", Severity: domain.Warning, NodeID: "n1", Message: "a warning"},
		{RuleName: "rule-info", Severity: domain.Info, NodeID: "n2", Message: "an info"},
	}

	out, err := r.Format(findings)
	if err != nil {
		t.Fatalf("Format() unexpected error: %v", err)
	}

	doc := parseSARIF(t, out)
	run := sarifRun0(t, doc)

	results := run["results"].([]interface{})
	if len(results) != 2 {
		t.Fatalf("results length = %d, want 2", len(results))
	}

	wantLevels := []string{"warning", "note"}
	for i, res := range results {
		m := res.(map[string]interface{})
		if got := m["level"].(string); got != wantLevels[i] {
			t.Errorf("results[%d].level = %q, want %q", i, got, wantLevels[i])
		}
	}

	// Two distinct ruleIds → two rules.
	driver := run["tool"].(map[string]interface{})["driver"].(map[string]interface{})
	rules := driver["rules"].([]interface{})
	if len(rules) != 2 {
		t.Errorf("rules length = %d, want 2", len(rules))
	}
}

// TestSARIFReporter_RuleDeduplication verifies that duplicate RuleNames produce
// a single entry in rules[] but multiple entries in results[].
func TestSARIFReporter_RuleDeduplication(t *testing.T) {
	r := reporter.NewSARIFReporter()
	findings := []domain.Finding{
		{RuleName: "cycle_detection", Severity: domain.Critical, NodeID: "n1", Message: "cycle at n1"},
		{RuleName: "cycle_detection", Severity: domain.Critical, NodeID: "n2", Message: "cycle at n2"},
		{RuleName: "cycle_detection", Severity: domain.Critical, NodeID: "n3", Message: "cycle at n3"},
	}

	out, err := r.Format(findings)
	if err != nil {
		t.Fatalf("Format() unexpected error: %v", err)
	}

	doc := parseSARIF(t, out)
	run := sarifRun0(t, doc)

	// 3 results.
	results := run["results"].([]interface{})
	if len(results) != 3 {
		t.Errorf("results length = %d, want 3", len(results))
	}

	// 1 rule (deduplicated).
	driver := run["tool"].(map[string]interface{})["driver"].(map[string]interface{})
	rules := driver["rules"].([]interface{})
	if len(rules) != 1 {
		t.Errorf("rules length = %d, want 1 (dedup)", len(rules))
	}
	ruleID := rules[0].(map[string]interface{})["id"].(string)
	if ruleID != "cycle_detection" {
		t.Errorf("rules[0].id = %q, want \"cycle_detection\"", ruleID)
	}
}

// TestSARIFReporter_SpecFields verifies the top-level SARIF spec fields and
// driver metadata.
func TestSARIFReporter_SpecFields(t *testing.T) {
	r := reporter.NewSARIFReporter()
	out, err := r.Format([]domain.Finding{
		{RuleName: "unreachable_node", Severity: domain.Warning, NodeID: "dead", Message: "unreachable"},
	})
	if err != nil {
		t.Fatalf("Format() unexpected error: %v", err)
	}

	doc := parseSARIF(t, out)

	// SARIF spec fields.
	if doc["$schema"] != "https://json.schemastore.org/sarif-2.1.0.json" {
		t.Errorf("$schema mismatch: %q", doc["$schema"])
	}
	if doc["version"] != "2.1.0" {
		t.Errorf("version = %q, want \"2.1.0\"", doc["version"])
	}

	run := sarifRun0(t, doc)
	driver := run["tool"].(map[string]interface{})["driver"].(map[string]interface{})

	if driver["name"] != "Shingan" {
		t.Errorf("driver.name = %q, want \"Shingan\"", driver["name"])
	}
	if driver["version"] != "0.1.0" {
		t.Errorf("driver.version = %q, want \"0.1.0\"", driver["version"])
	}
	if driver["informationUri"] != "https://github.com/hatyibei/shingan" {
		t.Errorf("driver.informationUri = %q", driver["informationUri"])
	}
}

// TestSARIFReporter_EmptyNodeID verifies that a finding with no NodeID uses
// the "workflow://graph" fallback URI.
func TestSARIFReporter_EmptyNodeID(t *testing.T) {
	r := reporter.NewSARIFReporter()
	findings := []domain.Finding{
		{RuleName: "graph_rule", Severity: domain.Info, NodeID: "", Message: "graph-level notice"},
	}

	out, err := r.Format(findings)
	if err != nil {
		t.Fatalf("Format() unexpected error: %v", err)
	}

	doc := parseSARIF(t, out)
	run := sarifRun0(t, doc)
	results := run["results"].([]interface{})
	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1", len(results))
	}

	res := results[0].(map[string]interface{})
	locations := res["locations"].([]interface{})
	loc := locations[0].(map[string]interface{})
	phys := loc["physicalLocation"].(map[string]interface{})
	artifact := phys["artifactLocation"].(map[string]interface{})
	if artifact["uri"] != "workflow://graph" {
		t.Errorf("uri = %q, want \"workflow://graph\"", artifact["uri"])
	}
}
