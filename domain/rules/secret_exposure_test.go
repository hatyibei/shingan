package rules_test

import (
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/domain/testutil"
)

// buildSecret is a local helper that builds a WorkflowGraph from a Builder,
// failing the test immediately if Build() returns an error.
func buildSecret(t *testing.T, b *testutil.Builder) *domain.WorkflowGraph {
	t.Helper()
	g, err := b.Build()
	if err != nil {
		t.Fatalf("testutil.Builder.Build() error: %v", err)
	}
	return g
}

// helper: return all findings from the scanner for a given graph.
func scanSecrets(g *domain.WorkflowGraph) []domain.Finding {
	return rules.NewSecretExposureScanner().Analyze(g)
}

// ─── Case 1: AWS Access Key → Critical ───────────────────────────────────────

func TestSecretExposure_AWSKey_Critical(t *testing.T) {
	g := buildSecret(t, testutil.NewBuilder().
		AddNodeWithConfig("n1", domain.NodeTypeLLM, map[string]any{
			"prompt": "Use key AKIAIOSFODNN7EXAMPLE for AWS access",
		}).
		Entry("n1"))

	findings := scanSecrets(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", f.Severity)
	}
	if f.RuleName != "secret_exposure_scanner" {
		t.Errorf("RuleName = %q, want secret_exposure_scanner", f.RuleName)
	}
	if f.NodeID != "n1" {
		t.Errorf("NodeID = %q, want n1", f.NodeID)
	}
}

// ─── Case 2: OpenAI API Key → Critical ───────────────────────────────────────

func TestSecretExposure_OpenAIKey_Critical(t *testing.T) {
	g := buildSecret(t, testutil.NewBuilder().
		AddNodeWithConfig("llm", domain.NodeTypeLLM, map[string]any{
			"api_key": "sk-abcdefghijklmnopqrstuvwxyz1234567890",
		}).
		Entry("llm"))

	findings := scanSecrets(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", findings[0].Severity)
	}
}

// ─── Case 3: Anthropic API Key → Critical ────────────────────────────────────

func TestSecretExposure_AnthropicKey_Critical(t *testing.T) {
	g := buildSecret(t, testutil.NewBuilder().
		AddNodeWithConfig("agent", domain.NodeTypeLLM, map[string]any{
			"instruction": "Use key sk-ant-api01-abcdefghijklmnopqrstuvwxyz1234567890 to call Claude",
		}).
		Entry("agent"))

	findings := scanSecrets(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", findings[0].Severity)
	}
}

// ─── Case 4: GitHub Token → Warning ──────────────────────────────────────────

func TestSecretExposure_GitHubToken_Warning(t *testing.T) {
	g := buildSecret(t, testutil.NewBuilder().
		AddNodeWithConfig("tool", domain.NodeTypeTool, map[string]any{
			"prompt": "token: ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ1234567890",
		}).
		Entry("tool"))

	findings := scanSecrets(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Warning {
		t.Errorf("Severity = %v, want Warning", findings[0].Severity)
	}
}

// ─── Case 5: Slack Bot Token → Warning ───────────────────────────────────────

func TestSecretExposure_SlackToken_Warning(t *testing.T) {
	g := buildSecret(t, testutil.NewBuilder().
		AddNodeWithConfig("notifier", domain.NodeTypeTool, map[string]any{
			"prompt_template": "post via xoxb-1234567890-abcdefghijklmnop",
		}).
		Entry("notifier"))

	findings := scanSecrets(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Warning {
		t.Errorf("Severity = %v, want Warning", findings[0].Severity)
	}
}

// ─── Case 6: Environment variable reference → no finding ─────────────────────

func TestSecretExposure_EnvVarReference_NoFinding(t *testing.T) {
	g := buildSecret(t, testutil.NewBuilder().
		AddNodeWithConfig("safe", domain.NodeTypeLLM, map[string]any{
			"api_key": "${API_KEY}",
			"prompt":  "Use ${OPENAI_KEY} for auth",
		}).
		Entry("safe"))

	findings := scanSecrets(g)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// ─── Case 7: Template placeholder → no finding ───────────────────────────────

func TestSecretExposure_TemplatePlaceholder_NoFinding(t *testing.T) {
	g := buildSecret(t, testutil.NewBuilder().
		AddNodeWithConfig("template_node", domain.NodeTypeLLM, map[string]any{
			"prompt":  "authenticate with {{secret}}",
			"api_key": "{{api_key}}",
		}).
		Entry("template_node"))

	findings := scanSecrets(g)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// ─── Case 8: Nested config map (headers.Authorization) → Critical ────────────

func TestSecretExposure_NestedHeadersMap_Critical(t *testing.T) {
	g := buildSecret(t, testutil.NewBuilder().
		AddNodeWithConfig("http_node", domain.NodeTypeTool, map[string]any{
			"headers": map[string]any{
				"Authorization": "Bearer sk-abcdefghijklmnopqrstuvwxyz1234567890",
				"Content-Type":  "application/json",
			},
		}).
		Entry("http_node"))

	findings := scanSecrets(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", findings[0].Severity)
	}
}

// ─── Case 9: Array item containing secret → Critical ─────────────────────────

func TestSecretExposure_ArrayItem_Critical(t *testing.T) {
	g := buildSecret(t, testutil.NewBuilder().
		AddNodeWithConfig("multi_prompt", domain.NodeTypeLLM, map[string]any{
			"prompts": []any{
				"hello, how can I help?",
				"sk-abcdefghijklmnopqrstuvwxyz1234567890 is the auth key",
			},
		}).
		Entry("multi_prompt"))

	findings := scanSecrets(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Critical {
		t.Errorf("Severity = %v, want Critical", findings[0].Severity)
	}
}

// ─── Case 10: Empty / nil graph → no panic, no findings ──────────────────────

func TestSecretExposure_EmptyGraph_NoFindings(t *testing.T) {
	scanner := rules.NewSecretExposureScanner()

	if findings := scanner.Analyze(nil); len(findings) != 0 {
		t.Errorf("nil graph: expected 0 findings, got %d", len(findings))
	}

	emptyGraph := &domain.WorkflowGraph{Nodes: map[string]*domain.Node{}}
	if findings := scanner.Analyze(emptyGraph); len(findings) != 0 {
		t.Errorf("empty graph: expected 0 findings, got %d", len(findings))
	}
}

// ─── Case 11: Name check ──────────────────────────────────────────────────────

func TestSecretExposure_Name(t *testing.T) {
	if got := rules.NewSecretExposureScanner().Name(); got != "secret_exposure_scanner" {
		t.Errorf("Name() = %q, want secret_exposure_scanner", got)
	}
}
