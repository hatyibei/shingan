package rules_test

import (
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/domain/testutil"
)

// buildDyn is a tiny helper that builds a graph from a Builder and fails the
// test on configuration errors.
func buildDyn(t *testing.T, b *testutil.Builder) *domain.WorkflowGraph {
	t.Helper()
	g, err := b.Build()
	if err != nil {
		t.Fatalf("testutil.Builder.Build() failed: %v", err)
	}
	return g
}

// ─── Case 1: eval( in Config["body"] → Critical, exact_static_match ─────────

func TestDynamicNodeConstruction_EvalInBody_Critical(t *testing.T) {
	g := buildDyn(t, testutil.NewBuilder().
		AddNodeWithConfig("dyn_node", domain.NodeTypeTool, map[string]any{
			"body": "lambda x: eval(x)",
		}).
		Entry("dyn_node"))

	findings := rules.NewDynamicNodeConstruction().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", f.Severity)
	}
	if f.RuleName != "dynamic_node_construction" {
		t.Errorf("RuleName = %q, want %q", f.RuleName, "dynamic_node_construction")
	}
	if f.Confidence != 0.95 {
		t.Errorf("Confidence = %.2f, want 0.95", f.Confidence)
	}
	if f.ConfidenceReason != domain.ReasonExactStaticMatch {
		t.Errorf("ConfidenceReason = %q, want %q", f.ConfidenceReason, domain.ReasonExactStaticMatch)
	}
	if f.NodeID != "dyn_node" {
		t.Errorf("NodeID = %q, want %q", f.NodeID, "dyn_node")
	}
}

// ─── Case 2: exec( and Function( fire Critical ──────────────────────────────

func TestDynamicNodeConstruction_CriticalPatterns(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"exec_call", "exec(some_str)"},
		{"function_constructor", "new Function('a', 'return eval(a)')"},
		{"eval_with_spaces", "result = eval ( payload )"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := buildDyn(t, testutil.NewBuilder().
				AddNodeWithConfig("n", domain.NodeTypeTool, map[string]any{
					"handler": tc.body,
				}).
				Entry("n"))

			findings := rules.NewDynamicNodeConstruction().Analyze(g)
			if len(findings) != 1 {
				t.Fatalf("expected 1 finding for %q, got %d", tc.name, len(findings))
			}
			if findings[0].Severity != domain.Critical {
				t.Errorf("Severity = %v, want Critical", findings[0].Severity)
			}
		})
	}
}

// ─── Case 3: compile( and __import__( fire Warning ──────────────────────────

func TestDynamicNodeConstruction_WarningPatterns(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"compile", "compile(src, '<str>', 'exec')"},
		{"dunder_import", "__import__('os')"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := buildDyn(t, testutil.NewBuilder().
				AddNodeWithConfig("n", domain.NodeTypeTool, map[string]any{
					"fn": tc.body,
				}).
				Entry("n"))

			findings := rules.NewDynamicNodeConstruction().Analyze(g)
			if len(findings) != 1 {
				t.Fatalf("expected 1 finding for %q, got %d", tc.name, len(findings))
			}
			f := findings[0]
			if f.Severity != domain.Warning {
				t.Errorf("Severity = %v, want Warning", f.Severity)
			}
			if f.Confidence != 0.85 {
				t.Errorf("Confidence = %.2f, want 0.85", f.Confidence)
			}
			if f.ConfidenceReason != domain.ReasonExactStaticMatch {
				t.Errorf("ConfidenceReason = %q, want %q", f.ConfidenceReason, domain.ReasonExactStaticMatch)
			}
		})
	}
}

// ─── Case 4: getattr/setattr fire Info, heuristic_pattern ───────────────────

func TestDynamicNodeConstruction_InfoPatterns(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"getattr", "getattr(obj, name)"},
		{"setattr", "setattr(obj, name, val)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := buildDyn(t, testutil.NewBuilder().
				AddNodeWithConfig("n", domain.NodeTypeTool, map[string]any{
					"body": tc.body,
				}).
				Entry("n"))

			findings := rules.NewDynamicNodeConstruction().Analyze(g)
			if len(findings) != 1 {
				t.Fatalf("expected 1 finding for %q, got %d", tc.name, len(findings))
			}
			f := findings[0]
			if f.Severity != domain.Info {
				t.Errorf("Severity = %v, want Info", f.Severity)
			}
			if f.Confidence != 0.6 {
				t.Errorf("Confidence = %.2f, want 0.6", f.Confidence)
			}
			if f.ConfidenceReason != domain.ReasonHeuristicPattern {
				t.Errorf("ConfidenceReason = %q, want %q", f.ConfidenceReason, domain.ReasonHeuristicPattern)
			}
		})
	}
}

