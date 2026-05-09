package rules

import (
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
)

// makePromptSecretGraph constructs a one-LLM-node WorkflowGraph for the
// secret_in_prompt_template rule's test cases.
func makePromptSecretGraph(config map[string]any) *domain.WorkflowGraph {
	nodes := map[string]*domain.Node{
		"l1": {
			ID:     "l1",
			Name:   "llm_under_test",
			Type:   domain.NodeTypeLLM,
			Config: config,
		},
	}
	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       nil,
		EntryNodeID: "l1",
	}
}

// --- Name / Meta -----------------------------------------------------------

func TestSecretInPromptTemplate_Name(t *testing.T) {
	if got := NewSecretInPromptTemplate().Name(); got != "secret_in_prompt_template" {
		t.Errorf("Name() = %q, want secret_in_prompt_template", got)
	}
}

func TestSecretInPromptTemplate_Meta(t *testing.T) {
	m := NewSecretInPromptTemplate().Meta()
	if m.Name != "secret_in_prompt_template" {
		t.Errorf("Meta.Name = %q, want secret_in_prompt_template", m.Name)
	}
	if m.Severity != domain.Critical {
		t.Errorf("Meta.Severity = %s, want Critical", m.Severity)
	}
}

// --- positive: OpenAI key in system_prompt ---------------------------------

func TestSecretInPromptTemplate_OpenAIKey_Critical(t *testing.T) {
	g := makePromptSecretGraph(map[string]any{
		"model":         "gpt-4o-mini",
		"system_prompt": "You are an assistant. API key: sk-abcdefghijklmnopqrstuvwxyz1234567890",
	})
	findings := NewSecretInPromptTemplate().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Critical {
		t.Errorf("Severity = %s, want Critical", f.Severity)
	}
	if f.Confidence != 0.95 {
		t.Errorf("Confidence = %f, want 0.95", f.Confidence)
	}
	if f.ConfidenceReason != domain.ReasonExactStaticMatch {
		t.Errorf("Reason = %q, want exact_static_match", f.ConfidenceReason)
	}
	if !strings.Contains(f.Message, "openai_api_key") {
		t.Errorf("expected 'openai_api_key' in message, got: %s", f.Message)
	}
	if !strings.Contains(f.Message, "system_prompt") {
		t.Errorf("expected 'system_prompt' in message, got: %s", f.Message)
	}
	if !strings.Contains(f.Suggestion, "rotate") {
		t.Errorf("expected 'rotate' in Suggestion, got: %s", f.Suggestion)
	}
}

// --- positive: Anthropic key matched before generic sk- ---------------------

func TestSecretInPromptTemplate_AnthropicKey_Critical(t *testing.T) {
	g := makePromptSecretGraph(map[string]any{
		"model":         "claude-3-5-sonnet",
		"system_prompt": "Use sk-ant-api03-abcdef0123456789ABCDEFGHIJ for routing.",
	})
	findings := NewSecretInPromptTemplate().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if !strings.Contains(findings[0].Message, "anthropic_api_key") {
		t.Errorf("expected 'anthropic_api_key' (more specific match), got: %s", findings[0].Message)
	}
}

// --- positive: AWS access key in instruction -------------------------------

func TestSecretInPromptTemplate_AWSKey_Critical(t *testing.T) {
	g := makePromptSecretGraph(map[string]any{
		"model":       "gpt-4o-mini",
		"instruction": "Region: us-west-2. Access key: AKIAIOSFODNN7EXAMPLE.",
	})
	findings := NewSecretInPromptTemplate().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != domain.Critical {
		t.Errorf("Severity = %s, want Critical", findings[0].Severity)
	}
}

// --- positive: GitHub token in prompt_template -----------------------------

func TestSecretInPromptTemplate_GitHubToken_Critical(t *testing.T) {
	g := makePromptSecretGraph(map[string]any{
		"model":           "gpt-4o-mini",
		"prompt_template": "Token ghp_abcdefghijklmnopqrstuvwxyz0123456789AB issued.",
	})
	findings := NewSecretInPromptTemplate().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if !strings.Contains(findings[0].Message, "github_token") {
		t.Errorf("expected 'github_token' in message, got: %s", findings[0].Message)
	}
}

// --- positive: PEM private key in user_message_template --------------------

func TestSecretInPromptTemplate_PEM_Critical(t *testing.T) {
	g := makePromptSecretGraph(map[string]any{
		"model":                 "gpt-4o-mini",
		"user_message_template": "Sign with:\n-----BEGIN RSA PRIVATE KEY-----\nMIIB...",
	})
	findings := NewSecretInPromptTemplate().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if !strings.Contains(findings[0].Message, "private_key_pem") {
		t.Errorf("expected 'private_key_pem' in message, got: %s", findings[0].Message)
	}
}

// --- positive: JWT is Warning (heuristic) ----------------------------------

func TestSecretInPromptTemplate_JWT_Warning(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	g := makePromptSecretGraph(map[string]any{
		"model":         "gpt-4o-mini",
		"system_prompt": "Authorization: Bearer " + jwt,
	})
	findings := NewSecretInPromptTemplate().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Severity != domain.Warning {
		t.Errorf("Severity = %s, want Warning (JWT)", f.Severity)
	}
	if f.Confidence != 0.7 {
		t.Errorf("Confidence = %f, want 0.7 (JWT heuristic)", f.Confidence)
	}
	if f.ConfidenceReason != domain.ReasonHeuristicPattern {
		t.Errorf("Reason = %q, want heuristic_pattern", f.ConfidenceReason)
	}
}

