package rules_test

import (
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/domain/testutil"
)

// buildCDA mirrors buildPII / buildRS: build a graph from a Builder and fail
// the test on configuration errors.
func buildCDA(t *testing.T, b *testutil.Builder) *domain.WorkflowGraph {
	t.Helper()
	g, err := b.Build()
	if err != nil {
		t.Fatalf("testutil.Builder.Build() failed: %v", err)
	}
	return g
}

// agentNode adds an LLM node tagged with Config["agent_role"], the canonical
// way to declare a multi-agent participant.
func agentNode(b *testutil.Builder, id string, role string) *testutil.Builder {
	return b.AddNodeWithConfig(id, domain.NodeTypeLLM, map[string]any{
		"model":      "gpt-4o-mini",
		"agent_role": role,
	})
}

// ─── Case 1: Name() and Meta() ──────────────────────────────────────────────

func TestCircularDepAgents_NameAndMeta(t *testing.T) {
	r := rules.NewCircularDepAgents()
	if got := r.Name(); got != "circular_dep_agents" {
		t.Errorf("Name() = %q, want %q", got, "circular_dep_agents")
	}
	if r.Meta().Name != "circular_dep_agents" {
		t.Errorf("Meta().Name = %q, want %q", r.Meta().Name, "circular_dep_agents")
	}
}

// ─── Case 2: rule implements PathRule + AnalysisRule ────────────────────────

func TestCircularDepAgents_ImplementsPathRule(t *testing.T) {
	var r any = rules.NewCircularDepAgents()
	if _, ok := r.(domain.PathRule); !ok {
		t.Errorf("CircularDepAgents does not implement domain.PathRule")
	}
	if _, ok := r.(domain.AnalysisRule); !ok {
		t.Errorf("CircularDepAgents does not implement domain.AnalysisRule")
	}
}

// ─── Case 3: 2-agent cycle (A → B → A) → Warning, 0.85 ──────────────────────

func TestCircularDepAgents_TwoAgentCycle_Warning(t *testing.T) {
	b := testutil.NewBuilder()
	agentNode(b, "agent_a", "planner")
	agentNode(b, "agent_b", "worker")
	b.AddEdge("agent_a", "agent_b")
	b.AddEdge("agent_b", "agent_a")
	b.Entry("agent_a")

	findings := rules.NewCircularDepAgents().Analyze(buildCDA(t, b))
	if len(findings) == 0 {
		t.Fatalf("expected at least 1 finding, got 0")
	}
	// Each agent in the cycle is reported (so we get >=1; depending on the
	// implementation, both agents' starting points may emit). Assert all are
	// Warning + 0.85 + exact_static_match.
	for _, f := range findings {
		if f.Severity != domain.Warning {
			t.Errorf("Severity = %v, want Warning", f.Severity)
		}
		if f.Confidence != 0.85 {
			t.Errorf("Confidence = %.2f, want 0.85", f.Confidence)
		}
		if f.ConfidenceReason != domain.ReasonExactStaticMatch {
			t.Errorf("ConfidenceReason = %q, want %q", f.ConfidenceReason, domain.ReasonExactStaticMatch)
		}
		if f.RuleName != "circular_dep_agents" {
			t.Errorf("RuleName = %q, want %q", f.RuleName, "circular_dep_agents")
		}
	}
	// Suggestion should mention orchestrator pattern or max_handoffs.
	if !strings.Contains(strings.ToLower(findings[0].Suggestion), "orchestrator") &&
		!strings.Contains(strings.ToLower(findings[0].Suggestion), "max_handoffs") {
		t.Errorf("Suggestion should mention orchestrator or max_handoffs, got: %s", findings[0].Suggestion)
	}
}

// ─── Case 4: 3-agent cycle (A → B → C → A) → Warning, 0.75 ─────────────────

func TestCircularDepAgents_ThreeAgentCycle_Warning(t *testing.T) {
	b := testutil.NewBuilder()
	agentNode(b, "agent_a", "planner")
	agentNode(b, "agent_b", "worker")
	agentNode(b, "agent_c", "reviewer")
	b.AddEdge("agent_a", "agent_b")
	b.AddEdge("agent_b", "agent_c")
	b.AddEdge("agent_c", "agent_a")
	b.Entry("agent_a")

	findings := rules.NewCircularDepAgents().Analyze(buildCDA(t, b))
	if len(findings) == 0 {
		t.Fatalf("expected at least 1 finding, got 0")
	}
	for _, f := range findings {
		if f.Severity != domain.Warning {
			t.Errorf("Severity = %v, want Warning", f.Severity)
		}
		if f.Confidence != 0.75 {
			t.Errorf("Confidence = %.2f, want 0.75", f.Confidence)
		}
		if f.ConfidenceReason != domain.ReasonExactStaticMatch {
			t.Errorf("ConfidenceReason = %q, want %q", f.ConfidenceReason, domain.ReasonExactStaticMatch)
		}
	}
}

// ─── Case 5: self-reference (A → A) → Info, 0.6, heuristic_pattern ──────────

