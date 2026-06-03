package domain

import (
	"encoding/json"
	"testing"
	"time"
)

// fp builds a fingerprint from a finding — the canonical way callers should
// construct expected baseline entries now that the digest is derived.
func fp(f Finding) FindingFingerprint { return Fingerprint(f) }

func TestBaseline_Contains_Match(t *testing.T) {
	f := Finding{RuleName: "cycle_detection", NodeID: "loop_body", Message: "cycle detected"}
	b := &Baseline{Findings: []FindingFingerprint{fp(f)}}
	if !b.Contains(f) {
		t.Error("Contains should match identical fingerprint")
	}
}

func TestBaseline_Contains_NoMatch(t *testing.T) {
	base := Finding{RuleName: "cycle_detection", NodeID: "loop_body", Message: "cycle detected"}
	b := &Baseline{Findings: []FindingFingerprint{fp(base)}}
	cases := []Finding{
		{RuleName: "other_rule", NodeID: "loop_body", Message: "cycle detected"},
		{RuleName: "cycle_detection", NodeID: "other_node", Message: "cycle detected"},
		{RuleName: "cycle_detection", NodeID: "loop_body", Message: "different message"},
	}
	for i, f := range cases {
		if b.Contains(f) {
			t.Errorf("case %d: Contains should not match differing fingerprint %+v", i, f)
		}
	}
}

func TestBaseline_Contains_Nil(t *testing.T) {
	var b *Baseline
	if b.Contains(Finding{RuleName: "x", NodeID: "y", Message: "z"}) {
		t.Error("nil Baseline should never Contains anything")
	}
}

func TestBaseline_Contains_IgnoresSeverityAndConfidence(t *testing.T) {
	base := Finding{RuleName: "r", NodeID: "n", Message: "m"}
	b := &Baseline{Findings: []FindingFingerprint{fp(base)}}
	f := Finding{
		RuleName: "r", NodeID: "n", Message: "m",
		Severity:   Critical,
		Confidence: 0.3,
	}
	if !b.Contains(f) {
		t.Error("fingerprint match should be independent of Severity/Confidence")
	}
}

func TestFingerprint(t *testing.T) {
	f := Finding{
		RuleName:   "cycle_detection",
		NodeID:     "loop_body",
		Message:    "cycle detected",
		SourceFile: "wf.py",
		Severity:   Critical,
		Confidence: 1.0,
		Suggestion: "add max_iterations",
	}
	got := Fingerprint(f)
	if got.RuleName != "cycle_detection" || got.NodeID != "loop_body" || got.SourceFile != "wf.py" {
		t.Errorf("Fingerprint dropped identity fields: %+v", got)
	}
	if got.MessageDigest == "" {
		t.Error("Fingerprint should populate a MessageDigest")
	}
}

// TestFingerprint_StableAcrossMessageNumericChange is the core ADR-016
// regression: a numeric value embedded in a message (fan-out count) must not
// change the fingerprint.
func TestFingerprint_StableAcrossMessageNumericChange(t *testing.T) {
	f1 := Finding{RuleName: "max_parallel_branches", NodeID: "n1", Message: "fan-out: 7 branches"}
	f2 := Finding{RuleName: "max_parallel_branches", NodeID: "n1", Message: "fan-out: 9 branches"}
	if Fingerprint(f1) != Fingerprint(f2) {
		t.Errorf("fingerprint should be stable across numeric message changes: %+v vs %+v",
			Fingerprint(f1), Fingerprint(f2))
	}
}

// TestFingerprint_DistinguishesQuotedDiscriminators guards the codex P2 fix:
// rules like unbounded_tool_arg emit one finding per offending schema field on
// the SAME node, distinguished only by the quoted field path. The fingerprint
// must keep them distinct — otherwise a baseline captured with field "query"
// silently suppresses a genuinely-new field "payload" finding.
func TestFingerprint_DistinguishesQuotedDiscriminators(t *testing.T) {
	query := Finding{RuleName: "unbounded_tool_arg", NodeID: "n1",
		Message: `Tool node "fetch" schema field "query" (string) has no maxLength`}
	payload := Finding{RuleName: "unbounded_tool_arg", NodeID: "n1",
		Message: `Tool node "fetch" schema field "payload" (string) has no maxLength`}
	if Fingerprint(query) == Fingerprint(payload) {
		t.Fatalf("per-field findings on the same node must have distinct fingerprints")
	}
	// User-visible behavior: a baseline of the "query" finding must NOT suppress
	// the new "payload" finding, but must still match the identical "query" one.
	b := NewBaselineFromFindings([]Finding{query})
	if b.Contains(payload) {
		t.Errorf(`baseline for field "query" wrongly suppressed a new field "payload" finding`)
	}
	if !b.Contains(query) {
		t.Errorf("baseline must still match the identical query finding")
	}
}

