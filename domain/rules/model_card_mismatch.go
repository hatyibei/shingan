package rules

import (
	"fmt"
	"net/url"
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
//
// Codex iter7 P1: when both `provider` and `base_url` are present we now
// validate them independently. Previously a matching `provider` short-
// circuited the check, so a config with `provider="openai"` and
// `base_url="https://api.anthropic.com/v1"` slipped through silently —
// exactly the runtime miswire this rule is supposed to catch.
//
// Codex iter7 P2: `base_url` host classification now uses url.Parse +
// host equality / suffix matching instead of strings.Contains across the
// whole URL. This rejects spoofed lookalikes like
// `https://api.openai.com.proxy.corp/v1` that previously slipped past
// the substring matcher.
func evaluateModelCardMismatch(node *domain.Node) (domain.Finding, bool) {
	model := stringConfig(node, "model")
	if model == "" {
		return domain.Finding{}, false
	}

	expected, known := lookupProvider(model)
	providerStr := stringConfig(node, "provider")
	baseURL := stringConfig(node, "base_url")

	// Sources are evaluated independently so neither hides the other
	// when both are set (Codex iter7 P1).
	providerKey, providerHasSignal := providerFromExplicit(providerStr)
	urlKey, urlHasSignal := providerFromBaseURL(baseURL)

	// No endpoint clue at all — cannot judge.
	if !providerHasSignal && !urlHasSignal {
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

	// Known prefix: each present signal must agree with `expected`.
	// `provider` takes the lead description when both are set, but a
	// matching provider no longer hides a contradicting *recognised*
	// base_url (Codex iter7 P1).
	//
	// An unknown base_url host (urlKey == "") is treated as benign:
	// custom proxies, Azure OpenAI deployments, Vertex AI endpoints
	// and self-hosted gateways all surface as unknown hosts and would
	// otherwise produce false positives whenever they're used with the
	// canonical provider value.
	providerOK := !providerHasSignal || providerKey == expected
	urlOK := !urlHasSignal || urlKey == "" || urlKey == expected

	if providerOK && urlOK {
		return domain.Finding{}, false
	}

	// Mismatch: build a Critical finding. Phrase the message based on
	// which signal disagreed so the operator knows what to fix.
	var (
		actual string
		source string
	)
	switch {
	case !providerOK && !urlOK:
		actual = fmt.Sprintf("%s (provider) / %s (base_url)", providerStr, baseURL)
		source = "provider+base_url"
	case !providerOK:
		actual = providerStr
		source = "provider"
	default: // !urlOK
		actual = baseURL
		source = "base_url"
	}

	return domain.Finding{
		RuleName: "model_card_mismatch",
		Severity: domain.Critical,
		NodeID:   node.ID,
		Message: fmt.Sprintf(
			"model %q belongs to provider %q but %s is set to %q; the runtime call will fail.",
			model, expected, source, actual,
		),
		Suggestion: fmt.Sprintf(
			"Model %q belongs to provider %q but %s is set to %q. Either update the model name or the endpoint to match.",
			model, expected, source, actual,
		),
		Confidence:       1.0,
		ConfidenceReason: domain.ReasonExactStaticMatch,
	}, true
}

// providerFromExplicit returns the provider key implied by an explicit
// Config["provider"] value. Returns false on the second value when the
// field is empty (no signal).
func providerFromExplicit(providerStr string) (providerKey, bool) {
	if providerStr == "" {
		return "", false
	}
	key := strings.ToLower(strings.TrimSpace(providerStr))
	if p, ok := providerAliases[key]; ok {
		return p, true
	}
	// Unrecognised alias surfaces as itself so downstream comparison
	// against `expected` fails (and the user sees the offending value).
	return providerKey(key), true
}

// providerFromBaseURL parses Config["base_url"] and matches its host
// against the provider table. Codex iter7 P2: parses with net/url and
// checks the host (exact match or suffix on a label boundary) rather
// than substring-matching the raw URL string, which rejects lookalikes
// like `https://api.openai.com.proxy.corp/v1` that previously matched.
//
// Returns (key, true) when the URL is recognised; (key, true) with an
// empty key when the URL is well-formed but its host is unknown (so the
// caller knows a base_url was supplied); (empty, false) when no URL
// was set.
func providerFromBaseURL(baseURL string) (providerKey, bool) {
	if baseURL == "" {
		return "", false
	}
	host := extractHost(baseURL)
	if host == "" {
		return "", true // ill-formed URL — treat as "user supplied an endpoint" with no provider match
	}
	host = strings.ToLower(host)
	for p, hosts := range providerHosts {
		for _, h := range hosts {
			h = strings.ToLower(h)
			if hostMatches(host, h) {
				return p, true
			}
		}
	}
	return "", true
}

// hostMatches returns true when host equals candidate or host ends with
// "." + candidate (label-boundary suffix). This rejects spoofed
// lookalikes such as "api.openai.com.proxy.corp" matching "api.openai.com"
// (Codex iter7 P2).
func hostMatches(host, candidate string) bool {
	if host == candidate {
		return true
	}
	// Allow legitimate sub-domains (e.g. `*.openai.azure.com` matches
	// `westus.api.cognitive.microsoft.com.openai.azure.com`'s suffix
	// rules) while rejecting non-label-boundary substring noise.
	if strings.HasSuffix(host, "."+candidate) {
		return true
	}
	// Wildcard candidate of the form "*.example.com" matches any host
	// ending in ".example.com" (and the bare suffix).
	if strings.HasPrefix(candidate, "*.") {
		bare := candidate[2:]
		return host == bare || strings.HasSuffix(host, "."+bare)
	}
	return false
}

// extractHost returns the host of the given URL, or "" if parsing
// fails. Accepts inputs without a scheme (treated as opaque host) since
// some users write `Config["base_url"] = "api.openai.com"` without
// `https://`.
func extractHost(raw string) string {
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		// Hostname() strips the port (Codex iter8 P2): u.Host on
		// "https://api.openai.com:443/v1" is "api.openai.com:443"
		// which would never match the providerHosts table.
		return u.Hostname()
	}
	// No scheme — strip any path / port and return the leading authority.
	if i := strings.IndexAny(raw, "/?#"); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.LastIndex(raw, ":"); i >= 0 {
		raw = raw[:i]
	}
	return raw
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

// (classifyConfiguredProvider removed in iter7 — replaced by the
// independently-evaluated providerFromExplicit + providerFromBaseURL
// pair so a matching `provider` no longer hides a contradicting
// `base_url`.)

func init() {
	registerBuiltin(NewModelCardMismatchChecker())
}
