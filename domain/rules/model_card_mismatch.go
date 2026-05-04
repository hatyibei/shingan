package rules

import (
	"fmt"
	"strings"

	"github.com/hatyibei/shingan/domain"
)

// providerKey is the canonical identifier for a model provider. It matches the
// strings users typically write in `Config["provider"]`, lower-cased.
type providerKey string

const (
	providerOpenAI    providerKey = "openai"
	providerAnthropic providerKey = "anthropic"
	providerGoogle    providerKey = "google"
)

// modelPrefixToProvider maps the leading token of a known model name to its
// owning provider. If the prefix is not in this table the rule treats the
// model as `unknown` and degrades to the heuristic Info path.
var modelPrefixToProvider = []struct {
	prefix   string
	provider providerKey
}{
	{"gpt-", providerOpenAI},
	{"o1-", providerOpenAI},
	{"o1", providerOpenAI},
	{"text-davinci", providerOpenAI},
	{"text-embedding", providerOpenAI},
	{"claude-", providerAnthropic},
	{"claude", providerAnthropic},
	{"gemini-", providerGoogle},
	{"gemini", providerGoogle},
	{"text-bison", providerGoogle},
	{"chat-bison", providerGoogle},
}

// providerHosts lists URL substrings that uniquely identify a provider's
// public endpoints. Azure OpenAI and Google Vertex AI are treated as the
// same provider as their model owner so legitimate proxies do not flag.
var providerHosts = map[providerKey][]string{
	providerOpenAI: {
		"api.openai.com",
		"openai.azure.com", // Azure OpenAI matches the *.openai.azure.com pattern
	},
	providerAnthropic: {
		"api.anthropic.com",
	},
	providerGoogle: {
		"generativelanguage.googleapis.com",
		"aiplatform.googleapis.com", // Vertex AI
	},
}

// providerAliases maps the strings users write in `Config["provider"]` (case
// insensitive) onto the canonical provider key.
var providerAliases = map[string]providerKey{
	"openai":         providerOpenAI,
	"azure":          providerOpenAI,
	"azure-openai":   providerOpenAI,
	"anthropic":      providerAnthropic,
	"claude":         providerAnthropic,
	"google":         providerGoogle,
	"google-vertex":  providerGoogle,
	"vertex":         providerGoogle,
	"vertexai":       providerGoogle,
	"gemini":         providerGoogle,
	"genai":          providerGoogle,
	"google-genai":   providerGoogle,
	"generativelang": providerGoogle,
}

// ModelCardMismatchChecker detects LLM nodes whose declared model name
// disagrees with the configured `base_url` or `provider`. A mismatch on a
// known prefix means the request will fail at runtime (the wrong API will
// reject the model name).
//
// Tier: Local (ADR-007) — decision per node, fits the 1-walk dispatcher.
//
// Severity rules:
//   - Known prefix + provider/url that disagrees → Critical, Confidence 1.0,
//     ReasonExactStaticMatch (the runtime call WILL fail).
//   - Unknown prefix + an explicit provider config → Info, Confidence 0.4,
//     ReasonHeuristicPattern (knowledge gap; surfaced so reviewers can decide
//     whether to extend the table).
//   - Unknown prefix without provider config → no finding (silent).
//   - Provider matches model prefix (even with a custom base_url) → no
//     finding (legitimate proxy / self-hosted compatible endpoint).
type ModelCardMismatchChecker struct{}

// NewModelCardMismatchChecker returns a ready-to-use checker.
func NewModelCardMismatchChecker() *ModelCardMismatchChecker {
	return &ModelCardMismatchChecker{}
}

// Name returns the unique rule identifier.
func (m *ModelCardMismatchChecker) Name() string {
	return "model_card_mismatch"
}

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (m *ModelCardMismatchChecker) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     m.Name(),
		Severity: domain.Critical,
		Fixable:  true,
	}
}

// Listener implements domain.LocalRule. Only LLM nodes are inspected.
func (m *ModelCardMismatchChecker) Listener(ctx *domain.RuleContext) domain.Listener {
	return domain.Listener{
		OnNode: map[domain.NodeType]domain.NodeHandler{
			domain.NodeTypeLLM: func(c *domain.RuleContext, n *domain.Node) {
				if f, ok := evaluateModelCardMismatch(n); ok {
					c.Report(f)
				}
			},
		},
	}
}

