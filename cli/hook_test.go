package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// runHook must never block (return an error) on input that isn't a real
// workflow edit — otherwise an agent gets stuck on unrelated file writes.
func TestRunHook_NonBlocking(t *testing.T) {
	cases := []struct{ name, stdin string }{
		{"empty_filepath", `{"tool_input":{"file_path":""}}`},
		{"non_workflow_ext", `{"tool_input":{"file_path":"/tmp/readme.md"}}`},
		{"malformed_json", `this is not json`},
		{"missing_field", `{"tool_name":"Write"}`},
		{"nonexistent_file", `{"tool_input":{"file_path":"/tmp/shingan-nope-xyz.py"}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.SetIn(strings.NewReader(c.stdin))
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			if err := runHook(cmd); err != nil {
				t.Errorf("runHook should not block, got %v", err)
			}
		})
	}
}

func TestCriticalSection(t *testing.T) {
	md := "# Shingan Report\n\n## Summary\n\n| t |\n\n## Critical\n\n| Rule | Node |\n| loop_guard | retry |\n\n## Warning\n\n| other |\n"
	got := criticalSection(md)
	if !strings.Contains(got, "## Critical") || !strings.Contains(got, "loop_guard") {
		t.Errorf("criticalSection missing critical content: %q", got)
	}
	if strings.Contains(got, "## Warning") || strings.Contains(got, "other") {
		t.Errorf("criticalSection leaked the Warning section: %q", got)
	}
	if criticalSection("# Report\n\n## Summary\nno criticals\n") != "" {
		t.Errorf("criticalSection should be empty when there is no Critical block")
	}
}

func TestInstallClaudeHook(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-existing settings: an unrelated key plus an existing Bash hook.
	os.WriteFile(path, []byte(`{"model":"opus","hooks":{"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo x"}]}]}}`), 0o644)

	added, err := installClaudeHook(path, "shingan")
	if err != nil || !added {
		t.Fatalf("first install: added=%v err=%v, want true/nil", added, err)
	}

	var s map[string]any
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("result not valid JSON: %v", err)
	}
	if s["model"] != "opus" {
		t.Errorf("unrelated key 'model' not preserved: %v", s["model"])
	}
	post := s["hooks"].(map[string]any)["PostToolUse"].([]any)
	if len(post) != 2 {
		t.Fatalf("want 2 PostToolUse entries (existing Bash + shingan), got %d", len(post))
	}

	// Idempotent: a second install adds nothing.
	added2, err := installClaudeHook(path, "shingan")
	if err != nil || added2 {
		t.Fatalf("second install: added=%v err=%v, want false/nil (idempotent)", added2, err)
	}
	raw2, _ := os.ReadFile(path)
	json.Unmarshal(raw2, &s)
	if got := len(s["hooks"].(map[string]any)["PostToolUse"].([]any)); got != 2 {
		t.Errorf("idempotency broken: PostToolUse grew to %d entries", got)
	}
}

// Installing into a fresh location creates the file and a well-formed hook.
func TestInstallClaudeHook_FreshFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", ".claude", "settings.json")
	added, err := installClaudeHook(path, "/abs/shingan")
	if err != nil || !added {
		t.Fatalf("fresh install: added=%v err=%v", added, err)
	}
	raw, _ := os.ReadFile(path)
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("fresh file not valid JSON: %v", err)
	}
	post := s["hooks"].(map[string]any)["PostToolUse"].([]any)
	entry := post[0].(map[string]any)
	if entry["matcher"] != "Edit|Write" {
		t.Errorf("matcher = %v, want Edit|Write", entry["matcher"])
	}
	inner := entry["hooks"].([]any)[0].(map[string]any)
	if inner["command"] != "/abs/shingan hook run" {
		t.Errorf("command = %v, want '/abs/shingan hook run'", inner["command"])
	}
}
