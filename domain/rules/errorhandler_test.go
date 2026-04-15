package rules

import (
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/testutil"
)

// helper: build a graph and fail the test on error.
func mustBuild(t *testing.T, b *testutil.Builder) *domain.WorkflowGraph {
	t.Helper()
	g, err := b.Build()
	if err != nil {
		t.Fatalf("testutil.Builder.Build() failed: %v", err)
	}
	return g
}

// TestErrorHandlerChecker_BrowserNoHandler checks that a browser Tool node whose
// outgoing edges are all unconditional produces a Critical finding.
func TestErrorHandlerChecker_BrowserNoHandler(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("browser1", domain.NodeTypeTool, map[string]any{"category": "browser"}).
		AddNode("output1", domain.NodeTypeOutput).
		AddEdge("browser1", "output1").
		Entry("browser1"),
	)

	checker := NewErrorHandlerChecker()
	findings := checker.Analyze(g)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.NodeID != "browser1" {
		t.Errorf("expected NodeID=browser1, got %q", f.NodeID)
	}
	if f.Severity != domain.Critical {
		t.Errorf("expected Critical severity, got %v", f.Severity)
	}
	if f.RuleName != checker.Name() {
		t.Errorf("expected RuleName=%q, got %q", checker.Name(), f.RuleName)
	}
	if f.Suggestion == "" {
		t.Error("Suggestion must not be empty")
	}
}

// TestErrorHandlerChecker_BrowserWithHandler checks that a browser Tool node that
// has at least one conditional outgoing edge produces no finding.
func TestErrorHandlerChecker_BrowserWithHandler(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("browser1", domain.NodeTypeTool, map[string]any{"category": "browser"}).
		AddNode("success", domain.NodeTypeOutput).
		AddNode("failure", domain.NodeTypeOutput).
		AddConditionalEdge("browser1", "success", "ok").
		AddConditionalEdge("browser1", "failure", "error").
		Entry("browser1"),
	)

	checker := NewErrorHandlerChecker()
	findings := checker.Analyze(g)

	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// TestErrorHandlerChecker_APINoHandler checks that an api Tool node (default category)
// without conditional edges produces a Warning finding.
func TestErrorHandlerChecker_APINoHandler(t *testing.T) {
	// "api" category explicitly set
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("api1", domain.NodeTypeTool, map[string]any{"category": "api"}).
		AddNode("next", domain.NodeTypeLLM).
		AddEdge("api1", "next").
		Entry("api1"),
	)

	checker := NewErrorHandlerChecker()
	findings := checker.Analyze(g)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != domain.Warning {
		t.Errorf("expected Warning severity, got %v", findings[0].Severity)
	}
}

// TestErrorHandlerChecker_DefaultCategoryIsAPI checks that a Tool node with no
// "category" config key is treated as "api" and produces a Warning.
func TestErrorHandlerChecker_DefaultCategoryIsAPI(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNode("tool1", domain.NodeTypeTool).
		AddNode("next", domain.NodeTypeOutput).
		AddEdge("tool1", "next").
		Entry("tool1"),
	)

	checker := NewErrorHandlerChecker()
	findings := checker.Analyze(g)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != domain.Warning {
		t.Errorf("expected Warning severity for default category, got %v", findings[0].Severity)
	}
}

// TestErrorHandlerChecker_MCPNoHandler checks that an mcp Tool node without
// conditional edges produces a Warning finding.
func TestErrorHandlerChecker_MCPNoHandler(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("mcp1", domain.NodeTypeTool, map[string]any{"category": "mcp"}).
		AddNode("next", domain.NodeTypeOutput).
		AddEdge("mcp1", "next").
		Entry("mcp1"),
	)

	checker := NewErrorHandlerChecker()
	findings := checker.Analyze(g)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != domain.Warning {
		t.Errorf("expected Warning severity for mcp, got %v", findings[0].Severity)
	}
}