// Analyze keeps the legacy AnalysisRule contract alive.
func (m *ModelCardMismatchChecker) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	var findings []domain.Finding
	for _, node := range graph.Nodes {
		if node.Type != domain.NodeTypeLLM {
			continue
		}
		if f, ok := evaluateModelCardMismatch(node); ok {
			findings = append(findings, f)
		}
	}
	return findings
}

// evaluateModelCardMismatch encodes the severity table.
func evaluateModelCardMismatch(node *domain.Node) (domain.Finding, bool) {
	model := stringConfig(node, "model")
	if model == "" {
		return domain.Finding{}, false
	}

	expected, known := lookupProvider(model)
	providerStr := stringConfig(node, "provider")
	baseURL := stringConfig(node, "base_url")

	// Determine what the configured endpoint claims about the provider.
	configuredProvider, providerSource := classifyConfiguredProvider(providerStr, baseURL)

	// No endpoint clue at all — cannot judge.
	if providerSource == "" {
		return domain.Finding{}, false
	}

	if !known {
		// Unknown prefix: only emit Info when the user wrote an explicit
		// provider string. URL alone is not enough — too many self-hosted
		// inference endpoints make that noisy.
		if providerStr != "" {
			return domain.Finding{
				RuleName: "model_card_mismatch",
				Severity: domain.Info,
				NodeID:   node.ID,
				Message: fmt.Sprintf(
					"node %q declares model %q with provider %q, but model prefix is not in Shingan's known-provider table.",
					node.ID, model, providerStr,
				),
				Suggestion: fmt.Sprintf(
					"Verify provider %q owns model %q. If correct, this rule's table needs to be extended; if not, update either the model name or the provider/base_url.",
					providerStr, model,
				),
				Confidence:       0.4,
				ConfidenceReason: domain.ReasonHeuristicPattern,
			}, true
		}
		return domain.Finding{}, false
	}

	// Known prefix: provider takes precedence over base_url. If `provider`
	// matches the expected one we accept the config even if base_url points
	// to a custom proxy (legitimate self-host scenario).
	if configuredProvider == expected {
		return domain.Finding{}, false
	}

	// Mismatch: build a Critical finding. Phrase the message based on which
	// signal disagreed so the operator knows what to fix.
	actual := string(configuredProvider)
	if providerStr != "" && providerSource == "provider" {
		// Provider explicit — quote the user's exact value.
		actual = providerStr
	} else if baseURL != "" {
		actual = baseURL
	}

	return domain.Finding{
		RuleName: "model_card_mismatch",
		Severity: domain.Critical,
		NodeID:   node.ID,
		Message: fmt.Sprintf(
			"model %q belongs to provider %q but %s is set to %q; the runtime call will fail.",
			model, expected, providerSource, actual,
		),
		Suggestion: fmt.Sprintf(
			"Model %q belongs to provider %q but base_url/provider is set to %q. Either update the model name or the endpoint to match.",
			model, expected, actual,
		),
		Confidence:       1.0,
		ConfidenceReason: domain.ReasonExactStaticMatch,
	}, true
}

// lookupProvider returns the canonical provider for a model name. The bool
// is false if the prefix is not in the table.
func lookupProvider(model string) (providerKey, bool) {
	lower := strings.ToLower(model)
	for _, entry := range modelPrefixToProvider {
		if strings.HasPrefix(lower, entry.prefix) {
			return entry.provider, true
		}
	}
	return "", false
}

// classifyConfiguredProvider returns the provider implied by Config["provider"]
// or Config["base_url"] (in that priority order) plus a label naming the
// signal source ("provider" or "base_url"). When neither is set the second
// return value is empty.
func classifyConfiguredProvider(providerStr, baseURL string) (providerKey, string) {
	if providerStr != "" {
		key := strings.ToLower(strings.TrimSpace(providerStr))
		if p, ok := providerAliases[key]; ok {
			return p, "provider"
		}
		// Provider value present but unrecognised — surface it as itself so
		// downstream comparison fails for known model prefixes.
		return providerKey(key), "provider"
	}
	if baseURL != "" {
		host := strings.ToLower(baseURL)
		for p, hosts := range providerHosts {
			for _, h := range hosts {
				if strings.Contains(host, h) {
					return p, "base_url"
				}
			}
		}
		// URL present but no known host substring — return empty key with
		// source so the caller knows the user supplied an endpoint.
		return "", "base_url"
	}
	return "", ""
}

func init() {
	registerBuiltin(NewModelCardMismatchChecker())
}
