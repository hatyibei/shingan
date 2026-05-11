package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRun_ReturnsAnalysisExitCode locks in Codex Slice B #1: cli.Run
// must surface analyze's exit code (1 for Warning, 2 for Critical)
// as its return value without calling os.Exit. A pre-fix analyze
// would terminate this test process via os.Exit(2).
//
// Uses a synthetic buggy.json with an unbounded loop so the
// cycle/loop_guard rules emit a Critical finding deterministically.
func TestRun_ReturnsAnalysisExitCode(t *testing.T) {
	tmp := t.TempDir()
	graphPath := filepath.Join(tmp, "buggy.json")
	if err := os.WriteFile(graphPath, []byte(`{
        "entry_node_id": "a",
        "nodes": [
            {"id": "a", "name": "a", "type": "loop", "config": {}}
        ],
        "edges": [{"from": "a", "to": "a"}]
    }`), 0o644); err != nil {
		t.Fatalf("write graph: %v", err)
	}

	// Pipe stdout into a buffer so the report doesn't leak into the
	// test output. SetOut on the root reaches every subcommand via
	// cobra's default writer propagation.
	root := NewRootCmd()
	silenceErrors(root)
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"analyze", "--input", graphPath, "--format", "json", "--output", "json"})

	code := runWithSilencedRoot(root)
	if code != 2 {
		t.Errorf("Run() exit code: got %d, want 2 (Critical); stderr=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "loop_guard") {
		t.Errorf("expected report on captured stdout to mention loop_guard; got: %s", out.String())
	}
}

// TestRun_CleanExitsZero asserts the inverse: a graph with no
// findings produces exit code 0 and a report goes to the captured
// writer.
func TestRun_CleanExitsZero(t *testing.T) {
	tmp := t.TempDir()
	graphPath := filepath.Join(tmp, "clean.json")
	if err := os.WriteFile(graphPath, []byte(`{
        "entry_node_id": "a",
        "nodes": [{"id": "a", "name": "a", "type": "llm", "config": {}}],
        "edges": []
    }`), 0o644); err != nil {
		t.Fatalf("write graph: %v", err)
	}
	root := NewRootCmd()
	silenceErrors(root)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"analyze", "--input", graphPath, "--format", "json", "--output", "json"})
	if code := runWithSilencedRoot(root); code != 0 {
		t.Errorf("Run() exit code: got %d, want 0", code)
	}
	if out.Len() == 0 {
		t.Error("expected captured stdout to receive the report; got empty buffer")
	}
}

// runWithSilencedRoot mirrors cli.Run's core logic against a
// pre-built root. The test helper exists so we can drive a root we
// configured (SetOut/SetArgs) and still get back the exit code, since
// the public Run() builds its own root.
func runWithSilencedRoot(root interface {
	Execute() error
}) int {
	if err := root.Execute(); err != nil {
		if ec, ok := err.(*exitCodeError); ok {
			return ec.code
		}
		return 1
	}
	return 0
}

// TestRun_AmbiguousADKRootNoCritical is the end-to-end half of
// Slice E #1 + Slice G #2: an ambiguous-root ADK-Go file flows
// through the parser, reachability rule, and orchestrator without
// emitting the spurious `entry node is not set` Critical that the
// pre-fix swap surfaced. The parser-only test
// (TestADKGoParser_AmbiguousRootsNoSpuriousCritical) covers half;
// this one closes the loop.
func TestRun_AmbiguousADKRootNoCritical(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "agents.go")
	if err := os.WriteFile(src, []byte(`package agents

func NewA() agent.Agent {
	a, _ := llmagent.New(llmagent.Config{Name: "agent_a", Model: "gpt-4o"})
	return a
}

func NewB() agent.Agent {
	a, _ := llmagent.New(llmagent.Config{Name: "agent_b", Model: "gpt-4o"})
	return a
}
`), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	// Call Run() directly to exercise the public contract, not a
	// local mirror (Slice G #3). Output goes to os.Stdout; the test
	// only asserts the exit code which should be 0 (no Critical) or
	// 1 (Warning), never the pre-fix Critical (which would be 2).
	args := []string{"analyze", "--input", src, "--format", "adk-go", "--output", "json"}
	if code := Run(args); code == 2 {
		t.Errorf("Run() returned 2 (Critical) — ambiguous-root regression: spurious 'entry node not set' or similar")
	}
}