func TestCircularDepAgents_SelfReference_Info(t *testing.T) {
	b := testutil.NewBuilder()
	agentNode(b, "self_agent", "planner")
	b.AddEdge("self_agent", "self_agent")
	b.Entry("self_agent")

	findings := rules.NewCircularDepAgents().Analyze(buildCDA(t, b))
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Info {
		t.Errorf("Severity = %v, want Info", f.Severity)
	}
	if f.Confidence != 0.6 {
		t.Errorf("Confidence = %.2f, want 0.6", f.Confidence)
	}
	if f.ConfidenceReason != domain.ReasonHeuristicPattern {
		t.Errorf("ConfidenceReason = %q, want %q (intentional recursion may exist)", f.ConfidenceReason, domain.ReasonHeuristicPattern)
	}
}

// ─── Case 6: linear agent chain (A → B → C, no back edge) → 0 findings ─────

func TestCircularDepAgents_NoCycle_LinearChain(t *testing.T) {
	b := testutil.NewBuilder()
	agentNode(b, "agent_a", "planner")
	agentNode(b, "agent_b", "worker")
	agentNode(b, "agent_c", "reviewer")
	b.AddEdge("agent_a", "agent_b")
	b.AddEdge("agent_b", "agent_c")
	b.Entry("agent_a")

	findings := rules.NewCircularDepAgents().Analyze(buildCDA(t, b))
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (no cycle), got %d: %+v", len(findings), findings)
	}
}

// ─── Case 7: agents using sub_agents config (instead of agent_role) ────────
//
// Some frameworks declare a parent agent with a `sub_agents` list rather than
// per-agent `agent_role`. The detector must accept this variant too.

func TestCircularDepAgents_SubAgentsConfig(t *testing.T) {
	g := buildCDA(t, testutil.NewBuilder().
		AddNodeWithConfig("orchestrator", domain.NodeTypeLLM, map[string]any{
			"model":      "gpt-4o-mini",
			"sub_agents": []any{"worker_a", "worker_b"},
		}).
		AddNodeWithConfig("worker", domain.NodeTypeLLM, map[string]any{
			"model":      "gpt-4o-mini",
			"agent_role": "worker",
		}).
		AddEdge("orchestrator", "worker").
		AddEdge("worker", "orchestrator").
		Entry("orchestrator"))

	findings := rules.NewCircularDepAgents().Analyze(g)
	if len(findings) == 0 {
		t.Fatalf("expected ≥1 finding (sub_agents flag identifies orchestrator as agent), got 0")
	}
	if findings[0].Severity != domain.Warning {
		t.Errorf("Severity = %v, want Warning (2-agent cycle)", findings[0].Severity)
	}
}

// ─── Case 8: cycle through non-agent intermediate node ─────────────────────
//
// agent_a → tool → agent_b → agent_a. A "tool" is not an agent, but the path
// is still an agent-to-agent delegation cycle (the tool acts as a transfer
// router, e.g. transfer_to_agent). The rule MUST detect this.

func TestCircularDepAgents_CycleThroughTransferTool(t *testing.T) {
	b := testutil.NewBuilder()
	agentNode(b, "agent_a", "planner")
	agentNode(b, "agent_b", "worker")
	b.AddNodeWithConfig("transfer_tool", domain.NodeTypeTool, map[string]any{
		"category": "transfer",
		"name":     "transfer_to_agent",
	})
	b.AddEdge("agent_a", "transfer_tool")
	b.AddEdge("transfer_tool", "agent_b")
	b.AddEdge("agent_b", "agent_a")
	b.Entry("agent_a")

	findings := rules.NewCircularDepAgents().Analyze(buildCDA(t, b))
	if len(findings) == 0 {
		t.Fatalf("expected ≥1 finding (cycle via transfer tool), got 0")
	}
	// 2 agents in the cycle (a, b) so this should classify as a 2-agent cycle.
	for _, f := range findings {
		if f.Severity != domain.Warning {
			t.Errorf("Severity = %v, want Warning", f.Severity)
		}
		if f.Confidence != 0.85 {
			t.Errorf("Confidence = %.2f, want 0.85 (2-agent cycle)", f.Confidence)
		}
	}
}

// ─── Case 9: graph with raw cycle but no agents → 0 findings ───────────────

func TestCircularDepAgents_NoAgentMetadata(t *testing.T) {
	g := buildCDA(t, testutil.NewBuilder().
		AddNode("a", domain.NodeTypeLLM).
		AddNode("b", domain.NodeTypeLLM).
		AddEdge("a", "b").
		AddEdge("b", "a").
		Entry("a"))

	findings := rules.NewCircularDepAgents().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (no agent metadata), got %d: %+v", len(findings), findings)
	}
}

// ─── Case 10: nil graph and empty graph do not panic ────────────────────────

