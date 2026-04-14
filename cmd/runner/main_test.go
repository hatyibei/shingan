package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hatyibei/shingan/domain"
)

// projectRoot returns the root of the Shingan module for test fixtures.
func projectRoot(t *testing.T) string {
	t.Helper()
	// This test file lives at cmd/runner/main_test.go.
	// Walk up two directories to reach the project root.
	dir, err := filepath.Abs("../../")
	if err != nil {
		t.Fatalf("resolve project root: %v", err)
	}
	return dir
}

// TestKnownSamples verifies that all sample names are registered.
func TestKnownSamples(t *testing.T) {
	t.Parallel()
	expected := []string{"simple", "infinite_loop_bounded", "infinite_loop_unbounded"}
	for _, name := range expected {
		if _, ok := knownSamples[name]; !ok {
			t.Errorf("sample %q not found in knownSamples", name)
		}
	}
}

// TestAnalyzeFile_Simple verifies that simple_agent.go produces no Critical findings.
func TestAnalyzeFile_Simple(t *testing.T) {
	t.Parallel()
	root := projectRoot(t)
	srcPath := filepath.Join(root, "examples", "runtime", "simple_agent.go")

	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		t.Skipf("source file not found: %s", srcPath)
	}

	findings, err := analyzeFile(srcPath)
	if err != nil {
		t.Fatalf("analyzeFile: %v", err)
	}
	for _, f := range findings {
		if f.Severity == domain.Critical {
			t.Errorf("unexpected Critical finding in simple_agent.go: %s — %s", f.RuleName, f.Message)
		}
	}
}

// TestAnalyzeFile_BoundedLoop verifies that infinite_loop_bounded.go produces no Critical findings.
func TestAnalyzeFile_BoundedLoop(t *testing.T) {
	t.Parallel()
	root := projectRoot(t)
	srcPath := filepath.Join(root, "examples", "runtime", "infinite_loop_bounded.go")

	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		t.Skipf("source file not found: %s", srcPath)
	}

	findings, err := analyzeFile(srcPath)
	if err != nil {
		t.Fatalf("analyzeFile: %v", err)
	}
	for _, f := range findings {
		if f.Severity == domain.Critical {
			t.Errorf("unexpected Critical finding in infinite_loop_bounded.go: %s — %s", f.RuleName, f.Message)
		}
	}
}

// TestAnalyzeFile_UnboundedLoop verifies that infinite_loop_unbounded.go triggers
// a cycle_detection Critical finding (the key safety assertion for the demo).
func TestAnalyzeFile_UnboundedLoop(t *testing.T) {
	t.Parallel()
	root := projectRoot(t)
	srcPath := filepath.Join(root, "examples", "runtime", "infinite_loop_unbounded.go")

	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		t.Skipf("source file not found: %s", srcPath)
	}

	findings, err := analyzeFile(srcPath)
	if err != nil {
		t.Fatalf("analyzeFile: %v", err)
	}

	hasCritical := false
	for _, f := range findings {
		if f.RuleName == "cycle_detection" && f.Severity == domain.Critical {
			hasCritical = true
			t.Logf("PASS cycle_detection Critical: %s", f.Message)
		}
	}
	if !hasCritical {
		t.Errorf("expected cycle_detection Critical for infinite_loop_unbounded.go; got %+v", findings)
	}
}

// TestDryRun_Simple verifies that analysis on simple_agent returns no error and no Critical.
// This test uses analyzeFile directly (avoids cwd dependency of runSample in parallel tests).
func TestDryRun_Simple(t *testing.T) {
	t.Parallel()
	root := projectRoot(t)
	srcPath := filepath.Join(root, "examples", "runtime", "simple_agent.go")
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		t.Skipf("source file not found: %s", srcPath)
	}

	findings, err := analyzeFile(srcPath)
	if err != nil {
		t.Fatalf("analyzeFile: %v", err)
	}
	if hasCriticalFinding(findings) {
		t.Errorf("unexpected Critical finding for simple_agent: %+v", findings)
	}
	t.Logf("dry-run simple: %d findings (no Critical)", len(findings))
}

// TestDryRun_UnboundedLoop verifies the safe-guard: analyzeFile returns Critical, hasCriticalFinding=true.
func TestDryRun_UnboundedLoop(t *testing.T) {
	t.Parallel()
	root := projectRoot(t)
	srcPath := filepath.Join(root, "examples", "runtime", "infinite_loop_unbounded.go")
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		t.Skipf("source file not found: %s", srcPath)
	}

	findings, err := analyzeFile(srcPath)
	if err != nil {
		t.Fatalf("analyzeFile: %v", err)
	}
	if !hasCriticalFinding(findings) {
		t.Errorf("expected Critical finding for infinite_loop_unbounded (safe-guard must trigger); got %+v", findings)
	}
	t.Logf("dry-run unbounded: safe-guard would block execution (Critical detected)")
}

// TestDryRun_BoundedLoop verifies that the bounded loop passes without Critical findings.
func TestDryRun_BoundedLoop(t *testing.T) {
	t.Parallel()
	root := projectRoot(t)
	srcPath := filepath.Join(root, "examples", "runtime", "infinite_loop_bounded.go")
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		t.Skipf("source file not found: %s", srcPath)
	}

	findings, err := analyzeFile(srcPath)
	if err != nil {
		t.Fatalf("analyzeFile: %v", err)
	}
	if hasCriticalFinding(findings) {
		t.Errorf("unexpected Critical finding for infinite_loop_bounded: %+v", findings)
	}
	t.Logf("dry-run bounded: %d findings (no Critical)", len(findings))
}

// TestUnknownSample verifies that an unknown sample name returns an error.
func TestUnknownSample(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	err := runSample(ctx, "nonexistent_sample", 0, true)
	if err == nil {
		t.Error("expected error for unknown sample, got nil")
	}
}
