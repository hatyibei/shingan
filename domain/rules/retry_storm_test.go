package rules_test

import (
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/domain/testutil"
)

// buildRS is a small helper mirroring buildPII from pii_leak_test.go: it builds
// a graph from a Builder and fails the test on configuration errors.
func buildRS(t *testing.T, b *testutil.Builder) *domain.WorkflowGraph {
	t.Helper()
	g, err := b.Build()
	if err != nil {
		t.Fatalf("testutil.Builder.Build() failed: %v", err)
	}
	return g
}

// ─── Case 1: Name() and Meta() ──────────────────────────────────────────────

func TestRetryStorm_NameAndMeta(t *testing.T) {
	r := rules.NewRetryStorm()
	if got := r.Name(); got != "retry_storm" {
		t.Errorf("Name() = %q, want %q", got, "retry_storm")
	}
	if r.Meta().Name != "retry_storm" {
		t.Errorf("Meta().Name = %q, want %q", r.Meta().Name, "retry_storm")
	}
}

// ─── Case 2: rule implements PathRule ───────────────────────────────────────

func TestRetryStorm_ImplementsPathRule(t *testing.T) {
	var r any = rules.NewRetryStorm()
	if _, ok := r.(domain.PathRule); !ok {
		t.Errorf("RetryStorm does not implement domain.PathRule")
	}
	if _, ok := r.(domain.AnalysisRule); !ok {
		t.Errorf("RetryStorm does not implement domain.AnalysisRule")
	}
}

// ─── Case 3: Critical — retries=5 × parallel(max_concurrency)=20 = 100 ─────

func TestRetryStorm_Critical_BlastRadius100(t *testing.T) {
	g := buildRS(t, testutil.NewBuilder().
		AddNodeWithConfig("api_caller", domain.NodeTypeTool, map[string]any{
			"category":        "api",
			"retries":         5,
			"max_concurrency": 20,
		}).
		Entry("api_caller"))

	findings := rules.NewRetryStorm().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", f.Severity)
	}
	if f.Confidence != 0.9 {
		t.Errorf("Confidence = %.2f, want 0.9", f.Confidence)
	}
	if f.ConfidenceReason != domain.ReasonExactStaticMatch {
		t.Errorf("ConfidenceReason = %q, want %q", f.ConfidenceReason, domain.ReasonExactStaticMatch)
	}
	if f.RuleName != "retry_storm" {
		t.Errorf("RuleName = %q, want %q", f.RuleName, "retry_storm")
	}
	if f.NodeID != "api_caller" {
		t.Errorf("NodeID = %q, want %q", f.NodeID, "api_caller")
	}
	if !strings.Contains(f.Message, "100") {
		t.Errorf("Message should mention blast radius 100, got: %s", f.Message)
	}
	if !strings.Contains(strings.ToLower(f.Suggestion), "backoff") &&
		!strings.Contains(strings.ToLower(f.Suggestion), "circuit") &&
		!strings.Contains(strings.ToLower(f.Suggestion), "rate limiter") {
		t.Errorf("Suggestion should mention backoff/circuit-breaker/rate-limiter, got: %s", f.Suggestion)
	}
}

// ─── Case 4: Warning — retries=3 × parallel=10 = 30 ─────────────────────────

func TestRetryStorm_Warning_BlastRadius30(t *testing.T) {
	g := buildRS(t, testutil.NewBuilder().
		AddNodeWithConfig("flaky_api", domain.NodeTypeTool, map[string]any{
			"category":        "api",
			"max_retries":     3,
			"max_concurrency": 10,
		}).
		Entry("flaky_api"))

	findings := rules.NewRetryStorm().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Warning {
		t.Errorf("Severity = %v, want Warning", f.Severity)
	}
	if f.Confidence != 0.7 {
		t.Errorf("Confidence = %.2f, want 0.7", f.Confidence)
	}
	if f.ConfidenceReason != domain.ReasonHeuristicPattern {
		t.Errorf("ConfidenceReason = %q, want %q", f.ConfidenceReason, domain.ReasonHeuristicPattern)
	}
}

// ─── Case 5: Info — retries=5 × parallel=2 = 10 ─────────────────────────────

