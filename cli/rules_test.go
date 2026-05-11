package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hatyibei/shingan/application"
	infraFactory "github.com/hatyibei/shingan/infrastructure/factory"
)

// TestListRuleManifests_StaticTableCoversAllRules asserts every
// registered rule has a row in application/rule_catalog.go's static
// metadata table. The moment a new rule lands in domain/rules/ without
// a corresponding manifest entry, the catalog renderer downstream
// (IDE hovers, shingan.dev catalog page, SARIF taxonomy) silently
// drops it — this test makes that mismatch fail at CI time.
//
// Tests live in cmd/shingan/ because application/ can't import
// infrastructure/factory/ (Onion). The wiring point in main.go is the
// only package that depends on both.
func TestListRuleManifests_StaticTableCoversAllRules(t *testing.T) {
	rules := infraFactory.NewAnalyzerFactory().CreateAll()
	catalog := application.ListRuleManifests(rules)
	if len(catalog) == 0 {
		t.Fatal("ListRuleManifests returned empty — factory wiring or registry broken")
	}
	// We can't access the unexported staticRuleMeta map directly from
	// here, so we detect drift by checking each manifest has
	// non-empty Frameworks and Tags — both fields originate in the
	// static table. A rule registered without a static row would come
	// back with nil slices.
	for _, m := range catalog {
		if len(m.Frameworks) == 0 {
			t.Errorf("rule %q has empty Frameworks — add a row to staticRuleMeta in application/rule_catalog.go", m.Name)
		}
		if len(m.Tags) == 0 {
			t.Errorf("rule %q has empty Tags — add a row to staticRuleMeta in application/rule_catalog.go", m.Name)
		}
	}
}

// TestListRuleManifests_DescriptionPopulated verifies every manifest
// has a non-empty Description distinct from its Name. Description
// falls back to Name when RuleExplanations has no entry — that's the
// signal a rule landed without an explanation block.
func TestListRuleManifests_DescriptionPopulated(t *testing.T) {
	rules := infraFactory.NewAnalyzerFactory().CreateAll()
	catalog := application.ListRuleManifests(rules)
	for _, m := range catalog {
		if strings.TrimSpace(m.Description) == "" {
			t.Errorf("rule %q has empty Description", m.Name)
		}
		if m.Description == m.Name {
			t.Errorf("rule %q has no RuleExplanations entry — Description fell back to Name", m.Name)
		}
	}
}

// TestListRuleManifests_JSONRoundTrip asserts the catalog marshals to
// valid JSON that round-trips losslessly. The `shingan rules
// --format=json` output is consumed by IDEs, shingan.dev, and CI
// policy generators — schema stability matters.
func TestListRuleManifests_JSONRoundTrip(t *testing.T) {
	rules := infraFactory.NewAnalyzerFactory().CreateAll()
	catalog := application.ListRuleManifests(rules)
	raw, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var round []application.RuleManifest
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(round) != len(catalog) {
		t.Fatalf("round-trip changed length: %d → %d", len(catalog), len(round))
	}
	for i := range catalog {
		if catalog[i].Name != round[i].Name {
			t.Errorf("order changed at %d: %q vs %q", i, catalog[i].Name, round[i].Name)
		}
	}
}

// TestRulesCmd_TableOutput runs the `shingan rules` subcommand with the
// default table format and asserts the expected columns + at least
// one well-known rule name shows up.
func TestRulesCmd_TableOutput(t *testing.T) {
	cmd := newRulesCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	got := out.String()
	for _, want := range []string{"RULE", "SEVERITY", "FRAMEWORKS", "DESCRIPTION", "cycle_detection"} {
		if !strings.Contains(got, want) {
			t.Errorf("table output missing %q; got:\n%s", want, got)
		}
	}
}

// TestRulesCmd_JSONOutput runs `shingan rules --format=json` and
// asserts the output parses as an array of RuleManifest.
func TestRulesCmd_JSONOutput(t *testing.T) {
	cmd := newRulesCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Flags().Set("format", "json"); err != nil {
		t.Fatalf("set --format: %v", err)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	var got []application.RuleManifest
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json output didn't parse: %v\nout: %s", err, out.String())
	}
	if len(got) == 0 {
		t.Fatal("json output had zero rules")
	}
}
