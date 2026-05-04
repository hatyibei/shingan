package rules

import (
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
)

// makeModelCardGraph builds a single-LLM-node WorkflowGraph for the
// model_card_mismatch tests.
func makeModelCardGraph(nodeType domain.NodeType, config map[string]any) *domain.WorkflowGraph {
	nodes := map[string]*domain.Node{
		"n1": {
			ID:     "n1",
			Name:   "test_llm",
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

// --- positive: known prefix vs wrong base_url ------------------------------

func TestModelCardMismatch_GPT_AnthropicURL_Critical(t *testing.T) {
	g := makeModelCardGraph(domain.NodeTypeLLM, map[string]any{
		"model":    "gpt-4o",
		"base_url": "https://api.anthropic.com/v1",
	})
	findings := NewModelCardMismatchChecker().Analyze(g)
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
	if f.ConfidenceReason != domain.ReasonExactStaticMatch {
		t.Errorf("expected ReasonExactStaticMatch, got %q", f.ConfidenceReason)
	}
	if f.RuleName != "model_card_mismatch" {
		t.Errorf("expected rule name model_card_mismatch, got %s", f.RuleName)
	}
	// Suggestion should mention both expected and actual providers
	if !strings.Contains(f.Suggestion, "openai") || !strings.Contains(f.Suggestion, "anthropic") {
		t.Errorf("expected suggestion to mention both providers, got: %s", f.Suggestion)
	}
}

func TestModelCardMismatch_Claude_OpenAIURL_Critical(t *testing.T) {
	g := makeModelCardGraph(domain.NodeTypeLLM, map[string]any{
		"model":    "claude-3-5-sonnet",
		"base_url": "https://api.openai.com/v1",
	})
	findings := NewModelCardMismatchChecker().Analyze(g)
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

// --- positive: known prefix vs wrong provider string -----------------------

func TestModelCardMismatch_Gemini_AnthropicProvider_Critical(t *testing.T) {
	g := makeModelCardGraph(domain.NodeTypeLLM, map[string]any{
		"model":    "gemini-1.5-pro",
		"provider": "anthropic",
	})
	findings := NewModelCardMismatchChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != domain.Critical {
		t.Errorf("expected Critical, got %s", findings[0].Severity)
	}
}

// --- negative: matching provider/url ---------------------------------------

func TestModelCardMismatch_GPT_OpenAIURL_NoFinding(t *testing.T) {
	g := makeModelCardGraph(domain.NodeTypeLLM, map[string]any{
		"model":    "gpt-4o-mini",
		"base_url": "https://api.openai.com/v1",
		"provider": "openai",
	})
	findings := NewModelCardMismatchChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when model and url match, got %d: %+v", len(findings), findings)
	}
}

func TestModelCardMismatch_Claude_AnthropicProvider_NoFinding(t *testing.T) {
	g := makeModelCardGraph(domain.NodeTypeLLM, map[string]any{
		"model":    "claude-3-5-sonnet",
		"provider": "anthropic",
	})
	findings := NewModelCardMismatchChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when claude + anthropic provider, got %d", len(findings))
	}
}

func TestModelCardMismatch_Gemini_VertexAI_NoFinding(t *testing.T) {
	// gemini-* on Google Vertex AI endpoint should not flag.
	g := makeModelCardGraph(domain.NodeTypeLLM, map[string]any{
		"model":    "gemini-1.5-pro",
		"base_url": "https://us-central1-aiplatform.googleapis.com/v1",
	})
	findings := NewModelCardMismatchChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for gemini on vertex AI, got %d", len(findings))
	}
}

func TestModelCardMismatch_GPT_AzureOpenAI_NoFinding(t *testing.T) {
	// Azure OpenAI is the same provider — must NOT flag.
	g := makeModelCardGraph(domain.NodeTypeLLM, map[string]any{
		"model":    "gpt-4o",
		"base_url": "https://my-resource.openai.azure.com/",
	})
	findings := NewModelCardMismatchChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for gpt-4o on Azure OpenAI, got %d", len(findings))
	}
}

// --- negative: no provider config (cannot judge) ---------------------------

