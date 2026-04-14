package rules_test

import (
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/domain/testutil"
)

// helper: build a graph and fail the test on error (local to this file).
// NOTE: mustBuild is also defined in errorhandler_test.go (package rules),
// so we reuse that name in the _test package here — no conflict.

func buildPII(t *testing.T, b *testutil.Builder) *domain.WorkflowGraph {
	t.Helper()
	g, err := b.Build()
	if err != nil {
		t.Fatalf("testutil.Builder.Build() failed: %v", err)
	}
	return g
}

// ─── Case 1: RAG → API 直接パス、Human無し → Warning ──────────────────────────

func TestPIILeakScanner_RAGtoAPI_NoHuman_Warning(t *testing.T) {
	g := buildPII(t, testutil.NewBuilder().
		AddNodeWithConfig("rag", domain.NodeTypeTool, map[string]any{"category": "rag"}).
		AddNodeWithConfig("api", domain.NodeTypeTool, map[string]any{"category": "api"}).
		AddEdge("rag", "api").
		Entry("rag"))

	findings := rules.NewPIILeakScanner().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Warning {
		t.Errorf("Severity = %v, want Warning", f.Severity)
	}
	if f.NodeID != "api" {
		t.Errorf("NodeID = %q, want %q", f.NodeID, "api")
	}
	if f.RuleName != "pii_leak_scanner" {
		t.Errorf("RuleName = %q, want %q", f.RuleName, "pii_leak_scanner")
	}
}

// ─── Case 2: RAG → Human → API → 検出なし ────────────────────────────────────

func TestPIILeakScanner_RAGtoHumanToAPI_NoFinding(t *testing.T) {
	g := buildPII(t, testutil.NewBuilder().
		AddNodeWithConfig("rag", domain.NodeTypeTool, map[string]any{"category": "rag"}).
		AddNode("human", domain.NodeTypeHuman).
		AddNodeWithConfig("api", domain.NodeTypeTool, map[string]any{"category": "api"}).
		AddEdge("rag", "human").
		AddEdge("human", "api").
		Entry("rag"))

	findings := rules.NewPIILeakScanner().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (Human gate present), got %d: %+v", len(findings), findings)
	}
}

// ─── Case 3: RAG → Browser → Warning ─────────────────────────────────────────

func TestPIILeakScanner_RAGtoBrowser_Warning(t *testing.T) {
	g := buildPII(t, testutil.NewBuilder().
		AddNodeWithConfig("rag", domain.NodeTypeTool, map[string]any{"category": "rag"}).
		AddNodeWithConfig("browser", domain.NodeTypeTool, map[string]any{"category": "browser"}).
		AddEdge("rag", "browser").
		Entry("rag"))

	findings := rules.NewPIILeakScanner().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Warning {
		t.Errorf("Severity = %v, want Warning", findings[0].Severity)
	}
	if findings[0].NodeID != "browser" {
		t.Errorf("NodeID = %q, want %q", findings[0].NodeID, "browser")
	}
}

// ─── Case 4: RAG → MCP → Warning ─────────────────────────────────────────────

func TestPIILeakScanner_RAGtoMCP_Warning(t *testing.T) {
	g := buildPII(t, testutil.NewBuilder().
		AddNodeWithConfig("rag", domain.NodeTypeTool, map[string]any{"category": "rag"}).
		AddNodeWithConfig("mcp", domain.NodeTypeTool, map[string]any{"category": "mcp"}).
		AddEdge("rag", "mcp").
		Entry("rag"))

	findings := rules.NewPIILeakScanner().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Warning {
		t.Errorf("Severity = %v, want Warning", findings[0].Severity)
	}
}

// ─── Case 5: name="pii_processor" → API → Info (ヒューリスティック) ───────────

func TestPIILeakScanner_PIINameHint_API_Info(t *testing.T) {
	g := buildPII(t, testutil.NewBuilder().
		AddNode("pii_processor", domain.NodeTypeLLM).
		AddNodeWithConfig("api", domain.NodeTypeTool, map[string]any{"category": "api"}).
		AddEdge("pii_processor", "api").
		Entry("pii_processor"))

	findings := rules.NewPIILeakScanner().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Info {
		t.Errorf("Severity = %v, want Info", findings[0].Severity)
	}
	if findings[0].NodeID != "api" {
		t.Errorf("NodeID = %q, want %q", findings[0].NodeID, "api")
	}
}

// ─── Case 6: RAG → LLM → API (中間にLLMノード) → Warning ────────────────────

