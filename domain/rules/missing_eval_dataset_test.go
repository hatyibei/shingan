package rules

import (
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
)

// makeGraphWith returns a single-LLM-node graph whose Config is the given
// map. Helper for table-driven cases.
func makeGraphWith(config map[string]any) *domain.WorkflowGraph {
	nodes := map[string]*domain.Node{
		"orchestrator": {
			ID:     "orchestrator",
			Name:   "agent_orchestrator",
			Type:   domain.NodeTypeLLM,
			Config: config,
		},
	}
	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       nil,
		EntryNodeID: "orchestrator",
	}
}

// --- Name / Meta -----------------------------------------------------------

func TestMissingEvalDataset_Name(t *testing.T) {
	if got := NewMissingEvalDatasetChecker().Name(); got != "missing_eval_dataset" {
		t.Errorf("Name() = %q, want missing_eval_dataset", got)
	}
}

func TestMissingEvalDataset_Meta(t *testing.T) {
	m := NewMissingEvalDatasetChecker().Meta()
	if m.Name != "missing_eval_dataset" {
		t.Errorf("Meta.Name = %q, want missing_eval_dataset", m.Name)
	}
	if m.Severity != domain.Warning {
		t.Errorf("Meta.Severity = %s, want Warning", m.Severity)
	}
}

// --- positive: Config["deployment"] == true, no eval ------------------------

func TestMissingEvalDataset_DeployFlagNoEval_Warning(t *testing.T) {
	g := makeGraphWith(map[string]any{
		"model":      "gpt-4o-mini",
		"deployment": true,
	})
	findings := NewMissingEvalDatasetChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Warning {
		t.Errorf("Severity = %s, want Warning", f.Severity)
	}
	if f.Confidence != 0.7 {
		t.Errorf("Confidence = %f, want 0.7", f.Confidence)
	}
	if f.ConfidenceReason != domain.ReasonHeuristicPattern {
		t.Errorf("Reason = %q, want heuristic_pattern", f.ConfidenceReason)
	}
	if !strings.Contains(strings.ToLower(f.Message), "production") {
		t.Errorf("expected 'production' in message, got: %s", f.Message)
	}
	if f.NodeID != "orchestrator" {
		t.Errorf("NodeID = %q, want 'orchestrator' (the deploy-flagged node)", f.NodeID)
	}
}

// --- positive: Config["env"] == "prod" -------------------------------------

func TestMissingEvalDataset_EnvProd_Warning(t *testing.T) {
	g := makeGraphWith(map[string]any{
		"model": "gpt-4o-mini",
		"env":   "prod",
	})
	findings := NewMissingEvalDatasetChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for env=prod, got %d", len(findings))
	}
}

func TestMissingEvalDataset_EnvProduction_Warning(t *testing.T) {
	g := makeGraphWith(map[string]any{
		"model": "gpt-4o-mini",
		"env":   "Production", // case-insensitive
	})
	findings := NewMissingEvalDatasetChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for env=Production, got %d", len(findings))
	}
}

func TestMissingEvalDataset_EnvStaging_Warning(t *testing.T) {
	g := makeGraphWith(map[string]any{
		"model": "gpt-4o-mini",
		"env":   "staging",
	})
	findings := NewMissingEvalDatasetChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for env=staging, got %d", len(findings))
	}
}

// --- negative: Config["env"] == "dev" — no finding ------------------------

func TestMissingEvalDataset_EnvDev_NoFinding(t *testing.T) {
	g := makeGraphWith(map[string]any{
		"model": "gpt-4o-mini",
		"env":   "dev",
	})
	findings := NewMissingEvalDatasetChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for env=dev, got %d", len(findings))
	}
}

// --- negative: deploy flag + eval_dataset present → no finding ------------

func TestMissingEvalDataset_DeployWithEval_NoFinding(t *testing.T) {
	g := makeGraphWith(map[string]any{
		"model":        "gpt-4o-mini",
		"deployment":   true,
		"eval_dataset": "s3://datasets/regression-v3.jsonl",
	})
	findings := NewMissingEvalDatasetChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when eval_dataset is set, got %d: %+v", len(findings), findings)
	}
}

func TestMissingEvalDataset_DeployWithBenchmark_NoFinding(t *testing.T) {
	g := makeGraphWith(map[string]any{
		"model":      "gpt-4o-mini",
		"deployment": true,
		"benchmark":  "internal/eval-suite-2026-04",
	})
	findings := NewMissingEvalDatasetChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when benchmark is set, got %d", len(findings))
	}
}

func TestMissingEvalDataset_DeployWithTestSet_NoFinding(t *testing.T) {
	g := makeGraphWith(map[string]any{
		"model":      "gpt-4o-mini",
		"deployment": true,
		"test_set":   "tests/regression.jsonl",
	})
	findings := NewMissingEvalDatasetChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when test_set is set, got %d", len(findings))
	}
}

// --- positive: deploy on one node, eval on … nothing → Warning -------------