func TestModelCardMismatch_NoEndpoint_NoFinding(t *testing.T) {
	g := makeModelCardGraph(domain.NodeTypeLLM, map[string]any{
		"model": "gpt-4o",
	})
	findings := NewModelCardMismatchChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings without provider/base_url, got %d", len(findings))
	}
}

// --- unknown prefix: Info heuristic when provider hint exists --------------

func TestModelCardMismatch_UnknownPrefix_WithProvider_Info(t *testing.T) {
	g := makeModelCardGraph(domain.NodeTypeLLM, map[string]any{
		"model":    "mistral-large",
		"provider": "mistral",
	})
	findings := NewModelCardMismatchChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 Info finding for unknown prefix + provider, got %d", len(findings))
	}
	f := findings[0]
	if f.Severity != domain.Info {
		t.Errorf("expected Info severity for unknown prefix, got %s", f.Severity)
	}
	if f.Confidence != 0.4 {
		t.Errorf("expected confidence 0.4, got %f", f.Confidence)
	}
	if f.ConfidenceReason != domain.ReasonHeuristicPattern {
		t.Errorf("expected ReasonHeuristicPattern, got %q", f.ConfidenceReason)
	}
}

func TestModelCardMismatch_UnknownPrefix_NoProvider_NoFinding(t *testing.T) {
	g := makeModelCardGraph(domain.NodeTypeLLM, map[string]any{
		"model": "mistral-large",
	})
	findings := NewModelCardMismatchChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (knowledge gap, no provider), got %d", len(findings))
	}
}

// --- negative: model key absent --------------------------------------------

func TestModelCardMismatch_NoModelKey_NoFinding(t *testing.T) {
	g := makeModelCardGraph(domain.NodeTypeLLM, map[string]any{
		"provider": "openai",
	})
	findings := NewModelCardMismatchChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when model is absent, got %d", len(findings))
	}
}

// --- negative: non-LLM node skipped ----------------------------------------

func TestModelCardMismatch_NonLLMNode_Skipped(t *testing.T) {
	g := makeModelCardGraph(domain.NodeTypeTool, map[string]any{
		"model":    "gpt-4o",
		"base_url": "https://api.anthropic.com/v1",
	})
	findings := NewModelCardMismatchChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for non-LLM node, got %d", len(findings))
	}
}

// --- meta + reason stamp ---------------------------------------------------

func TestModelCardMismatch_Meta(t *testing.T) {
	c := NewModelCardMismatchChecker()
	if c.Name() != "model_card_mismatch" {
		t.Errorf("expected name model_card_mismatch, got %s", c.Name())
	}
	m := c.Meta()
	if m.Name != "model_card_mismatch" {
		t.Errorf("Meta.Name expected model_card_mismatch, got %s", m.Name)
	}
	if m.Severity != domain.Critical {
		t.Errorf("Meta.Severity expected Critical (default), got %s", m.Severity)
	}
}

func TestModelCardMismatch_AllFindingsHaveReason(t *testing.T) {
	cases := []map[string]any{
		{"model": "gpt-4o", "base_url": "https://api.anthropic.com/v1"},
		{"model": "claude-3-5-sonnet", "provider": "openai"},
		{"model": "mistral-large", "provider": "openai"},
	}
	for i, cfg := range cases {
		g := makeModelCardGraph(domain.NodeTypeLLM, cfg)
		findings := NewModelCardMismatchChecker().Analyze(g)
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

// --- nil / empty graph guard -----------------------------------------------

func TestModelCardMismatch_NilGraph_NoFinding(t *testing.T) {
	findings := NewModelCardMismatchChecker().Analyze(nil)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for nil graph, got %d", len(findings))
	}
}

// --- model with explicit provider override (gpt-* on openai-compatible) ----

func TestModelCardMismatch_OpenAICompatibleProxy_NoFinding(t *testing.T) {
	// User self-hosted OpenAI-compatible proxy — provider correctly set to
	// "openai", base_url is custom but provider takes precedence; the rule
	// should not fabricate a mismatch on the URL alone when provider matches.
	g := makeModelCardGraph(domain.NodeTypeLLM, map[string]any{
		"model":    "gpt-4o",
		"provider": "openai",
		"base_url": "https://my-internal-proxy.corp.example/v1",
	})
	findings := NewModelCardMismatchChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when provider matches model prefix (proxy case), got %d: %+v", len(findings), findings)
	}
}
