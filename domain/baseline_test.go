package domain

import (
	"testing"
	"time"
)

func TestBaseline_Contains_Match(t *testing.T) {
	b := &Baseline{
		Findings: []FindingFingerprint{
			{RuleName: "cycle_detection", NodeID: "loop_body", Message: "cycle detected"},
		},
	}
	f := Finding{RuleName: "cycle_detection", NodeID: "loop_body", Message: "cycle detected"}
	if !b.Contains(f) {
		t.Error("Contains should match identical fingerprint")
	}
}

func TestBaseline_Contains_NoMatch(t *testing.T) {
	b := &Baseline{
		Findings: []FindingFingerprint{
			{RuleName: "cycle_detection", NodeID: "loop_body", Message: "cycle detected"},
		},
	}
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
	b := &Baseline{
		Findings: []FindingFingerprint{
			{RuleName: "r", NodeID: "n", Message: "m"},
		},
	}
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
		Severity:   Critical,
		Confidence: 1.0,
		Suggestion: "add max_iterations",
	}
	fp := Fingerprint(f)
	if fp.RuleName != "cycle_detection" || fp.NodeID != "loop_body" || fp.Message != "cycle detected" {
		t.Errorf("Fingerprint dropped identity fields: %+v", fp)
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