// ─── Case 5: env-var-only placeholder is NOT flagged ────────────────────────

func TestDynamicNodeConstruction_PlaceholdersIgnored(t *testing.T) {
	cases := []map[string]any{
		{"body": "${EVAL_FN}"},
		{"body": "{{handler}}"},
		{"body": "${LAMBDA_BODY}"},
		// Mixed but no actual eval pattern after stripping placeholders.
		{"body": "${PRE} ${POST}"},
	}
	for i, cfg := range cases {
		g := buildDyn(t, testutil.NewBuilder().
			AddNodeWithConfig("n", domain.NodeTypeTool, cfg).
			Entry("n"))

		findings := rules.NewDynamicNodeConstruction().Analyze(g)
		if len(findings) != 0 {
			t.Errorf("case %d: expected 0 findings for placeholder %v, got %d", i, cfg, len(findings))
		}
	}
}

// ─── Case 6: mixed placeholder + real eval still fires ──────────────────────

func TestDynamicNodeConstruction_PlaceholderMixedWithEval_StillFires(t *testing.T) {
	g := buildDyn(t, testutil.NewBuilder().
		AddNodeWithConfig("n", domain.NodeTypeTool, map[string]any{
			"body": "eval(${PAYLOAD})",
		}).
		Entry("n"))

	findings := rules.NewDynamicNodeConstruction().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (placeholder + eval), got %d", len(findings))
	}
	if findings[0].Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", findings[0].Severity)
	}
}

// ─── Case 7: only specific Config keys are scanned ──────────────────────────

func TestDynamicNodeConstruction_OnlyScannedKeys(t *testing.T) {
	// `description` is not in the scanned-key list, so even a literal eval(
	// string in description should be ignored.
	g := buildDyn(t, testutil.NewBuilder().
		AddNodeWithConfig("n", domain.NodeTypeTool, map[string]any{
			"description": "Wraps eval() calls safely with a sandbox",
		}).
		Entry("n"))

	findings := rules.NewDynamicNodeConstruction().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (description is not a scanned key), got %d: %+v", len(findings), findings)
	}
}

// ─── Case 8: Severity precedence — eval beats getattr in same string ───────

func TestDynamicNodeConstruction_PrecedenceCriticalOverInfo(t *testing.T) {
	g := buildDyn(t, testutil.NewBuilder().
		AddNodeWithConfig("n", domain.NodeTypeTool, map[string]any{
			"body": "getattr(obj, 'cmd')(eval(payload))",
		}).
		Entry("n"))

	findings := rules.NewDynamicNodeConstruction().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (highest severity wins), got %d", len(findings))
	}
	if findings[0].Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical (eval beats getattr)", findings[0].Severity)
	}
}

// ─── Case 9: Listener fires on any NodeType (Local rule, OnAny) ─────────────

func TestDynamicNodeConstruction_FiresOnAnyNodeType(t *testing.T) {
	cases := []domain.NodeType{
		domain.NodeTypeTool,
		domain.NodeTypeLLM,
		domain.NodeTypeOutput,
		domain.NodeTypeHuman,
	}
	for _, nt := range cases {
		t.Run(nt.String(), func(t *testing.T) {
			g := buildDyn(t, testutil.NewBuilder().
				AddNodeWithConfig("n", nt, map[string]any{
					"body": "eval(x)",
				}).
				Entry("n"))

			findings := rules.NewDynamicNodeConstruction().Analyze(g)
			if len(findings) != 1 {
				t.Fatalf("expected 1 finding for NodeType=%v, got %d", nt, len(findings))
			}
		})
	}
}

// ─── Case 10: nil graph does not panic ──────────────────────────────────────

func TestDynamicNodeConstruction_NilGraph(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Analyze(nil) panicked: %v", r)
		}
	}()
	findings := rules.NewDynamicNodeConstruction().Analyze(nil)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for nil graph, got %d", len(findings))
	}
}