// --- negative: env-var placeholder is safe ---------------------------------

func TestSecretInPromptTemplate_PlaceholderEnvVar_Safe(t *testing.T) {
	g := makePromptSecretGraph(map[string]any{
		"model":         "gpt-4o-mini",
		"system_prompt": "Authorization: Bearer ${OPENAI_API_KEY}",
	})
	findings := NewSecretInPromptTemplate().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for ${ENV_VAR} placeholder, got %d: %+v", len(findings), findings)
	}
}

func TestSecretInPromptTemplate_PlaceholderMustache_Safe(t *testing.T) {
	g := makePromptSecretGraph(map[string]any{
		"model":         "gpt-4o-mini",
		"system_prompt": "Use {{ env.ANTHROPIC_API_KEY }} for routing",
	})
	findings := NewSecretInPromptTemplate().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for {{ env.X }} placeholder, got %d", len(findings))
	}
}

// --- negative: generic `prompt` key is NOT inspected (overlap with secret_exposure) -

func TestSecretInPromptTemplate_GenericPromptKeyIgnored(t *testing.T) {
	g := makePromptSecretGraph(map[string]any{
		"model":  "gpt-4o-mini",
		"prompt": "Use sk-abcdefghijklmnopqrstuvwxyz1234567890 for auth",
	})
	findings := NewSecretInPromptTemplate().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings on generic 'prompt' key (covered by secret_exposure_scanner), got %d", len(findings))
	}
}

// --- negative: non-LLM node ignored ----------------------------------------

func TestSecretInPromptTemplate_NonLLMNode_Skipped(t *testing.T) {
	nodes := map[string]*domain.Node{
		"t1": {
			ID:   "t1",
			Type: domain.NodeTypeTool,
			Config: map[string]any{
				"system_prompt": "key sk-abcdefghijklmnopqrstuvwxyz1234567890",
			},
		},
	}
	g := &domain.WorkflowGraph{Nodes: nodes, EntryNodeID: "t1"}
	findings := NewSecretInPromptTemplate().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings on non-LLM node, got %d", len(findings))
	}
}

// --- edge: empty config / missing keys --------------------------------------

func TestSecretInPromptTemplate_NoTemplateKeys_NoFinding(t *testing.T) {
	g := makePromptSecretGraph(map[string]any{
		"model": "gpt-4o-mini",
	})
	findings := NewSecretInPromptTemplate().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when no template keys present, got %d", len(findings))
	}
}

// --- edge: multiple template keys with secrets — one finding per key -------

func TestSecretInPromptTemplate_MultipleKeysOneFinding(t *testing.T) {
	g := makePromptSecretGraph(map[string]any{
		"model":         "gpt-4o-mini",
		"system_prompt": "key sk-abcdefghijklmnopqrstuvwxyz1234567890",
		"instruction":   "fallback AKIAIOSFODNN7EXAMPLE",
	})
	findings := NewSecretInPromptTemplate().Analyze(g)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings (one per template key), got %d: %+v", len(findings), findings)
	}
}

// --- edge: same template, multiple patterns — only one finding emitted -----

func TestSecretInPromptTemplate_SameKeyMultiPattern_DedupOnce(t *testing.T) {
	// AWS key + OpenAI key in the same field. Implementation emits only the
	// first matching pattern per (node, key) to avoid noise.
	g := makePromptSecretGraph(map[string]any{
		"model":         "gpt-4o-mini",
		"system_prompt": "AKIAIOSFODNN7EXAMPLE and also sk-abcdefghijklmnopqrstuvwxyz1234567890",
	})
	findings := NewSecretInPromptTemplate().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (dedup per key), got %d", len(findings))
	}
}

// --- nil / empty graph guard ------------------------------------------------

func TestSecretInPromptTemplate_NilGraph_NoFinding(t *testing.T) {
	findings := NewSecretInPromptTemplate().Analyze(nil)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for nil graph, got %d", len(findings))
	}
}

// --- ConfidenceReason stamp -------------------------------------------------

func TestSecretInPromptTemplate_AllFindingsHaveReason(t *testing.T) {
	cases := []map[string]any{
		{"model": "gpt-4o-mini", "system_prompt": "sk-abcdefghijklmnopqrstuvwxyz1234567890"},
		{"model": "gpt-4o-mini", "instruction": "AKIAIOSFODNN7EXAMPLE"},
		{"model": "gpt-4o-mini", "user_message_template": "ghp_abcdefghijklmnopqrstuvwxyz0123456789AB"},
	}
	for i, cfg := range cases {
		g := makePromptSecretGraph(cfg)
		findings := NewSecretInPromptTemplate().Analyze(g)
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

// --- redaction in message ---------------------------------------------------

func TestSecretInPromptTemplate_MessageIsRedacted(t *testing.T) {
	g := makePromptSecretGraph(map[string]any{
		"model":         "gpt-4o-mini",
		"system_prompt": "key sk-abcdefghijklmnopqrstuvwxyz1234567890",
	})
	findings := NewSecretInPromptTemplate().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	// The full secret should NOT appear in Suggestion (privacy / log-safety):
	if strings.Contains(findings[0].Suggestion, "sk-abcdefghijklmnopqrstuvwxyz1234567890") {
		t.Errorf("Suggestion leaks full secret: %s", findings[0].Suggestion)
	}
	// But a redacted prefix with *** should appear so the reviewer can
	// correlate the finding with their source:
	if !strings.Contains(findings[0].Suggestion, "***") {
		t.Errorf("Suggestion missing redacted token: %s", findings[0].Suggestion)
	}
}