func TestRetryStorm_Info_BlastRadius10(t *testing.T) {
	g := buildRS(t, testutil.NewBuilder().
		AddNodeWithConfig("retry_tool", domain.NodeTypeTool, map[string]any{
			"category":        "api",
			"retry_count":     5,
			"max_concurrency": 2,
		}).
		Entry("retry_tool"))

	findings := rules.NewRetryStorm().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Info {
		t.Errorf("Severity = %v, want Info", f.Severity)
	}
	if f.Confidence != 0.5 {
		t.Errorf("Confidence = %.2f, want 0.5", f.Confidence)
	}
	if f.ConfidenceReason != domain.ReasonHeuristicPattern {
		t.Errorf("ConfidenceReason = %q, want %q", f.ConfidenceReason, domain.ReasonHeuristicPattern)
	}
}

// ─── Case 6: blast < 10 → no finding (retries=2, parallel=3 = 6) ────────────

func TestRetryStorm_NoFinding_LowBlastRadius(t *testing.T) {
	g := buildRS(t, testutil.NewBuilder().
		AddNodeWithConfig("benign_tool", domain.NodeTypeTool, map[string]any{
			"category":        "api",
			"retries":         2, // below the >=3 threshold
			"max_concurrency": 3,
		}).
		Entry("benign_tool"))

	findings := rules.NewRetryStorm().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (retries < 3), got %d: %+v", len(findings), findings)
	}
}

// ─── Case 7: retries=3 with parallel=1 (single, no fan-in) → blast 3 → 0 ────

func TestRetryStorm_NoFinding_BlastBelow10(t *testing.T) {
	// retries=3 × parallel=1 = 3 (below info threshold 10).
	g := buildRS(t, testutil.NewBuilder().
		AddNode("entry", domain.NodeTypeLLM).
		AddNodeWithConfig("api_caller", domain.NodeTypeTool, map[string]any{
			"category": "api",
			"retries":  3,
		}).
		AddEdge("entry", "api_caller").
		Entry("entry"))

	findings := rules.NewRetryStorm().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (blast=3 < 10), got %d: %+v", len(findings), findings)
	}
}

// ─── Case 8: parallelism via fan-in (5 incoming edges × retries=3 = 15) ─────

func TestRetryStorm_ParallelismFromFanIn(t *testing.T) {
	b := testutil.NewBuilder()
	for i := 0; i < 5; i++ {
		b.AddNode("upstream_"+string(rune('a'+i)), domain.NodeTypeLLM)
	}
	b.AddNodeWithConfig("api_caller", domain.NodeTypeTool, map[string]any{
		"category": "api",
		"retries":  3,
	})
	for i := 0; i < 5; i++ {
		b.AddEdge("upstream_"+string(rune('a'+i)), "api_caller")
	}
	b.Entry("upstream_a")

	g := buildRS(t, b)
	findings := rules.NewRetryStorm().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (blast=15 = 3×5), got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Info {
		t.Errorf("Severity = %v, want Info (blast 15 in [10,30))", f.Severity)
	}
	if f.NodeID != "api_caller" {
		t.Errorf("NodeID = %q, want %q", f.NodeID, "api_caller")
	}
}

// ─── Case 9: parallelism via upstream Loop max_iterations (50 × retries 3) ──

func TestRetryStorm_ParallelismFromUpstreamLoop(t *testing.T) {
	g := buildRS(t, testutil.NewBuilder().
		AddNodeWithConfig("loop", domain.NodeTypeLoop, map[string]any{"max_iterations": 50}).
		AddNodeWithConfig("api_caller", domain.NodeTypeTool, map[string]any{
			"category": "api",
			"retries":  3,
		}).
		AddEdge("loop", "api_caller").
		Entry("loop"))

	findings := rules.NewRetryStorm().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (blast=150 = 3×50), got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical (blast 150 >= 100)", f.Severity)
	}
}

// ─── Case 10: prefer max() of multiple parallelism signals ──────────────────
//
// Source has max_concurrency=4, fan-in=3, upstream Loop max_iterations=10
// → max() = 10, blast = retries 3 * 10 = 30 → Warning.