// TestErrorHandlerChecker_CodeNoHandler checks that a code Tool node without
// conditional edges produces an Info finding.
func TestErrorHandlerChecker_CodeNoHandler(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("code1", domain.NodeTypeTool, map[string]any{"category": "code"}).
		AddNode("next", domain.NodeTypeOutput).
		AddEdge("code1", "next").
		Entry("code1"),
	)

	checker := NewErrorHandlerChecker()
	findings := checker.Analyze(g)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != domain.Info {
		t.Errorf("expected Info severity for code, got %v", findings[0].Severity)
	}
}

// TestErrorHandlerChecker_LLMNodeIgnored checks that LLM nodes are not flagged
// even when they have only unconditional outgoing edges.
func TestErrorHandlerChecker_LLMNodeIgnored(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNode("llm1", domain.NodeTypeLLM).
		AddNode("next", domain.NodeTypeOutput).
		AddEdge("llm1", "next").
		Entry("llm1"),
	)

	checker := NewErrorHandlerChecker()
	findings := checker.Analyze(g)

	if len(findings) != 0 {
		t.Errorf("expected 0 findings for LLM node, got %d: %+v", len(findings), findings)
	}
}

// TestErrorHandlerChecker_TerminalToolNode checks that a Tool node with no outgoing
// edges at all (terminal) is not flagged.
func TestErrorHandlerChecker_TerminalToolNode(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("browser1", domain.NodeTypeTool, map[string]any{"category": "browser"}).
		Entry("browser1"),
	)

	checker := NewErrorHandlerChecker()
	findings := checker.Analyze(g)

	if len(findings) != 0 {
		t.Errorf("expected 0 findings for terminal Tool node, got %d: %+v", len(findings), findings)
	}
}

// TestErrorHandlerChecker_MixedEdges checks that a Tool node with both conditional
// and unconditional edges is not flagged (the conditional edge is sufficient).
func TestErrorHandlerChecker_MixedEdges(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNodeWithConfig("api1", domain.NodeTypeTool, map[string]any{"category": "api"}).
		AddNode("always", domain.NodeTypeLLM).
		AddNode("onerror", domain.NodeTypeOutput).
		AddEdge("api1", "always").
		AddConditionalEdge("api1", "onerror", "error").
		Entry("api1"),
	)

	checker := NewErrorHandlerChecker()
	findings := checker.Analyze(g)

	if len(findings) != 0 {
		t.Errorf("expected 0 findings for mixed-edge node, got %d: %+v", len(findings), findings)
	}
}

// TestErrorHandlerChecker_NilGraph checks that a nil graph is handled gracefully.
func TestErrorHandlerChecker_NilGraph(t *testing.T) {
	checker := NewErrorHandlerChecker()
	findings := checker.Analyze(nil)
	if findings != nil {
		t.Errorf("expected nil findings for nil graph, got %+v", findings)
	}
}

// TestErrorHandlerChecker_Name verifies the rule name constant.
func TestErrorHandlerChecker_Name(t *testing.T) {
	checker := NewErrorHandlerChecker()
	if checker.Name() != "error_handler_checker" {
		t.Errorf("unexpected Name(): %q", checker.Name())
	}
}

// ─── LLM-node Check 2 tests (ADK-Go pattern: LLM→Tool edges) ────────────────

// TestErrorHandlerChecker_LLMWithTerminalTool_Warning checks that an LLM node
// whose only outgoing edge leads to a terminal Tool node (no outgoing edges from tool)
// produces a Warning — the common ADK-Go LLM→Tool pattern.
func TestErrorHandlerChecker_LLMWithTerminalTool_Warning(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNode("planner", domain.NodeTypeLLM).
		AddNodeWithConfig("browser_search", domain.NodeTypeTool, map[string]any{"category": "browser"}).
		AddEdge("planner", "browser_search"). // terminal tool, no outgoing edges
		Entry("planner"),
	)

	checker := NewErrorHandlerChecker()
	findings := checker.Analyze(g)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.NodeID != "planner" {
		t.Errorf("expected NodeID=planner, got %q", f.NodeID)
	}
	if f.Severity != domain.Warning {
		t.Errorf("expected Warning severity, got %v", f.Severity)
	}
	if f.RuleName != checker.Name() {
		t.Errorf("expected RuleName=%q, got %q", checker.Name(), f.RuleName)
	}
}