func TestCircularDepAgents_NilGraph(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Analyze(nil) panicked: %v", r)
		}
	}()
	if got := rules.NewCircularDepAgents().Analyze(nil); len(got) != 0 {
		t.Errorf("expected 0 findings for nil graph, got %d", len(got))
	}
	g := &domain.WorkflowGraph{Nodes: map[string]*domain.Node{}, Edges: nil, EntryNodeID: ""}
	if got := rules.NewCircularDepAgents().Analyze(g); len(got) != 0 {
		t.Errorf("expected 0 findings for empty graph, got %d", len(got))
	}
}

// ─── Case 11: every Finding carries a ConfidenceReason ─────────────────────

func TestCircularDepAgents_ConfidenceReasonStamped(t *testing.T) {
	b := testutil.NewBuilder()
	// 2-agent cycle.
	agentNode(b, "two_a", "planner")
	agentNode(b, "two_b", "worker")
	b.AddEdge("two_a", "two_b")
	b.AddEdge("two_b", "two_a")
	// 3-agent cycle.
	agentNode(b, "three_a", "planner")
	agentNode(b, "three_b", "worker")
	agentNode(b, "three_c", "reviewer")
	b.AddEdge("three_a", "three_b")
	b.AddEdge("three_b", "three_c")
	b.AddEdge("three_c", "three_a")
	// Self-ref.
	agentNode(b, "self", "planner")
	b.AddEdge("self", "self")
	b.Entry("two_a")

	findings := rules.NewCircularDepAgents().Analyze(buildCDA(t, b))
	if len(findings) == 0 {
		t.Fatalf("expected ≥1 finding, got 0")
	}
	for _, f := range findings {
		if f.ConfidenceReason == "" {
			t.Errorf("finding %q missing ConfidenceReason: %+v", f.NodeID, f)
		}
	}
	// Verify all expected severity levels appear.
	severities := map[domain.Severity]int{}
	for _, f := range findings {
		severities[f.Severity]++
	}
	if severities[domain.Warning] == 0 {
		t.Errorf("expected ≥1 Warning finding, got %v", severities)
	}
	if severities[domain.Info] == 0 {
		t.Errorf("expected ≥1 Info finding (self-ref), got %v", severities)
	}
}

// ─── Case 12: cycle_detection coexistence — both rules fire together ───────
//
// Per task spec: cycle_detection and circular_dep_agents OVERLAP intentionally.
// cycle_detection fires Critical for the structural back-edge; circular_dep_agents
// fires Warning specifically for the agent-delegation pattern. The static
// design guarantees both fire and surface different aspects.

func TestCircularDepAgents_CoexistsWithCycleDetection(t *testing.T) {
	b := testutil.NewBuilder()
	agentNode(b, "agent_a", "planner")
	agentNode(b, "agent_b", "worker")
	b.AddEdge("agent_a", "agent_b")
	b.AddEdge("agent_b", "agent_a")
	b.Entry("agent_a")
	g := buildCDA(t, b)

	cdaFindings := rules.NewCircularDepAgents().Analyze(g)
	if len(cdaFindings) == 0 {
		t.Fatalf("expected ≥1 circular_dep_agents finding, got 0")
	}
	cycleFindings := rules.NewCycleDetector().Analyze(g)
	if len(cycleFindings) == 0 {
		t.Fatalf("expected ≥1 cycle_detection finding, got 0")
	}

	// Verify the Severity contrast: cycle_detection MUST be Critical for an
	// unbounded raw cycle (no parent Loop), circular_dep_agents Warning.
	for _, f := range cycleFindings {
		if f.RuleName != "cycle_detection" {
			continue
		}
		if f.Severity != domain.Critical {
			t.Errorf("cycle_detection on raw cycle = %v, want Critical", f.Severity)
		}
	}
	for _, f := range cdaFindings {
		if f.Severity != domain.Warning {
			t.Errorf("circular_dep_agents = %v, want Warning", f.Severity)
		}
	}
}

// ─── Case 13: only one agent in cycle (others are non-agents) → 0 findings ─
//
// agent_a → tool → tool → agent_a. The cycle structurally exists but only
// involves ONE agent; it is therefore not a delegation cycle.

func TestCircularDepAgents_SingleAgentInCycle_IsNotCircularDep(t *testing.T) {
	b := testutil.NewBuilder()
	agentNode(b, "lone_agent", "planner")
	b.AddNodeWithConfig("tool_a", domain.NodeTypeTool, map[string]any{"category": "api"})
	b.AddNodeWithConfig("tool_b", domain.NodeTypeTool, map[string]any{"category": "api"})
	b.AddEdge("lone_agent", "tool_a")
	b.AddEdge("tool_a", "tool_b")
	b.AddEdge("tool_b", "lone_agent")
	b.Entry("lone_agent")

	findings := rules.NewCircularDepAgents().Analyze(buildCDA(t, b))
	// A single agent revisited via tools is NOT a multi-agent delegation
	// cycle (it is a tool-mediated retry/loop). Self-reference is detected by
	// a separate predicate (agent → agent self-edge).
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (only one agent in cycle), got %d: %+v", len(findings), findings)
	}
}