func TestRetryStorm_ParallelismMaxOfSignals(t *testing.T) {
	b := testutil.NewBuilder().
		AddNodeWithConfig("loop", domain.NodeTypeLoop, map[string]any{"max_iterations": 10}).
		AddNode("u1", domain.NodeTypeLLM).
		AddNode("u2", domain.NodeTypeLLM).
		AddNode("u3", domain.NodeTypeLLM).
		AddNodeWithConfig("api_caller", domain.NodeTypeTool, map[string]any{
			"category":        "api",
			"retries":         3,
			"max_concurrency": 4,
		}).
		AddEdge("loop", "api_caller").
		AddEdge("u1", "api_caller").
		AddEdge("u2", "api_caller").
		AddEdge("u3", "api_caller").
		Entry("loop")

	g := buildRS(t, b)
	findings := rules.NewRetryStorm().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Warning {
		t.Errorf("Severity = %v, want Warning (blast 30)", f.Severity)
	}
}

// ─── Case 11: nil graph and empty graph do not panic ────────────────────────

func TestRetryStorm_NilGraph(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Analyze(nil) panicked: %v", r)
		}
	}()
	if got := rules.NewRetryStorm().Analyze(nil); len(got) != 0 {
		t.Errorf("expected 0 findings for nil graph, got %d", len(got))
	}
	g := &domain.WorkflowGraph{Nodes: map[string]*domain.Node{}, Edges: nil, EntryNodeID: ""}
	if got := rules.NewRetryStorm().Analyze(g); len(got) != 0 {
		t.Errorf("expected 0 findings for empty graph, got %d", len(got))
	}
}

// ─── Case 12: non-Tool node with retry config is ignored ────────────────────

func TestRetryStorm_IgnoresNonToolNodes(t *testing.T) {
	g := buildRS(t, testutil.NewBuilder().
		AddNodeWithConfig("llm", domain.NodeTypeLLM, map[string]any{
			"retries":         5,
			"max_concurrency": 30,
		}).
		Entry("llm"))

	findings := rules.NewRetryStorm().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (LLM nodes ignored), got %d: %+v", len(findings), findings)
	}
}

// ─── Case 13: every Finding carries ConfidenceReason ────────────────────────

func TestRetryStorm_ConfidenceReasonStamped(t *testing.T) {
	// Mix Critical + Warning + Info to assert the stamp on each path.
	g := buildRS(t, testutil.NewBuilder().
		AddNodeWithConfig("crit", domain.NodeTypeTool, map[string]any{
			"category":        "api",
			"retries":         5,
			"max_concurrency": 20,
		}).
		AddNodeWithConfig("warn", domain.NodeTypeTool, map[string]any{
			"category":        "api",
			"max_retries":     3,
			"max_concurrency": 10,
		}).
		AddNodeWithConfig("info", domain.NodeTypeTool, map[string]any{
			"category":        "api",
			"retry_count":     5,
			"max_concurrency": 2,
		}).
		Entry("crit"))

	findings := rules.NewRetryStorm().Analyze(g)
	if len(findings) != 3 {
		t.Fatalf("expected 3 findings, got %d: %+v", len(findings), findings)
	}
	severities := map[domain.Severity]bool{}
	for _, f := range findings {
		if f.ConfidenceReason == "" {
			t.Errorf("finding %q missing ConfidenceReason", f.NodeID)
		}
		severities[f.Severity] = true
	}
	if !severities[domain.Critical] || !severities[domain.Warning] || !severities[domain.Info] {
		t.Errorf("expected all three severities, got %v", severities)
	}
}

// ─── Case 14: floats in JSON-decoded config are accepted ────────────────────
//
// JSON numbers decode as float64. The rule must accept retries=5.0 the same
// way it accepts retries=5.

func TestRetryStorm_FloatConfigValues(t *testing.T) {
	g := buildRS(t, testutil.NewBuilder().
		AddNodeWithConfig("api", domain.NodeTypeTool, map[string]any{
			"category":        "api",
			"retries":         float64(5),
			"max_concurrency": float64(20),
		}).
		Entry("api"))

	findings := rules.NewRetryStorm().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical (blast 100)", findings[0].Severity)
	}
}
