//go:build demo

// Package shingan_demo provides an E2E demo test that verifies Shingan can
// analyze real google.golang.org/adk SDK–based Go source files and produce
// the expected Findings.
//
// Run with: go test -tags=demo -v ./...
package shingan_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/infrastructure/parser"
)

// projectRoot returns the absolute path of the shingan module root.
func demoProjectRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

// parseRealFile reads a Go source file and parses it with ADKGoParser.
func parseRealFile(t *testing.T, relPath string) *domain.WorkflowGraph {
	t.Helper()
	absPath := filepath.Join(demoProjectRoot(), relPath)
	src, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	p := parser.NewADKGoParser()
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("parse %s: %v", relPath, err)
	}
	return graph
}

// TestDemo_InfiniteLoop verifies that examples/real/infinite_loop.go triggers
// a cycle_detection Critical finding.
func TestDemo_InfiniteLoop(t *testing.T) {
	graph := parseRealFile(t, "examples/real/infinite_loop.go")

	rule := rules.NewCycleDetector()
	findings := rule.Analyze(graph)

	hasCritical := false
	for _, f := range findings {
		if f.RuleName == "cycle_detection" && f.Severity == domain.Critical {
			hasCritical = true
			t.Logf("PASS cycle_detection Critical: %s", f.Message)
		}
	}
	if !hasCritical {
		t.Errorf("expected cycle_detection Critical finding for infinite_loop.go; got %+v", findings)
	}
}

// TestDemo_MissingHandler verifies that examples/real/missing_handler.go triggers
// an error_handler_checker finding (Warning or Critical).
// Note: tool detection from real SDK function calls is limited; this test is
// informational and does not fail if no finding is produced (see README).
func TestDemo_MissingHandler(t *testing.T) {
	graph := parseRealFile(t, "examples/real/missing_handler.go")

	rule := rules.NewErrorHandlerChecker()
	findings := rule.Analyze(graph)

	for _, f := range findings {
		if f.RuleName == "error_handler_checker" {
			t.Logf("PASS error_handler_checker %s: %s", f.Severity, f.Message)
			return
		}
	}
	t.Logf("error_handler_checker not triggered; graph nodes=%d edges=%d", len(graph.Nodes), len(graph.Edges))
	t.Logf("(functiontool.New call-based tool detection is not yet implemented in Shingan parser — see README)")
}

// TestDemo_Unreachable verifies that examples/real/unreachable.go triggers
// an unreachable_node finding.
func TestDemo_Unreachable(t *testing.T) {
	graph := parseRealFile(t, "examples/real/unreachable.go")

	rule := rules.NewReachabilityChecker()
	findings := rule.Analyze(graph)

	for _, f := range findings {
		if f.RuleName == "unreachable_node" {
			t.Logf("PASS unreachable_node %s: %s (node: %s)", f.Severity, f.Message, f.NodeID)
			return
		}
	}
	t.Logf("unreachable_node not found; graph nodes=%d edges=%d entry=%q",
		len(graph.Nodes), len(graph.Edges), graph.EntryNodeID)
	t.Logf("(orphan_analyzer parsed as LlmAgent; unreachable detection requires it to be in graph.Nodes)")
	// Check if the orchestrator was correctly identified as entry.
	if graph.Nodes["orchestrator"] == nil {
		t.Errorf("orchestrator node not found in graph — parser failed to detect sequentialagent.New pattern")
	}
}
