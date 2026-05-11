package reporter_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/infrastructure/reporter"
)

// TestSARIF_LegacyOutputUnchanged is a guard: when no metadata is
// attached, the emitted SARIF matches the pre-enrichment shape so
// existing consumers (GitHub Code Scanning, downstream tooling) don't
// regress just because the reporter gained an optional metadata
// channel.
func TestSARIF_LegacyOutputUnchanged(t *testing.T) {
	r := reporter.NewSARIFReporter()
	out, err := r.Format([]domain.Finding{
		{RuleName: "cycle_detection", NodeID: "a", Severity: domain.Critical, Confidence: 1.0},
	})
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if strings.Contains(string(out), `"helpUri"`) {
		t.Errorf("legacy output should not contain helpUri; got: %s", out)
	}
	if strings.Contains(string(out), `"stability"`) {
		t.Errorf("legacy output should not contain stability; got: %s", out)
	}
	if strings.Contains(string(out), `"shingan-rule"`) {
		t.Errorf("legacy output should not emit shingan-rule tag; got: %s", out)
	}
}

// TestSARIF_BuiltinMetadataEmitted: when a built-in rule's metadata
// is attached, the reportingDescriptor entry surfaces description,
// helpUri, stability=stable, and the framework/category tag chips
// that GitHub Code Scanning renders as filters.
func TestSARIF_BuiltinMetadataEmitted(t *testing.T) {
	r := reporter.NewSARIFReporter().WithRuleMetadata(map[string]reporter.RuleMetadata{
		"cycle_detection": {
			Description: "detects directed cycles in the workflow graph",
			Stability:   "stable",
			Tags:        []string{"correctness", "safety"},
			Frameworks:  []string{"all"},
			// Absolute URL: SARIF helpUri requires `format=uri`, and
			// Codex Slice D #1 drops relative paths. Use the
			// GitHub-resolved equivalent here.
			DocsURL: "https://github.com/hatyibei/shingan/blob/main/docs/cycle-detection-note.md",
		},
	})
	out, err := r.Format([]domain.Finding{
		{RuleName: "cycle_detection", NodeID: "a", Severity: domain.Critical, Confidence: 1.0},
	})
	if err != nil {
		t.Fatalf("Format: %v", err)
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	runs := doc["runs"].([]interface{})
	run := runs[0].(map[string]interface{})
	driver := run["tool"].(map[string]interface{})["driver"].(map[string]interface{})
	rules := driver["rules"].([]interface{})
	rule := rules[0].(map[string]interface{})

	if got := rule["helpUri"]; got != "https://github.com/hatyibei/shingan/blob/main/docs/cycle-detection-note.md" {
		t.Errorf("helpUri: got %v, want absolute URL", got)
	}
	short := rule["shortDescription"].(map[string]interface{})["text"]
	if short != "detects directed cycles in the workflow graph" {
		t.Errorf("shortDescription: got %v", short)
	}

	props := rule["properties"].(map[string]interface{})
	if props["stability"] != "stable" {
		t.Errorf("stability: got %v, want stable", props["stability"])
	}
	tags := props["tags"].([]interface{})
	got := make(map[string]bool, len(tags))
	for _, t := range tags {
		got[t.(string)] = true
	}
	for _, want := range []string{
		"shingan-rule",
		"stability:stable",
		"category:correctness",
		"category:safety",
		"framework:all",
	} {
		if !got[want] {
			t.Errorf("missing tag %q; tags=%v", want, tags)
		}
	}
}

// TestSARIF_PluginMetadataDistinguishable proves the plugin-namespace
// separation: a plugin rule's reportingDescriptor carries
// stability=experimental, which is the filter GitHub Code Scanning
// consumers use to scope to "only built-in findings" or "include
// experimental plugin findings".
func TestSARIF_PluginMetadataDistinguishable(t *testing.T) {
	r := reporter.NewSARIFReporter().WithRuleMetadata(map[string]reporter.RuleMetadata{
		"experimental:my_rule": {
			Description: "flags experimental pattern",
			Stability:   "experimental",
			Tags:        []string{"company-convention"},
			Frameworks:  []string{"langgraph"},
			DocsURL:     "https://example.com/rules/my_rule",
		},
	})
	out, err := r.Format([]domain.Finding{
		{RuleName: "experimental:my_rule", NodeID: "x", Severity: domain.Warning, Confidence: 0.9},
	})
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	body := string(out)
	if !strings.Contains(body, `"stability": "experimental"`) {
		t.Errorf("expected stability=experimental in output; got: %s", body)
	}
	if !strings.Contains(body, "https://example.com/rules/my_rule") {
		t.Errorf("expected plugin DocsURL as helpUri; got: %s", body)
	}
	if !strings.Contains(body, "framework:langgraph") {
		t.Errorf("expected framework tag chip; got: %s", body)
	}
}

// TestSARIF_UnknownRuleStillEmits: a finding whose rule has no
// metadata (e.g. a rule registered in this binary but not yet in the
// catalog table) must still produce a valid reportingDescriptor —
// falling back to the legacy shape. Protects against half-configured
// CI runs panicking.
func TestSARIF_UnknownRuleStillEmits(t *testing.T) {
	r := reporter.NewSARIFReporter().WithRuleMetadata(map[string]reporter.RuleMetadata{
		"some_other_rule": {Description: "x", Stability: "stable"},
	})
	out, err := r.Format([]domain.Finding{
		{RuleName: "uncatalogued_rule", NodeID: "a", Severity: domain.Info, Confidence: 0.5},
	})
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	body := string(out)
	if !strings.Contains(body, `"id": "uncatalogued_rule"`) {
		t.Errorf("expected rule id even without metadata; got: %s", body)
	}
	if strings.Contains(body, `"helpUri":`) && !strings.Contains(body, `"helpUri": ""`) {
		// helpUri may be absent (omitempty) or empty — both are fine.
		// What's NOT fine is a stray value bleeding in from the
		// metadata of a different rule.
		if strings.Contains(body, "x") {
			t.Errorf("uncatalogued rule must not inherit metadata from siblings; got: %s", body)
		}
	}
}

// TestSARIF_RelativeDocsURLOmitted: Codex Slice D #1. Relative
// DocsURL values fail SARIF helpUri schema validation
// (format=uri requires absolute). Verify the reporter drops them
// instead of emitting an invalid URI.
func TestSARIF_RelativeDocsURLOmitted(t *testing.T) {
	r := reporter.NewSARIFReporter().WithRuleMetadata(map[string]reporter.RuleMetadata{
		"cycle_detection": {DocsURL: "docs/cycle-detection-note.md"},
	})
	out, err := r.Format([]domain.Finding{
		{RuleName: "cycle_detection", NodeID: "a", Severity: domain.Critical, Confidence: 1.0},
	})
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	body := string(out)
	if strings.Contains(body, "docs/cycle-detection-note.md") {
		t.Errorf("relative DocsURL must NOT leak into helpUri; got: %s", body)
	}
}

// TestSARIF_NodeIDIsURLEncoded: Codex Slice D #2. NodeIDs with
// spaces / slashes / Unicode used to produce malformed SARIF URIs.
// After url.PathEscape, the encoded form lands in artifactLocation.uri.
func TestSARIF_NodeIDIsURLEncoded(t *testing.T) {
	r := reporter.NewSARIFReporter()
	out, err := r.Format([]domain.Finding{
		{RuleName: "rule_x", NodeID: "task #1", Severity: domain.Warning, Confidence: 0.9},
	})
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	body := string(out)
	if strings.Contains(body, "workflow://nodes/task #1") {
		t.Errorf("raw space in node URI: %s", body)
	}
	if !strings.Contains(body, "workflow://nodes/task%20%231") {
		t.Errorf("expected url-encoded node URI; got: %s", body)
	}
}

// TestSARIF_PrecisionIsMinimumAcrossFindings: Codex Slice D #3.
// Pre-fix, rule-level precision was "first finding's confidence".
// Same finding set in different order produced different precision.
// Now the rule precision is the minimum confidence across findings,
// which is both deterministic and conservative.
func TestSARIF_PrecisionIsMinimumAcrossFindings(t *testing.T) {
	in := []domain.Finding{
		{RuleName: "r", NodeID: "a", Severity: domain.Warning, Confidence: 1.0},
		{RuleName: "r", NodeID: "b", Severity: domain.Warning, Confidence: 0.5},
	}
	rev := []domain.Finding{in[1], in[0]}
	r := reporter.NewSARIFReporter()
	out1, _ := r.Format(in)
	out2, _ := r.Format(rev)
	// Both should pick precision derived from 0.5 (low), regardless
	// of finding order.
	for _, body := range [][]byte{out1, out2} {
		if !strings.Contains(string(body), `"precision": "low"`) {
			t.Errorf("expected precision=low (min of 1.0 and 0.5); got: %s", body)
		}
	}
}
