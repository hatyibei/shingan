package rules_test

import (
	"fmt"
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

// ─── Case 11: 大規模グラフ (100 nodes, multiple sources/sinks) ───────────────
//
// Topology:
//   src0..src4 (RAG sources) → intermediate0..intermediate9 → sink0..sink4 (api sinks)
//   src5 → human_gate → sink5 (safe, no finding expected)
//
// Expected findings: 5 sources × 5 sinks = 25 Warning findings (each source
// reachable from each sink via the shared intermediates).

func TestPIILeakScanner_LargeGraph_MultipleSources(t *testing.T) {
	b := testutil.NewBuilder()

	// 5 RAG source nodes
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("src%d", i)
		b.AddNodeWithConfig(id, domain.NodeTypeTool, map[string]any{"category": "rag"})
	}
	// 10 intermediate LLM nodes
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("mid%d", i)
		b.AddNode(id, domain.NodeTypeLLM)
	}
	// 5 external api sink nodes
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("sink%d", i)
		b.AddNodeWithConfig(id, domain.NodeTypeTool, map[string]any{"category": "api"})
	}
	// Safe path: src5 → human_gate → sink5
	b.AddNodeWithConfig("src5", domain.NodeTypeTool, map[string]any{"category": "rag"})
	b.AddNode("human_gate", domain.NodeTypeHuman)
	b.AddNodeWithConfig("sink5", domain.NodeTypeTool, map[string]any{"category": "api"})

	// All sources connect to all intermediates
	for i := 0; i < 5; i++ {
		for j := 0; j < 10; j++ {
			b.AddEdge(fmt.Sprintf("src%d", i), fmt.Sprintf("mid%d", j))
		}
	}
	// All intermediates connect to all sinks
	for i := 0; i < 10; i++ {
		for j := 0; j < 5; j++ {
			b.AddEdge(fmt.Sprintf("mid%d", i), fmt.Sprintf("sink%d", j))
		}
	}
	// Safe path edges
	b.AddEdge("src5", "human_gate")
	b.AddEdge("human_gate", "sink5")

	b.Entry("src0")
	g := buildPII(t, b)

	findings := rules.NewPIILeakScanner().Analyze(g)

	// Each of the 5 sinks can be reached (in reverse) from each of the 5 RAG sources.
	// sink5 is blocked by human_gate so no finding for src5→sink5.
	// Expected: 5 sinks × 5 sources = 25 Warning findings.
	if len(findings) != 25 {
		t.Fatalf("expected 25 findings, got %d: %+v", len(findings), findings)
	}
	for _, f := range findings {
		if f.Severity != domain.Warning {
			t.Errorf("finding %q: Severity = %v, want Warning", f.NodeID, f.Severity)
		}
		if f.RuleName != "pii_leak_scanner" {
			t.Errorf("finding RuleName = %q, want %q", f.RuleName, "pii_leak_scanner")
		}
	}
}

// ─── Case 12: Human gate が1つだけでも正しくブロック ───────────────────────
//
// Topology: rag → human → intermediate → sink
// The Human gate is early in the chain, but everything downstream is safe.
// Expected: 0 findings.

func TestPIILeakScanner_SingleHumanGate_BlocksAllDownstream(t *testing.T) {
	g := buildPII(t, testutil.NewBuilder().
		AddNodeWithConfig("rag", domain.NodeTypeTool, map[string]any{"category": "rag"}).
		AddNode("human", domain.NodeTypeHuman).
		AddNode("mid1", domain.NodeTypeLLM).
		AddNode("mid2", domain.NodeTypeLLM).
		AddNodeWithConfig("api", domain.NodeTypeTool, map[string]any{"category": "api"}).
		AddNodeWithConfig("mcp", domain.NodeTypeTool, map[string]any{"category": "mcp"}).
		AddEdge("rag", "human").
		AddEdge("human", "mid1").
		AddEdge("mid1", "mid2").
		AddEdge("mid2", "api").
		AddEdge("mid2", "mcp").
		Entry("rag"))

	findings := rules.NewPIILeakScanner().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (single Human gate blocks all paths), got %d: %+v", len(findings), findings)
	}
}

// ─── Name() ──────────────────────────────────────────────────────────────────

func TestPIILeakScanner_Name(t *testing.T) {
	p := rules.NewPIILeakScanner()
	if got := p.Name(); got != "pii_leak_scanner" {
		t.Errorf("Name() = %q, want %q", got, "pii_leak_scanner")
	}
}

// TestPIILeakScanner_Confidence_RAGSource verifies RAG source findings have Confidence == 0.6.
func TestPIILeakScanner_Confidence_RAGSource(t *testing.T) {
	g := buildPII(t, testutil.NewBuilder().
		AddNodeWithConfig("rag", domain.NodeTypeTool, map[string]any{"category": "rag"}).
		AddNodeWithConfig("api_sink", domain.NodeTypeTool, map[string]any{"category": "api"}).
		AddEdge("rag", "api_sink").
		Entry("rag"))

	findings := rules.NewPIILeakScanner().Analyze(g)
	if len(findings) == 0 {
		t.Fatal("expected ≥1 finding, got 0")
	}
	for _, f := range findings {
		if f.Confidence != 0.6 {
			t.Errorf("RAG source Confidence = %.2f, want 0.6", f.Confidence)
		}
	}
}

// TestPIILeakScanner_Confidence_HintSource verifies name-hint source findings have Confidence == 0.3.
func TestPIILeakScanner_Confidence_HintSource(t *testing.T) {
	g := buildPII(t, testutil.NewBuilder().
		AddNodeWithConfig("user_data", domain.NodeTypeTool, map[string]any{"category": "code"}).
		AddNodeWithConfig("api_sink", domain.NodeTypeTool, map[string]any{"category": "api"}).
		AddEdge("user_data", "api_sink").
		Entry("user_data"))
	// The node name "user_data" contains "user" — a PII hint keyword.
	g.Nodes["user_data"].Name = "user_data"

	findings := rules.NewPIILeakScanner().Analyze(g)
	if len(findings) == 0 {
		t.Fatal("expected ≥1 finding, got 0")
	}
	for _, f := range findings {
		if f.Confidence != 0.3 {
			t.Errorf("hint source Confidence = %.2f, want 0.3", f.Confidence)
		}
	}
}
