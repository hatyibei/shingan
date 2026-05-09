package application

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hatyibei/shingan/domain"
)

func writePolicy(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".shingan.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	return path
}

func TestLoadPolicy_BasicSeverityOverride(t *testing.T) {
	path := writePolicy(t, `
rules:
  eval_missing:
    severity: warning
  retry_storm:
    enabled: false
`)
	policy, err := LoadPolicy(path)
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	if policy.Rules["eval_missing"].Severity != "warning" {
		t.Errorf("eval_missing severity = %q, want warning", policy.Rules["eval_missing"].Severity)
	}
	if rs := policy.Rules["retry_storm"]; rs.Enabled == nil || *rs.Enabled {
		t.Errorf("retry_storm should be Enabled=false")
	}
}

func TestApplyPolicy_DowngradesSeverity(t *testing.T) {
	policy := &Policy{Rules: map[string]RuleConfig{
		"eval_missing": {Severity: "warning"},
	}}
	findings := []domain.Finding{
		{RuleName: "eval_missing", Severity: domain.Critical},
		{RuleName: "cycle_detection", Severity: domain.Critical},
	}
	out := ApplyPolicy(findings, policy)
	if len(out) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(out))
	}
	for _, f := range out {
		if f.RuleName == "eval_missing" && f.Severity != domain.Warning {
			t.Errorf("eval_missing severity = %v, want Warning", f.Severity)
		}
		if f.RuleName == "cycle_detection" && f.Severity != domain.Critical {
			t.Errorf("cycle_detection should retain Critical, got %v", f.Severity)
		}
	}
}

func TestApplyPolicy_DisablesRule(t *testing.T) {
	disabled := false
	policy := &Policy{Rules: map[string]RuleConfig{
		"retry_storm": {Enabled: &disabled},
	}}
	findings := []domain.Finding{
		{RuleName: "retry_storm", Severity: domain.Warning},
		{RuleName: "cycle_detection", Severity: domain.Critical},
	}
	out := ApplyPolicy(findings, policy)
	if len(out) != 1 || out[0].RuleName != "cycle_detection" {
		t.Errorf("expected only cycle_detection to remain; got %+v", out)
	}
}

func TestApplyPolicy_OffViaSeverityKeyword(t *testing.T) {
	policy := &Policy{Rules: map[string]RuleConfig{
		"retry_storm": {Severity: "off"},
	}}
	findings := []domain.Finding{
		{RuleName: "retry_storm", Severity: domain.Warning},
	}
	out := ApplyPolicy(findings, policy)
	if len(out) != 0 {
		t.Errorf("expected `severity: off` to drop the finding; got %+v", out)
	}
}

func TestApplyPolicy_PathOverride(t *testing.T) {
	disabled := false
	policy := &Policy{
		Rules: map[string]RuleConfig{
			"cycle_detection": {Severity: "critical"},
		},
		Overrides: []OverridePolicy{
			{
				Paths: []string{"legacy/**"},
				Rules: map[string]RuleConfig{
					"cycle_detection": {Enabled: &disabled},
				},
			},
		},
	}
	findings := []domain.Finding{
		{RuleName: "cycle_detection", Severity: domain.Critical, SourceFile: "legacy/orchestrator.py"},
		{RuleName: "cycle_detection", Severity: domain.Critical, SourceFile: "src/orchestrator.py"},
	}
	out := ApplyPolicy(findings, policy)
	if len(out) != 1 || out[0].SourceFile != "src/orchestrator.py" {
		t.Errorf("expected only src/ finding to remain (legacy/ disabled); got %+v", out)
	}
}

func TestDiscoverPolicy_WalksUp(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "deep", "subdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(root, ".shingan.yaml")
	if err := os.WriteFile(policyPath, []byte("rules: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverPolicy(subdir)
	if err != nil {
		t.Fatalf("DiscoverPolicy: %v", err)
	}
	if got != policyPath {
		t.Errorf("got %q, want %q", got, policyPath)
	}
}

func TestDiscoverPolicy_NoneFound(t *testing.T) {
	dir := t.TempDir()
	got, err := DiscoverPolicy(dir)
	if err != nil {
		t.Fatalf("DiscoverPolicy: %v", err)
	}
	if got != "" {
		t.Errorf("expected '' for missing policy; got %q", got)
	}
}