func TestPIILeakScanner_RAGthroughLLMtoAPI_Warning(t *testing.T) {
	// LLM is NOT a Human gate — PII still flows through.
	g := buildPII(t, testutil.NewBuilder().
		AddNodeWithConfig("rag", domain.NodeTypeTool, map[string]any{"category": "rag"}).
		AddNode("llm", domain.NodeTypeLLM).
		AddNodeWithConfig("api", domain.NodeTypeTool, map[string]any{"category": "api"}).
		AddEdge("rag", "llm").
		AddEdge("llm", "api").
		Entry("rag"))

	findings := rules.NewPIILeakScanner().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Warning {
		t.Errorf("Severity = %v, want Warning", findings[0].Severity)
	}
	if findings[0].NodeID != "api" {
		t.Errorf("NodeID = %q, want %q", findings[0].NodeID, "api")
	}
}

// ─── Case 7: ノード名 "user_profile" → API → Info ───────────────────────────

func TestPIILeakScanner_UserProfileName_API_Info(t *testing.T) {
	g := buildPII(t, testutil.NewBuilder().
		AddNode("user_profile", domain.NodeTypeLLM).
		AddNodeWithConfig("external_api", domain.NodeTypeTool, map[string]any{"category": "api"}).
		AddEdge("user_profile", "external_api").
		Entry("user_profile"))

	findings := rules.NewPIILeakScanner().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Info {
		t.Errorf("Severity = %v, want Info", findings[0].Severity)
	}
}

// ─── Case 8: 複数RAG + 複数external tool の組み合わせ ────────────────────────

func TestPIILeakScanner_MultipleRAGAndSinks(t *testing.T) {
	// rag1 → api1 (Warning)
	// rag2 → human → api2 (safe, no finding)
	// rag1 → mcp1 (Warning)
	g := buildPII(t, testutil.NewBuilder().
		AddNodeWithConfig("rag1", domain.NodeTypeTool, map[string]any{"category": "rag"}).
		AddNodeWithConfig("rag2", domain.NodeTypeTool, map[string]any{"category": "rag"}).
		AddNode("human", domain.NodeTypeHuman).
		AddNodeWithConfig("api1", domain.NodeTypeTool, map[string]any{"category": "api"}).
		AddNodeWithConfig("api2", domain.NodeTypeTool, map[string]any{"category": "api"}).
		AddNodeWithConfig("mcp1", domain.NodeTypeTool, map[string]any{"category": "mcp"}).
		AddEdge("rag1", "api1").
		AddEdge("rag1", "mcp1").
		AddEdge("rag2", "human").
		AddEdge("human", "api2").
		Entry("rag1"))

	findings := rules.NewPIILeakScanner().Analyze(g)
	// Expect: rag1→api1 (Warning), rag1→mcp1 (Warning). rag2→human→api2 is safe.
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d: %+v", len(findings), findings)
	}
	for _, f := range findings {
		if f.Severity != domain.Warning {
			t.Errorf("finding %q: Severity = %v, want Warning", f.NodeID, f.Severity)
		}
	}
}

// ─── Case 9: has_pii=true フラグ付きノード → API → Warning ──────────────────

func TestPIILeakScanner_HasPIIFlag_Warning(t *testing.T) {
	g := buildPII(t, testutil.NewBuilder().
		AddNodeWithConfig("db_tool", domain.NodeTypeTool, map[string]any{
			"category": "database",
			"has_pii":  true,
		}).
		AddNodeWithConfig("api", domain.NodeTypeTool, map[string]any{"category": "api"}).
		AddEdge("db_tool", "api").
		Entry("db_tool"))

	findings := rules.NewPIILeakScanner().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Warning {
		t.Errorf("Severity = %v, want Warning", findings[0].Severity)
	}
}

// ─── Case 10: nil グラフ → パニックしない ────────────────────────────────────

func TestPIILeakScanner_NilGraph(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Analyze(nil) panicked: %v", r)
		}
	}()
	findings := rules.NewPIILeakScanner().Analyze(nil)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for nil graph, got %d", len(findings))
	}
}

// ─── Name() ──────────────────────────────────────────────────────────────────

func TestPIILeakScanner_Name(t *testing.T) {
	p := rules.NewPIILeakScanner()
	if got := p.Name(); got != "pii_leak_scanner" {
		t.Errorf("Name() = %q, want %q", got, "pii_leak_scanner")
	}
}
