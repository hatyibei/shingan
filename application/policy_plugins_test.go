package application

import (
	"os"
	"strings"
	"testing"

	"github.com/hatyibei/shingan/plugin"
)

// TestVerifyRequiredPlugins_NilPolicy: defensive — caller should be
// allowed to pass nil even when policy discovery turned up nothing.
func TestVerifyRequiredPlugins_NilPolicy(t *testing.T) {
	if err := VerifyRequiredPlugins(nil, []string{"any"}); err != nil {
		t.Errorf("nil policy must succeed, got %v", err)
	}
}

// TestVerifyRequiredPlugins_EmptyPlugins: an empty `plugins:` list is
// the default and means "no opinion" — must not error.
func TestVerifyRequiredPlugins_EmptyPlugins(t *testing.T) {
	if err := VerifyRequiredPlugins(&Policy{}, []string{"cycle_detection"}); err != nil {
		t.Errorf("empty plugins list must succeed, got %v", err)
	}
}

// TestVerifyRequiredPlugins_AllPresent: happy path.
func TestVerifyRequiredPlugins_AllPresent(t *testing.T) {
	p := &Policy{Plugins: []string{"experimental:a", "experimental:b"}}
	available := []string{"cycle_detection", "experimental:a", "experimental:b"}
	if err := VerifyRequiredPlugins(p, available); err != nil {
		t.Errorf("all-present must succeed, got %v", err)
	}
}

// TestVerifyRequiredPlugins_MissingReported: the failure case — error
// must name the missing rules AND point at the wrapper-binary docs.
// We assert both because the user's two recovery actions are
// "fix the typo in .shingan.yaml" and "rebuild with the plugin", and
// the error has to be actionable in both directions.
func TestVerifyRequiredPlugins_MissingReported(t *testing.T) {
	p := &Policy{Plugins: []string{"experimental:a", "experimental:missing"}}
	available := []string{"experimental:a"}
	err := VerifyRequiredPlugins(p, available)
	if err == nil {
		t.Fatal("expected error for missing plugin, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "experimental:missing") {
		t.Errorf("error must name the missing plugin; got: %s", msg)
	}
	if !strings.Contains(msg, "wrapper") || !strings.Contains(msg, "plugin-sdk.md") {
		t.Errorf("error must point at wrapper-binary build docs; got: %s", msg)
	}
}

// TestVerifyRequiredPlugins_MultipleMissingAllReported asserts the
// error lists every missing plugin, not just the first. Users
// debugging a broken CI run shouldn't have to fix-and-retry to
// discover the next missing dep.
func TestVerifyRequiredPlugins_MultipleMissingAllReported(t *testing.T) {
	p := &Policy{Plugins: []string{"experimental:a", "experimental:b", "experimental:c"}}
	available := []string{} // none registered
	err := VerifyRequiredPlugins(p, available)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"experimental:a", "experimental:b", "experimental:c"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error must name %q; got: %s", want, msg)
		}
	}
}

// TestPolicy_UnmarshalsPluginsKey asserts the YAML round-trip works —
// the `plugins:` key has to land in the Policy struct.
func TestPolicy_UnmarshalsPluginsKey(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/.shingan.yaml"
	body := `plugins:
  - experimental:todo_node_marker
  - experimental:company_naming
rules:
  cycle_detection:
    severity: warning
`
	if err := writeFile(path, body); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	p, err := LoadPolicy(path)
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	if len(p.Plugins) != 2 {
		t.Fatalf("Plugins len: got %d, want 2 — Plugins=%v", len(p.Plugins), p.Plugins)
	}
	if p.Plugins[0] != "experimental:todo_node_marker" {
		t.Errorf("Plugins[0]: got %q, want experimental:todo_node_marker", p.Plugins[0])
	}
	// Sanity: other keys still parse with the Plugins addition.
	if _, ok := p.Rules["cycle_detection"]; !ok {
		t.Error("Rules.cycle_detection missing — Plugins addition broke other unmarshalling")
	}
}

// TestVerifyRequiredPlugins_BuiltinNameRejected covers Codex round-2
// P3: a non-prefixed name (e.g. a built-in like "cycle_detection")
// in plugins: must fail validation even if it's in `available`. The
// plugins: contract is specifically for experimental: plugin rules;
// built-in tuning belongs under `rules:`.
func TestVerifyRequiredPlugins_BuiltinNameRejected(t *testing.T) {
	p := &Policy{Plugins: []string{"cycle_detection"}}
	available := []string{"cycle_detection"} // even though it's "available"
	err := VerifyRequiredPlugins(p, available)
	if err == nil {
		t.Fatal("expected error for non-prefixed plugin entry, got nil")
	}
	if !strings.Contains(err.Error(), "experimental:") {
		t.Errorf("error must hint at the experimental: prefix requirement; got: %s", err)
	}
}

// TestVerifyRequiredPlugins_MixedErrorsReported asserts both the
// bad-prefix and missing-plugin error categories surface in a single
// validation pass when both apply.
func TestVerifyRequiredPlugins_MixedErrorsReported(t *testing.T) {
	p := &Policy{Plugins: []string{
		"cycle_detection",       // bad prefix
		"experimental:absent",   // wrong binary
		"experimental:present",  // ok
	}}
	available := []string{"experimental:present"}
	err := VerifyRequiredPlugins(p, available)
	if err == nil {
		t.Fatal("expected combined error")
	}
	msg := err.Error()
	for _, want := range []string{"cycle_detection", "experimental:absent", "experimental:"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error must mention %q; got: %s", want, msg)
		}
	}
}

// TestRuleNamesFromManifests is a trivial-but-load-bearing helper —
// the CLI uses it to feed VerifyRequiredPlugins.
func TestRuleNamesFromManifests(t *testing.T) {
	got := RuleNamesFromManifests([]RuleManifest{
		{Name: "a"}, {Name: "b"}, {Name: "c"},
	})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("len: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o644)
}

// TestExperimentalPrefix_MatchesSDK covers Slice A #9: the
// experimentalPrefix constant in application/policy.go is duplicated
// from plugin.ExperimentalPrefix to keep the dependency direction
// clean (application → plugin, not vice versa). If the two drift,
// VerifyRequiredPlugins rejects names that plugin.Register would
// accept (or vice versa). This test fails when either constant
// changes independently.
func TestExperimentalPrefix_MatchesSDK(t *testing.T) {
	if experimentalPrefix != plugin.ExperimentalPrefix {
		t.Fatalf("experimentalPrefix drift: application=%q vs plugin.ExperimentalPrefix=%q",
			experimentalPrefix, plugin.ExperimentalPrefix)
	}
}