// ─── Case 11: Name() and Meta() ─────────────────────────────────────────────

func TestDynamicNodeConstruction_NameAndMeta(t *testing.T) {
	r := rules.NewDynamicNodeConstruction()
	if got := r.Name(); got != "dynamic_node_construction" {
		t.Errorf("Name() = %q, want %q", got, "dynamic_node_construction")
	}
	m := r.Meta()
	if m.Name != "dynamic_node_construction" {
		t.Errorf("Meta().Name = %q, want %q", m.Name, "dynamic_node_construction")
	}
	if m.Severity != domain.Critical {
		t.Errorf("Meta().Severity = %v, want Critical (default)", m.Severity)
	}
}

// ─── Case 12: implements LocalRule and AnalysisRule ─────────────────────────

func TestDynamicNodeConstruction_InterfaceAssertion(t *testing.T) {
	var r any = rules.NewDynamicNodeConstruction()
	if _, ok := r.(domain.LocalRule); !ok {
		t.Errorf("DynamicNodeConstruction does not implement domain.LocalRule")
	}
	if _, ok := r.(domain.AnalysisRule); !ok {
		t.Errorf("DynamicNodeConstruction does not implement domain.AnalysisRule")
	}
}

// ─── Case 13: every Finding has ConfidenceReason ────────────────────────────

func TestDynamicNodeConstruction_ConfidenceReasonStamped(t *testing.T) {
	cases := []map[string]any{
		{"body": "eval(x)"},
		{"body": "compile(src, '<s>', 'exec')"},
		{"body": "getattr(obj, name)"},
	}
	for i, cfg := range cases {
		g := buildDyn(t, testutil.NewBuilder().
			AddNodeWithConfig("n", domain.NodeTypeTool, cfg).
			Entry("n"))

		findings := rules.NewDynamicNodeConstruction().Analyze(g)
		if len(findings) == 0 {
			t.Errorf("case %d: expected at least 1 finding, got 0", i)
			continue
		}
		for _, f := range findings {
			if f.ConfidenceReason == "" {
				t.Errorf("case %d: ConfidenceReason missing on finding %+v", i, f)
			}
		}
	}
}

// ─── Case 14: at most one finding per (node, key) ─────────────────────────

func TestDynamicNodeConstruction_OneFindingPerKey(t *testing.T) {
	// Two scanned keys, both with eval — expect 2 findings (one per key)
	// rather than N findings per match.
	g := buildDyn(t, testutil.NewBuilder().
		AddNodeWithConfig("n", domain.NodeTypeTool, map[string]any{
			"body":    "eval(a) + eval(b)",
			"handler": "eval(c)",
		}).
		Entry("n"))

	findings := rules.NewDynamicNodeConstruction().Analyze(g)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings (one per scanned key), got %d: %+v", len(findings), findings)
	}
}

// ─── Case 15: Suggestion mentions eval/exec or sandboxed evaluator ────────

func TestDynamicNodeConstruction_SuggestionMentionsRefactor(t *testing.T) {
	g := buildDyn(t, testutil.NewBuilder().
		AddNodeWithConfig("n", domain.NodeTypeTool, map[string]any{
			"body": "eval(payload)",
		}).
		Entry("n"))

	findings := rules.NewDynamicNodeConstruction().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	s := strings.ToLower(findings[0].Suggestion)
	if !strings.Contains(s, "sandbox") && !strings.Contains(s, "dispatch") && !strings.Contains(s, "allowlist") {
		t.Errorf("Suggestion should mention sandbox/dispatch/allowlist, got: %s", findings[0].Suggestion)
	}
}

// ─── Case 16: nested map / slice in Config is recursively scanned ──────────

func TestDynamicNodeConstruction_RecursiveScan(t *testing.T) {
	g := buildDyn(t, testutil.NewBuilder().
		AddNodeWithConfig("n", domain.NodeTypeTool, map[string]any{
			"body": map[string]any{
				"inner": "eval(x)",
			},
		}).
		Entry("n"))

	findings := rules.NewDynamicNodeConstruction().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding from nested map, got %d", len(findings))
	}
	if findings[0].Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", findings[0].Severity)
	}
}
