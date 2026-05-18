package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestRun_DemoEmitsCriticalAndExitsTwo locks in the contract advertised
// in README ("npx shingan-lint demo" should reproduce a Critical
// finding without any local setup). The bundled sample triggers
// loop_guard (Critical) + unreachable_node (Warning) so the demo
// must exit 2.
func TestRun_DemoEmitsCriticalAndExitsTwo(t *testing.T) {
	root := NewRootCmd()
	silenceErrors(root)
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"demo"})

	code := runWithSilencedRoot(root)
	if code != 2 {
		t.Errorf("demo exit code: got %d, want 2 (Critical); stderr=%q", code, errBuf.String())
	}
	report := out.String()
	for _, want := range []string{"loop_guard", "unreachable_node"} {
		if !strings.Contains(report, want) {
			t.Errorf("demo report missing %q; got:\n%s", want, report)
		}
	}
	if !strings.Contains(errBuf.String(), "exit code 2") {
		t.Errorf("demo stderr should restate the exit code; got:\n%s", errBuf.String())
	}
}

// TestRun_NoArgsPrintsGuidedBanner asserts the zero-arg root prints a
// short banner pointing at `demo`, rather than cobra's default
// multi-screen usage dump.
func TestRun_NoArgsPrintsGuidedBanner(t *testing.T) {
	root := NewRootCmd()
	silenceErrors(root)
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs([]string{})

	if code := runWithSilencedRoot(root); code != 0 {
		t.Errorf("no-arg root exit code: got %d, want 0; stderr=%q", code, errBuf.String())
	}
	// The banner is intentionally short. SetOut captures cobra's
	// OutOrStderr fallback because cobra routes Run-side prints
	// through SetOut when present.
	combined := out.String() + errBuf.String()
	for _, want := range []string{"shingan demo", "shingan analyze --input"} {
		if !strings.Contains(combined, want) {
			t.Errorf("no-arg banner missing %q; got:\n%s", want, combined)
		}
	}
	// Cobra's default Usage starts with "Usage:". The banner must NOT
	// duplicate that wall of text.
	if strings.Contains(combined, "Usage:\n  shingan [command]") {
		t.Errorf("no-arg path printed full cobra usage dump; expected short banner. got:\n%s", combined)
	}
}
