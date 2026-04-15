package rules

import (
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
)

// makeGraph is a test helper that constructs a one-node WorkflowGraph.
func makeDeprecatedGraph(nodeType domain.NodeType, config map[string]any) *domain.WorkflowGraph {
	nodes := map[string]*domain.Node{
		"n1": {
			ID:     "n1",
			Name:   "test_node",
			Type:   nodeType,
			Config: config,
		},
	}
	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       nil,
		EntryNodeID: "n1",
	}
}

func TestDeprecatedModel_ShutdownGPT35(t *testing.T) {
	g := makeDeprecatedGraph(domain.NodeTypeLLM, map[string]any{
		"model": "gpt-3.5-turbo-0613",
	})
	checker := NewDeprecatedModelChecker()
	findings := checker.Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Severity != domain.Critical {
		t.Errorf("expected Critical, got %s", f.Severity)
	}
	if f.Confidence != 1.0 {
		t.Errorf("expected confidence 1.0, got %f", f.Confidence)
	}
	if !strings.Contains(f.Message, "shut down") {
		t.Errorf("expected 'shut down' in message, got: %s", f.Message)
	}
	if f.RuleName != "deprecated_model" {
		t.Errorf("expected rule name 'deprecated_model', got %s", f.RuleName)
	}
}

func TestDeprecatedModel_ShutdownClaude2(t *testing.T) {
	g := makeDeprecatedGraph(domain.NodeTypeLLM, map[string]any{
		"model": "claude-2",
	})
	findings := NewDeprecatedModelChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != domain.Critical {
		t.Errorf("expected Critical, got %s", findings[0].Severity)
	}
	if findings[0].Confidence != 1.0 {
		t.Errorf("expected confidence 1.0, got %f", findings[0].Confidence)
	}
}

func TestDeprecatedModel_ShutdownGeminiPro(t *testing.T) {
	g := makeDeprecatedGraph(domain.NodeTypeLLM, map[string]any{
		"model": "gemini-pro",
	})
	findings := NewDeprecatedModelChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != domain.Critical {
		t.Errorf("expected Critical, got %s", findings[0].Severity)
	}
}

func TestDeprecatedModel_DeprecatedGPT4_32k(t *testing.T) {
	g := makeDeprecatedGraph(domain.NodeTypeLLM, map[string]any{
		"model": "gpt-4-32k",
	})
	findings := NewDeprecatedModelChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Severity != domain.Warning {
		t.Errorf("expected Warning, got %s", f.Severity)
	}
	if f.Confidence != 0.9 {
		t.Errorf("expected confidence 0.9, got %f", f.Confidence)
	}
	if !strings.Contains(f.Message, "deprecated") {
		t.Errorf("expected 'deprecated' in message, got: %s", f.Message)
	}
}

func TestDeprecatedModel_ActiveModel_NoFinding(t *testing.T) {
	g := makeDeprecatedGraph(domain.NodeTypeLLM, map[string]any{
		"model": "gpt-4o",
	})
	findings := NewDeprecatedModelChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for active model, got %d", len(findings))
	}
}

func TestDeprecatedModel_NoModelConfig_NoFinding(t *testing.T) {
	g := makeDeprecatedGraph(domain.NodeTypeLLM, map[string]any{
		"prompt_template": "some_template",
	})
	findings := NewDeprecatedModelChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when model key absent, got %d", len(findings))
	}
}

func TestDeprecatedModel_NonLLMNode_Skipped(t *testing.T) {
	g := makeDeprecatedGraph(domain.NodeTypeTool, map[string]any{
		"model": "gpt-3.5-turbo-0613", // should be ignored — node is not LLM
	})
	findings := NewDeprecatedModelChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for non-LLM node, got %d", len(findings))
	}
}

func TestDeprecatedModel_ModelIsNonString_Skipped(t *testing.T) {
	g := makeDeprecatedGraph(domain.NodeTypeLLM, map[string]any{
		"model": 12345, // integer, not a string
	})
	findings := NewDeprecatedModelChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when model is non-string, got %d", len(findings))
	}
}

func TestDeprecatedModel_SuggestionContainsReplacement(t *testing.T) {
	g := makeDeprecatedGraph(domain.NodeTypeLLM, map[string]any{
		"model": "gpt-3.5-turbo-0613",
	})
	findings := NewDeprecatedModelChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if !strings.Contains(findings[0].Suggestion, "gpt-4o-mini") {
		t.Errorf("expected suggestion to reference replacement model, got: %s", findings[0].Suggestion)
	}
}

func TestDeprecatedModel_NilGraph_NoFinding(t *testing.T) {
	findings := NewDeprecatedModelChecker().Analyze(nil)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for nil graph, got %d", len(findings))
	}
}

func TestDeprecatedModel_EmptyGraph_NoFinding(t *testing.T) {
	g := &domain.WorkflowGraph{
		Nodes: map[string]*domain.Node{},
	}
	findings := NewDeprecatedModelChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for empty graph, got %d", len(findings))
	}
}
