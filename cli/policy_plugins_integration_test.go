package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAnalyze_MissingPluginIsRejected runs the analyze pipeline
// against a valid graph while pointing `.shingan.yaml` at a plugin
// rule name not registered in this binary. The expected outcome is
// a hard error from executeAnalyze with the build-pointer hint —
// that's the v0.9 contract between the project config and the
// running binary's actual catalog.
//
// Calls executeAnalyze directly (not via cobra) because the
// happy-path command terminates with os.Exit, which doesn't compose
// with testing.T. The error path here returns before os.Exit, but
// using the helper keeps the two tests symmetric.
func TestAnalyze_MissingPluginIsRejected(t *testing.T) {
	tmp := t.TempDir()
	graphPath := filepath.Join(tmp, "graph.json")
	if err := os.WriteFile(graphPath, []byte(`{
        "entry_node_id": "a",
        "nodes": [{"id": "a", "name": "a", "type": "llm", "config": {}}],
        "edges": []
    }`), 0o644); err != nil {
		t.Fatalf("write graph: %v", err)
	}

	policyPath := filepath.Join(tmp, ".shingan.yaml")
	if err := os.WriteFile(policyPath, []byte(`plugins:
  - experimental:does_not_exist
`), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	flags := &analyzeFlags{
		input:  graphPath,
		format: "json",
		output: "json",
		policy: policyPath,
	}
	_, err := executeAnalyze(flags)
	if err == nil {
		t.Fatal("expected executeAnalyze to fail when policy declares a missing plugin")
	}
	msg := err.Error()
	if !strings.Contains(msg, "experimental:does_not_exist") {
		t.Errorf("error must name the missing plugin; got: %s", msg)
	}
	if !strings.Contains(msg, "wrapper") {
		t.Errorf("error must hint at the wrapper-binary build; got: %s", msg)
	}
}

// TestAnalyze_DeclaredPresentPluginPasses asserts the inverse: when
// `.shingan.yaml plugins:` lists a name that DOES exist in the
// catalog (we use a real built-in here so the test doesn't have to
// side-effect-import a plugin package), executeAnalyze proceeds
// normally. This pins the "opinion satisfied" branch of
// VerifyRequiredPlugins.
func TestAnalyze_DeclaredPresentPluginPasses(t *testing.T) {
	tmp := t.TempDir()
	graphPath := filepath.Join(tmp, "graph.json")
	if err := os.WriteFile(graphPath, []byte(`{
        "entry_node_id": "a",
        "nodes": [{"id": "a", "name": "a", "type": "llm", "config": {}}],
        "edges": []
    }`), 0o644); err != nil {
		t.Fatalf("write graph: %v", err)
	}
	policyPath := filepath.Join(tmp, ".shingan.yaml")
	if err := os.WriteFile(policyPath, []byte(`plugins:
  - cycle_detection
`), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	outPath := filepath.Join(tmp, "out.json")
	flags := &analyzeFlags{
		input:      graphPath,
		format:     "json",
		output:     "json",
		outputFile: outPath,
		policy:     policyPath,
	}
	if _, err := executeAnalyze(flags); err != nil {
		t.Errorf("declared-and-present plugin must not fail analyze: %v", err)
	}
}