func TestMissingEvalDataset_DeployAndEvalOnDifferentNodes_NoFinding(t *testing.T) {
	// Production graph spreads metadata across nodes: orchestrator declares
	// deploy=prod, downstream node declares eval_dataset. Rule should not
	// fire because the eval signal exists somewhere in the graph.
	nodes := map[string]*domain.Node{
		"orchestrator": {
			ID:   "orchestrator",
			Type: domain.NodeTypeLLM,
			Config: map[string]any{
				"model":      "gpt-4o-mini",
				"deployment": true,
			},
		},
		"evaluator": {
			ID:   "evaluator",
			Type: domain.NodeTypeTool,
			Config: map[string]any{
				"category":     "test",
				"eval_dataset": "internal://evals/v3",
			},
		},
	}
	g := &domain.WorkflowGraph{Nodes: nodes, EntryNodeID: "orchestrator"}
	findings := NewMissingEvalDatasetChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when eval is on a different node, got %d: %+v", len(findings), findings)
	}
}

// --- negative: no deploy signal anywhere → silent --------------------------

func TestMissingEvalDataset_NoDeploySignal_Silent(t *testing.T) {
	g := makeGraphWith(map[string]any{
		"model": "gpt-4o-mini",
	})
	findings := NewMissingEvalDatasetChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when no deploy flag, got %d", len(findings))
	}
}

// --- edge: empty eval_dataset string is treated as missing ----------------

func TestMissingEvalDataset_EmptyEvalString_StillTriggers(t *testing.T) {
	g := makeGraphWith(map[string]any{
		"model":        "gpt-4o-mini",
		"deployment":   true,
		"eval_dataset": "   ", // whitespace only
	})
	findings := NewMissingEvalDatasetChecker().Analyze(g)
	if len(findings) != 1 {
		t.Errorf("expected 1 finding when eval_dataset is empty/whitespace, got %d", len(findings))
	}
}

// --- edge: structured eval_dataset map counts as present -------------------

func TestMissingEvalDataset_MapEvalDataset_NoFinding(t *testing.T) {
	g := makeGraphWith(map[string]any{
		"model":      "gpt-4o-mini",
		"deployment": true,
		"eval_dataset": map[string]any{
			"name":    "regression-v3",
			"version": "2026-04",
		},
	})
	findings := NewMissingEvalDatasetChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for structured eval_dataset map, got %d", len(findings))
	}
}

// --- edge: deploy=false explicitly is silent -------------------------------

func TestMissingEvalDataset_DeployExplicitFalse_Silent(t *testing.T) {
	g := makeGraphWith(map[string]any{
		"model":      "gpt-4o-mini",
		"deployment": false,
	})
	findings := NewMissingEvalDatasetChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for deployment=false, got %d", len(findings))
	}
}

// --- edge: empty / nil graph guard ----------------------------------------

func TestMissingEvalDataset_NilGraph_NoFinding(t *testing.T) {
	findings := NewMissingEvalDatasetChecker().Analyze(nil)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for nil graph, got %d", len(findings))
	}
}

func TestMissingEvalDataset_EmptyGraph_NoFinding(t *testing.T) {
	g := &domain.WorkflowGraph{Nodes: map[string]*domain.Node{}}
	findings := NewMissingEvalDatasetChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for empty graph, got %d", len(findings))
	}
}

// --- single-finding-per-graph guarantee ------------------------------------

func TestMissingEvalDataset_MultipleDeployFlags_OneFinding(t *testing.T) {
	// Two nodes both declare deploy. We still emit only one Finding (graph
	// scope). NodeID is whichever one the iteration reached first — the
	// rule does not promise a specific tie-break.
	nodes := map[string]*domain.Node{
		"orchestrator": {
			ID:   "orchestrator",
			Type: domain.NodeTypeLLM,
			Config: map[string]any{
				"model":      "gpt-4o-mini",
				"deployment": true,
			},
		},
		"runner": {
			ID:   "runner",
			Type: domain.NodeTypeLLM,
			Config: map[string]any{
				"model": "gpt-4o-mini",
				"env":   "prod",
			},
		},
	}
	g := &domain.WorkflowGraph{Nodes: nodes, EntryNodeID: "orchestrator"}
	findings := NewMissingEvalDatasetChecker().Analyze(g)
	if len(findings) != 1 {
		t.Errorf("expected exactly 1 finding (graph-level), got %d", len(findings))
	}
}

// --- ConfidenceReason stamp on every Finding ------------------------------

func TestMissingEvalDataset_AllFindingsHaveReason(t *testing.T) {
	cases := []map[string]any{
		{"model": "gpt-4o-mini", "deployment": true},
		{"model": "gpt-4o-mini", "env": "prod"},
		{"model": "gpt-4o-mini", "env": "Staging"},
		{"model": "gpt-4o-mini", "deploy": true},
	}
	for i, cfg := range cases {
		g := makeGraphWith(cfg)
		findings := NewMissingEvalDatasetChecker().Analyze(g)
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