// TestFingerprint_StableAcrossTypoFix demonstrates the MessageTemplateID
// mechanism: when a rule supplies a stable template ID, a message typo fix
// leaves the fingerprint unchanged.
func TestFingerprint_StableAcrossTypoFix(t *testing.T) {
	f1 := Finding{RuleName: "loop_guard", NodeID: "n1", Message: "max_iteraions not set", MessageTemplateID: "loop_guard.max_iterations_missing"}
	f2 := Finding{RuleName: "loop_guard", NodeID: "n1", Message: "max_iterations not set", MessageTemplateID: "loop_guard.max_iterations_missing"}
	if Fingerprint(f1) != Fingerprint(f2) {
		t.Errorf("template-ID fingerprint should survive a message typo fix")
	}
	if Fingerprint(f1).MessageDigest != "loop_guard.max_iterations_missing" {
		t.Errorf("template ID should be used verbatim as the digest, got %q", Fingerprint(f1).MessageDigest)
	}
}

func TestNormalizeMessage(t *testing.T) {
	cases := []struct{ in, want string }{
		{"fan-out: 7 branches", "fan-out: [N] branches"},
		{"retries=3 over 1.5s", "retries=[N] over [N]s"},
		{`node "agent_a" cycles`, `node "agent_a" cycles`}, // quoted identity preserved
		{`tool "fetch_7"`, `tool "fetch_[N]"`},             // number inside a quote still normalized
		{"no variables here", "no variables here"},
	}
	for _, tc := range cases {
		if got := normalizeMessage(tc.in); got != tc.want {
			t.Errorf("normalizeMessage(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

// TestFingerprint_V1Migration verifies a legacy v1 fingerprint (full message)
// unmarshals into a digest matching the same finding's current fingerprint, so
// unchanged findings stay suppressed across the schema upgrade.
func TestFingerprint_V1Migration(t *testing.T) {
	legacy := `{"rule":"cycle_detection","node_id":"n1","message":"fan-out: 7 branches"}`
	var got FindingFingerprint
	if err := json.Unmarshal([]byte(legacy), &got); err != nil {
		t.Fatalf("unmarshal legacy fingerprint: %v", err)
	}
	want := Fingerprint(Finding{RuleName: "cycle_detection", NodeID: "n1", Message: "fan-out: 7 branches"})
	if got != want {
		t.Errorf("v1 migration mismatch: got %+v want %+v", got, want)
	}
	// A later run with a different fan-out count still matches the migrated v1.
	later := Fingerprint(Finding{RuleName: "cycle_detection", NodeID: "n1", Message: "fan-out: 12 branches"})
	if got != later {
		t.Errorf("migrated v1 fingerprint should match numerically-drifted finding")
	}
}

func TestNewBaselineFromFindings(t *testing.T) {
	findings := []Finding{
		{RuleName: "r1", NodeID: "n1", Message: "m1", Severity: Critical},
		{RuleName: "r2", NodeID: "n2", Message: "m2", Severity: Warning},
	}
	before := time.Now().UTC()
	b := NewBaselineFromFindings(findings)
	after := time.Now().UTC()

	if b.Version != BaselineSchemaVersion {
		t.Errorf("want version %d, got %d", BaselineSchemaVersion, b.Version)
	}
	if len(b.Findings) != 2 {
		t.Fatalf("want 2 fingerprints, got %d", len(b.Findings))
	}
	if b.Findings[0].RuleName != "r1" || b.Findings[1].RuleName != "r2" {
		t.Errorf("fingerprints out of order: %+v", b.Findings)
	}
	if b.GeneratedAt.Before(before) || b.GeneratedAt.After(after) {
		t.Errorf("GeneratedAt=%v not within [%v, %v]", b.GeneratedAt, before, after)
	}
}

func TestNewBaselineFromFindings_Empty(t *testing.T) {
	b := NewBaselineFromFindings(nil)
	if b == nil {
		t.Fatal("returned nil")
	}
	if len(b.Findings) != 0 {
		t.Errorf("want 0 fingerprints, got %d", len(b.Findings))
	}
}
