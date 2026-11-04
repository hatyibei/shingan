package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCLI executes the root command in-process with captured writers.
func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	root := NewRootCmd()
	silenceErrors(root)
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	code = runWithSilencedRoot(root)
	return code, out.String(), errBuf.String()
}

func copyN8nFixture(t *testing.T, name, dst string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "n8n", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

const loopGraphJSON = `{
  "entry_node_id": "a",
  "nodes": [{"id": "a", "name": "a", "type": "loop", "config": {}}],
  "edges": [{"from": "a", "to": "a"}]
}`

// The path can be passed positionally instead of via --input.
func TestAnalyze_PositionalInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.json")
	if err := os.WriteFile(path, []byte(loopGraphJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := runCLI(t, "analyze", path)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%q", code, stderr)
	}
	if !strings.Contains(out, "loop_guard") {
		t.Errorf("expected loop_guard in report, got %s", out)
	}
	// Non-TTY stdout keeps the historical JSON default.
	var v any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Errorf("default output to a non-terminal must stay JSON: %v\n%s", err, out)
	}
}

func TestAnalyze_PositionalAndInputConflict(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte(loopGraphJSON), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, _, _ := runCLI(t, "analyze", a, "--input", b)
	if code == 0 || code == 2 {
		t.Fatalf("conflicting positional + --input must be a usage error, got exit %d", code)
	}
	// Same path both ways is fine.
	if code, _, stderr := runCLI(t, "analyze", a, "--input", a); code != 2 {
		t.Fatalf("identical positional + --input should analyze; exit=%d stderr=%q", code, stderr)
	}
}

func TestAnalyze_MissingInputIsActionable(t *testing.T) {
	code, _, _ := runCLI(t, "analyze")
	if code == 0 {
		t.Fatalf("analyze with no path must fail")
	}
	_, err := executeAnalyzeCmdErr(t)
	if !strings.Contains(err, "shingan analyze workflow.json") {
		t.Errorf("error should show an example invocation, got %q", err)
	}
}

// executeAnalyzeCmdErr runs `analyze` with no args through the cobra command
// directly so the RunE error text is observable.
func executeAnalyzeCmdErr(t *testing.T) (int, string) {
	t.Helper()
	cmd := newAnalyzeCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)
	err := cmd.Execute()
	if err == nil {
		return 0, ""
	}
	return 1, err.Error()
}

// Without --format, an n8n export is detected and parsed as n8n (previously it
// was fed to the generic json parser and failed / produced nothing useful).
func TestAnalyze_DefaultFormatDetectsN8n(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flow.json")
	copyN8nFixture(t, "loop.json", path)

	code, out, stderr := runCLI(t, "analyze", path)
	if !strings.Contains(stderr, `detected format "n8n"`) {
		t.Errorf("expected a detection notice on stderr, got %q", stderr)
	}
	// loop.json's Split-In-Batches cycle is a Critical cycle_detection hit
	// once parsed as n8n; under the old json default it failed to parse.
	if code != 2 || !strings.Contains(out, "cycle_detection") {
		t.Fatalf("n8n export did not analyze as n8n; exit=%d stdout=%s stderr=%s", code, out, stderr)
	}
}

// A native shingan JSON graph stays on the json parser with no notice.
func TestAnalyze_DefaultFormatJSONIsQuiet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.json")
	if err := os.WriteFile(path, []byte(loopGraphJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, stderr := runCLI(t, "analyze", path)
	if strings.Contains(stderr, "detected format") {
		t.Errorf("plain json default must not print a detection notice, got %q", stderr)
	}
}

// Explicit --format auto stays strict: unknown input is an error, not a json
// fallback, so hooks can skip unrecognised files.
func TestAnalyze_ExplicitAutoStrict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveInputFormat("auto", path, &bytes.Buffer{}); err == nil {
		t.Fatal("--format auto on an unrecognised file must error")
	}
	got, err := resolveInputFormat("", path, &bytes.Buffer{})
	if err != nil || got != "json" {
		t.Fatalf("implicit format must fall back to json, got %q, %v", got, err)
	}
}

// A directory of n8n exports analyzes every workflow file and ignores
// unrelated JSON neighbours and vendored trees.
func TestAnalyze_N8nDirectory(t *testing.T) {
	dir := t.TempDir()
	copyN8nFixture(t, "loop.json", filepath.Join(dir, "a.json"))
	copyN8nFixture(t, "branching.json", filepath.Join(dir, "sub", "b.json"))
	// Would fail to parse as an n8n workflow if it were not filtered out.
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A vendored n8n-looking file must not be analyzed.
	copyN8nFixture(t, "loop.json", filepath.Join(dir, "node_modules", "pkg", "vendored.json"))

	code, out, stderr := runCLI(t, "analyze", dir, "--output", "json")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (loop.json has a Critical cycle); stderr=%s", code, stderr)
	}
	for _, want := range []string{"a.json", "b.json"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected findings attributed to %s; report:\n%s", want, out)
		}
	}
	if strings.Contains(stderr, "directory input is only supported") {
		t.Fatalf("n8n directory input must be supported; stderr=%s", stderr)
	}
	if strings.Contains(stderr, "skipping") {
		t.Errorf("non-workflow JSON should be filtered silently, got stderr=%s", stderr)
	}
	if strings.Contains(out, "vendored.json") {
		t.Errorf("node_modules must not be walked; report mentions vendored.json:\n%s", out)
	}
}

func TestResolveOutputFormat(t *testing.T) {
	cases := []struct {
		flag, file string
		want       string
	}{
		{"", "", "json"}, // buffer stdout (not a TTY)
		{"", "out.json", "json"},
		{"markdown", "", "markdown"},
		{"sarif", "x.sarif", "sarif"},
	}
	for _, c := range cases {
		if got := resolveOutputFormat(c.flag, c.file, &bytes.Buffer{}); got != c.want {
			t.Errorf("resolveOutputFormat(%q,%q) = %q, want %q", c.flag, c.file, got, c.want)
		}
	}
}

// Directory detection ties must not depend on map iteration order.
func TestDetectFormatDir_TieBreakDeterministic(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.py", "from crewai import Agent\n")
	write("b.py", "from langgraph.graph import StateGraph\n")
	for i := 0; i < 30; i++ {
		if got := detectFormatDir(dir); got != "crewai" {
			t.Fatalf("run %d: tie resolved to %q, want lexicographically-first %q", i, got, "crewai")
		}
	}
}
