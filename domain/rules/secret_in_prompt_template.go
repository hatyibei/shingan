package rules

import (
	"fmt"
	"regexp"

	"github.com/hatyibei/shingan/domain"
)

// SecretInPromptTemplate flags LLM nodes whose prompt-template fields
// (`system_prompt`, `prompt_template`, `user_message_template`,
// `instruction`) contain a hardcoded credential or PEM block. Unlike the
// broader `secret_exposure_scanner` (which scans every Config string on
// every node), this rule narrows in on prompt templates because that is
// where secrets are pasted in by mistake during prompt-engineering
// iterations and propagate quickly into logs / inference traces.
//
// Tier: Local (ADR-007) — decision per node, fits the 1-walk dispatcher.
//
// ConfidenceReason:
//   - exact_static_match for AWS / OpenAI / Anthropic / GitHub / PEM
//     patterns (precise regex, very low false-positive rate).
//   - heuristic_pattern for JWT (broad shape, may match coincidental
//     base64 strings).
//
// Detection scope (intentionally narrow): only the four template keys
// listed above are inspected, NOT the generic `prompt` key. The generic
// `prompt` key is already covered by `secret_exposure_scanner`'s OnAny
// recursive scan; restricting this rule prevents duplicate findings while
// keeping the prompt-specific Suggestion (env-var substitution + rotation)
// available where it matters most.
//
// Environment-variable / placeholder references (`${VAR}`, `{{VAR}}`,
// `process.env.X`, `os.Getenv(...)`) are stripped before pattern matching
// so a template like `"Authorization: Bearer ${API_KEY}"` does not fire.
type SecretInPromptTemplate struct{}

// NewSecretInPromptTemplate returns a ready-to-use checker.
func NewSecretInPromptTemplate() *SecretInPromptTemplate {
	return &SecretInPromptTemplate{}
}

// Name returns the unique rule identifier.
func (s *SecretInPromptTemplate) Name() string {
	return "secret_in_prompt_template"
}

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (s *SecretInPromptTemplate) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     s.Name(),
		Severity: domain.Critical,
		Fixable:  false,
	}
}

// Listener implements domain.LocalRule. Only LLM nodes are inspected.
func (s *SecretInPromptTemplate) Listener(ctx *domain.RuleContext) domain.Listener {
	return domain.Listener{
		OnNode: map[domain.NodeType]domain.NodeHandler{
			domain.NodeTypeLLM: func(c *domain.RuleContext, n *domain.Node) {
				for _, f := range evaluateSecretInPromptTemplate(n) {
					c.Report(f)
				}
			},
		},
	}
}

// Analyze keeps the legacy AnalysisRule contract alive.
func (s *SecretInPromptTemplate) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	var findings []domain.Finding
	for _, node := range graph.Nodes {
		if node.Type != domain.NodeTypeLLM {
			continue
		}
		findings = append(findings, evaluateSecretInPromptTemplate(node)...)
	}
	return findings
}

// promptTemplateKeys is the narrow set of Config keys this rule scans.
// Generic `prompt` is intentionally omitted — `secret_exposure_scanner`
// already walks every Config value via OnAny and would duplicate findings.
var promptTemplateKeys = []string{
	"system_prompt",
	"prompt_template",
	"user_message_template",
	"instruction",
}

// promptSecretPattern bundles the metadata each detectable secret category
// carries: human-readable label, compiled regex, severity, confidence, and
// confidence reason.
type promptSecretPattern struct {
	name       string
	pattern    *regexp.Regexp
	severity   domain.Severity
	confidence float64
	reason     domain.ConfidenceReason
}

// promptSecretPatterns is the ordered list of patterns inspected per
// template key. Order matters: more-specific patterns (sk-ant-) come
// before broader ones (sk-) so a single template hit is classified as the
// most specific category.
var promptSecretPatterns = []promptSecretPattern{
	{
		name:       "aws_access_key",
		pattern:    regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		severity:   domain.Critical,
		confidence: 0.95,
		reason:     domain.ReasonExactStaticMatch,
	},
	{
		name:       "private_key_pem",
		pattern:    regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
		severity:   domain.Critical,
		confidence: 0.95,
		reason:     domain.ReasonExactStaticMatch,
	},
	{
		// Must come before openai_api_key because sk-ant- is a sub-pattern of sk-
		name:       "anthropic_api_key",
		pattern:    regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{20,}`),
		severity:   domain.Critical,
		confidence: 0.95,
		reason:     domain.ReasonExactStaticMatch,
	},
	{
		name:       "openai_api_key",
		pattern:    regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),
		severity:   domain.Critical,
		confidence: 0.95,
		reason:     domain.ReasonExactStaticMatch,
	},
	{
		name:       "github_token",
		pattern:    regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36,}`),
		severity:   domain.Critical,
		confidence: 0.95,
		reason:     domain.ReasonExactStaticMatch,
	},
	{
		name:       "jwt",
		pattern:    regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),
		severity:   domain.Warning,
		confidence: 0.7,
		reason:     domain.ReasonHeuristicPattern,
	},
}

// evaluateSecretInPromptTemplate returns one Finding per (node, key,
// pattern) triple where a hardcoded credential is detected after env-var
// substitution stripping. Nodes without any prompt-template field are
// skipped silently.
func evaluateSecretInPromptTemplate(n *domain.Node) []domain.Finding {
	if n == nil || n.Config == nil {
		return nil
	}
	var findings []domain.Finding
	for _, key := range promptTemplateKeys {
		val := stringConfig(n, key)
		if val == "" {
			continue
		}

		// Strip placeholder references before matching so legitimate
		// env-var-driven templates like "${API_KEY}" or
		// "{{ env.OPENAI_API_KEY }}" do not trip the rule.
		stripped := placeholderPattern.ReplaceAllString(val, "")

		for _, p := range promptSecretPatterns {
			loc := p.pattern.FindStringIndex(stripped)
			if loc == nil {
				continue
			}
			redacted := redactSecret(stripped[loc[0]:loc[1]])
			findings = append(findings, domain.Finding{
				RuleName: "secret_in_prompt_template",
				Severity: p.severity,
				NodeID:   n.ID,
				Message: fmt.Sprintf(
					"node %q config[%q] contains a hardcoded %s (%s)",
					n.ID, key, p.name, redacted,
				),
				Suggestion: fmt.Sprintf(
					"Hardcoded credential detected inside an LLM prompt template (%s). Anyone with access to the workflow definition (logs, exports, repository) sees the secret in plaintext. Move to environment-variable substitution (e.g. ${API_KEY} or {{ENV_VAR}}) and rotate the leaked credential.",
					redacted,
				),
				Confidence:       p.confidence,
				ConfidenceReason: p.reason,
			})
			// Emit at most one Finding per (node, key) so a single
			// template that matches multiple patterns does not double-count.
			break
		}
	}
	return findings
}

// redactSecret returns a short, safe-to-display prefix of the matched
// secret so reviewers can locate it in source without copy-pasting the
// entire value into a bug ticket.
func redactSecret(s string) string {
	const keep = 6
	if len(s) <= keep {
		return s + "***"
	}
	return s[:keep] + "***"
}

func init() {
	registerBuiltin(NewSecretInPromptTemplate())
}