// TestErrorHandlerChecker_LLMWithToolHavingConditionalEdge_NoFinding checks that
// when the Tool node downstream from an LLM node has conditional outgoing edges,
// no finding is reported (error handling is present).
func TestErrorHandlerChecker_LLMWithToolHavingConditionalEdge_NoFinding(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNode("planner", domain.NodeTypeLLM).
		AddNodeWithConfig("api_tool", domain.NodeTypeTool, map[string]any{"category": "api"}).
		AddNode("success", domain.NodeTypeOutput).
		AddNode("failure", domain.NodeTypeOutput).
		AddEdge("planner", "api_tool").
		AddConditionalEdge("api_tool", "success", "ok").
		AddConditionalEdge("api_tool", "failure", "error").
		Entry("planner"),
	)

	checker := NewErrorHandlerChecker()
	findings := checker.Analyze(g)

	if len(findings) != 0 {
		t.Errorf("expected 0 findings (tool has error handling), got %d: %+v", len(findings), findings)
	}
}

// TestErrorHandlerChecker_LLMWithConditionalEdgeToTool_NoFinding checks that an LLM
// node whose outgoing edges include conditional branches (regardless of tool presence)
// does not produce a finding.
func TestErrorHandlerChecker_LLMWithConditionalEdgeToTool_NoFinding(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNode("planner", domain.NodeTypeLLM).
		AddNodeWithConfig("browser_search", domain.NodeTypeTool, map[string]any{"category": "browser"}).
		AddNode("fallback", domain.NodeTypeOutput).
		AddConditionalEdge("planner", "browser_search", "proceed").
		AddConditionalEdge("planner", "fallback", "error").
		Entry("planner"),
	)

	checker := NewErrorHandlerChecker()
	findings := checker.Analyze(g)

	if len(findings) != 0 {
		t.Errorf("expected 0 findings (LLM has conditional edges), got %d: %+v", len(findings), findings)
	}
}

// TestErrorHandlerChecker_LLMWithoutTools_NoFinding verifies that an LLM node
// whose edges all lead to other LLM nodes (no Tool targets) is not flagged.
func TestErrorHandlerChecker_LLMWithoutTools_NoFinding(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNode("planner", domain.NodeTypeLLM).
		AddNode("summarizer", domain.NodeTypeLLM).
		AddEdge("planner", "summarizer").
		Entry("planner"),
	)

	checker := NewErrorHandlerChecker()
	findings := checker.Analyze(g)

	if len(findings) != 0 {
		t.Errorf("expected 0 findings (no tool targets), got %d: %+v", len(findings), findings)
	}
}

// TestErrorHandlerChecker_Confidence verifies all findings have Confidence == 0.8.
func TestErrorHandlerChecker_Confidence(t *testing.T) {
	g := mustBuild(t, testutil.NewBuilder().
		AddNode("llm", domain.NodeTypeLLM).
		AddNode("browser_tool", domain.NodeTypeTool).
		AddEdge("llm", "browser_tool").
		AddEdge("browser_tool", "llm"). // back edge so tool has outgoing edge
		Entry("llm"))

	// Override category to "browser" so tool has category.
	g.Nodes["browser_tool"].Config = map[string]any{"category": "browser"}

	checker := NewErrorHandlerChecker()
	findings := checker.Analyze(g)
	if len(findings) == 0 {
		t.Fatal("expected ≥1 finding, got 0")
	}
	for _, f := range findings {
		if f.Confidence != 0.8 {
			t.Errorf("Confidence = %.2f, want 0.8", f.Confidence)
		}
	}
}
