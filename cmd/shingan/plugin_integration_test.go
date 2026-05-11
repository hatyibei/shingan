package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hatyibei/shingan/application"
	infraFactory "github.com/hatyibei/shingan/infrastructure/factory"

	// Side-effect import: registers the example plugin rule at init().
	// This simulates what a downstream plugin wrapper would do.
	_ "github.com/hatyibei/shingan/examples/plugin-template"
)

// TestPluginAppearsInRulesCatalog asserts that a plugin registered
// via `plugin.MustRegister` in an init() shows up in `shingan rules`
// alongside the built-ins. This is the v0.9 contract for downstream
// IDE / docs / SARIF consumers: external rules and built-ins are
// indistinguishable in the catalog except for the `stability` flag.
func TestPluginAppearsInRulesCatalog(t *testing.T) {
	rules := infraFactory.NewAnalyzerFactory().CreateAll()
	catalog := application.ListRuleManifests(rules)
	const want = "experimental:todo_node_marker"
	found := false
	for _, m := range catalog {
		if m.Name == want {
			found = true
			if m.Stability != "experimental" {
				t.Errorf("plugin rule Stability: got %q, want %q", m.Stability, "experimental")
			}
			if len(m.Frameworks) == 0 {
				t.Errorf("plugin rule Frameworks empty")
			}
			break
		}
	}
	if !found {
		names := make([]string, 0, len(catalog))
		for _, m := range catalog {
			names = append(names, m.Name)
		}
		t.Errorf("plugin rule %q missing from catalog; saw: %v", want, names)
	}
}

// TestRulesCmd_IncludesPluginInJSONOutput asserts the user-visible
// `shingan rules --format=json` includes the plugin rule. This is
// the contract surface plugin authors actually verify when they
// wire up their wrapper binary.
func TestRulesCmd_IncludesPluginInJSONOutput(t *testing.T) {
	cmd := newRulesCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Flags().Set("format", "json"); err != nil {
		t.Fatalf("set --format: %v", err)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	var catalog []application.RuleManifest
	if err := json.Unmarshal(out.Bytes(), &catalog); err != nil {
		t.Fatalf("json: %v", err)
	}
	found := false
	for _, m := range catalog {
		if m.Name == "experimental:todo_node_marker" {
			found = true
		}
	}
	if !found {
		t.Error("plugin rule not in `shingan rules --format=json` output")
	}
}

// TestRulesCmd_TableMarksExperimentalStability is a UX assertion:
// table output is the primary surface terminal users see, so the
// experimental tag has to be visible there.
func TestRulesCmd_TableMarksExperimentalStability(t *testing.T) {
	cmd := newRulesCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	// Table should at minimum show the plugin's name.
	if !strings.Contains(out.String(), "experimental:todo_node_marker") {
		t.Errorf("plugin rule name missing from table output:\n%s", out.String())
	}
}
