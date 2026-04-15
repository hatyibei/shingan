package rules

import (
	"fmt"
	"regexp"

	"github.com/hatyibei/shingan/domain"
)

// SecretExposureScanner detects API keys, tokens, and other secrets that may be
// hardcoded in Node.Config fields such as "prompt", "prompt_template", "instruction",
// "api_key", and "headers".
//
// Detection strategy:
//  1. Walk every Config value (string / map / slice) recursively.
//  2. Skip values that are purely environment-variable or placeholder references
//     (e.g. ${VAR}, {{secret}}, process.env.X, os.Getenv()).
//  3. Match against a ranked list of secret patterns.
//  4. Emit at most one Finding per (node, config-key) to avoid duplicates.
//
// Severity assignment:
//   - AWS access key, GCP/OpenAI/Anthropic private keys  → Critical
//   - GitHub token, Slack bot token                       → Warning
//   - JWT, generic "password=…"/"token=…" patterns       → Info
type SecretExposureScanner struct{}

// NewSecretExposureScanner returns a ready-to-use SecretExposureScanner.
func NewSecretExposureScanner() *SecretExposureScanner {
	return &SecretExposureScanner{}
}

// Name returns the unique rule identifier.
func (s *SecretExposureScanner) Name() string {
	return "secret_exposure_scanner"
}

// secretPattern associates a human-readable name, compiled regex, and severity
// with each category of detectable secret.
type secretPattern struct {
	name     string
	pattern  *regexp.Regexp
	severity domain.Severity
}

// secretPatterns is the ordered list of detectable secret categories.
// The order matters: more-specific patterns (e.g. sk-ant-) come before
// broader ones (e.g. sk-) so a single string matches the most-specific category.
var secretPatterns = []secretPattern{
	{
		name:     "aws_access_key",
		pattern:  regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		severity: domain.Critical,
	},
	{
		name:     "private_key_pem",
		pattern:  regexp.MustCompile(`-----BEGIN (RSA )?PRIVATE KEY-----`),
		severity: domain.Critical,
	},
	{
		// Must come before openai_api_key because sk-ant- is a sub-pattern of sk-
		name:     "anthropic_api_key",
		pattern:  regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{20,}`),
		severity: domain.Critical,
	},
	{
		name:     "openai_api_key",
		pattern:  regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),
		severity: domain.Critical,
	},
	{
		name:     "github_token",
		pattern:  regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36,}`),
		severity: domain.Warning,
	},
	{
		name:     "slack_token",
		pattern:  regexp.MustCompile(`xox[bpars]-[A-Za-z0-9-]{10,}`),
		severity: domain.Warning,
	},
	{
		name:     "jwt",
		pattern:  regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),
		severity: domain.Info,
	},
	{
		// Generic credential assignment: password=XXX, api_key=XXX, token=XXX (20+ chars)
		name:     "generic_secret",
		pattern:  regexp.MustCompile(`(?i)(password|secret|api_key|apikey|token)\s*[:=]\s*['"]?[A-Za-z0-9_\-]{20,}`),
		severity: domain.Info,
	},
}

// placeholderPattern matches common safe references to external secrets:
//   - Shell-style env vars:  $VAR, ${VAR}
//   - Template placeholders: {{secret}}, {{ env.TOKEN }}
//   - Node.js env refs:      process.env.VAR_NAME
//   - Go env refs:           os.Getenv(
var placeholderPattern = regexp.MustCompile(
	`\$\{?[A-Z_][A-Z0-9_]*\}?|` +
		`\{\{[^}]+\}\}|` +
		`process\.env\.[A-Z_][A-Z0-9_]*|` +
		`os\.Getenv\(`,
)

// Analyze iterates over all nodes and their Config entries, scanning string
// values (and nested map/slice values) for embedded secrets.
func (s *SecretExposureScanner) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}

	var findings []domain.Finding
	for _, node := range graph.Nodes {
		for key, val := range node.Config {
			s.scanValue(node, key, val, &findings)
		}
	}
	return findings
}

// scanValue dispatches to the appropriate handler based on the runtime type of val.
func (s *SecretExposureScanner) scanValue(node *domain.Node, key string, val any, findings *[]domain.Finding) {
	switch v := val.(type) {
	case string:
		s.scanString(node, key, v, findings)
	case map[string]any:
		for subKey, subVal := range v {
			s.scanValue(node, key+"."+subKey, subVal, findings)
		}
	case []any:
		for i, item := range v {
			s.scanValue(node, fmt.Sprintf("%s[%d]", key, i), item, findings)
		}
	}
}

// scanString checks a single string value for secret patterns.
// If the value consists entirely of safe placeholder references and the
// remaining text (after stripping placeholders) contains no secret pattern,
// it is skipped — avoiding false positives on values like "${API_KEY}".
func (s *SecretExposureScanner) scanString(node *domain.Node, key, val string, findings *[]domain.Finding) {
	if len(val) == 0 {
		return
	}

	// If the raw value contains placeholder references, check whether removing
	// them still reveals an embedded literal secret.  If no literal secret
	// survives after stripping, treat the value as safe.
	if placeholderPattern.MatchString(val) {
		if !hasActualSecret(val) {
			return
		}
	}

	for _, p := range secretPatterns {
		if p.pattern.MatchString(val) {
			// Confidence by severity: Critical/Warning patterns use precise regexes (0.95),
			// Info patterns (jwt, generic_secret) are broader heuristics (0.5).
			confidence := 0.95
			if p.severity == domain.Info {
				confidence = 0.5
			}
			*findings = append(*findings, domain.Finding{
				RuleName: "secret_exposure_scanner",
				Severity: p.severity,
				NodeID:   node.ID,
				Message: fmt.Sprintf(
					"node %q config[%q] contains potential %s",
					node.ID, key, p.name,
				),
				Suggestion: "Secrets should be injected via environment variables or a secret manager at runtime, never hardcoded in workflow configuration.",
				Confidence: confidence,
			})
			// Emit at most one Finding per config key to avoid duplicate noise
			// when multiple patterns match the same value.
			return
		}
	}
}

// hasActualSecret returns true if val contains a secret pattern even after all
// placeholder references have been removed.  This handles the edge case where a
// string mixes a placeholder with a literal secret, e.g.
// "sk-abc123...${SUFFIX}" — the prefix alone is still a secret.
func hasActualSecret(val string) bool {
	stripped := placeholderPattern.ReplaceAllString(val, "")
	for _, p := range secretPatterns {
		if p.pattern.MatchString(stripped) {
			return true
		}
	}
	return false
}
