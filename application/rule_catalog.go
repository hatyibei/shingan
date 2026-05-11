package application

import (
	"sort"
	"strings"

	"github.com/hatyibei/shingan/domain"
)

// RuleManifest is the declarative, machine-readable descriptor for one
// Shingan rule. It is the foundation for the v0.9 Plugin SDK (ADR-015):
// once external rule authors can ship a Manifest alongside their
// `init()`-registered rule, the same catalog renderer powers the CLI,
// the IDE rule-hover, the SARIF taxonomy block, and a future
// shingan.dev catalog page.
//
// The struct is JSON-serialisable so it can be emitted directly to
// downstream tools. Fields are deliberately additive (`omitempty` on
// the slice fields) so the v0.x → v1.0 evolution can add metadata
// without breaking consumers per the stability commitment in README.
type RuleManifest struct {
	Name        string          `json:"name"`
	Severity    domain.Severity `json:"severity"`
	SeverityStr string          `json:"severity_str"`
	Fixable     bool            `json:"fixable"`
	Description string          `json:"description"`
	Frameworks  []string        `json:"frameworks,omitempty"`
	Tags        []string        `json:"tags,omitempty"`
	Stability   string          `json:"stability"`
	DocsURL     string          `json:"docs_url,omitempty"`
}

// staticRuleMeta holds the metadata that isn't derivable from the rule
// implementation itself (Frameworks / Tags / Stability / DocsURL).
//
// Source of truth: this table. When you add a new rule, append a row
// here AND register the rule via `registerBuiltin` in its `init()`.
// The catalog test enforces 1-to-1 correspondence between this table
// and the registered rules so neither side drifts.
var staticRuleMeta = map[string]struct {
	Frameworks []string
	Tags       []string
	Stability  string
	DocsURL    string
}{
	"cycle_detection":             {[]string{"all"}, []string{"correctness", "safety"}, "stable", "docs/cycle-detection-note.md"},
	"unreachable_node":            {[]string{"all"}, []string{"correctness"}, "stable", ""},
	"error_handler_checker":       {[]string{"all"}, []string{"reliability"}, "stable", ""},
	"cost_estimation":             {[]string{"all"}, []string{"cost"}, "stable", "docs/deprecated-models.md"},
	"redundant_llm_call":          {[]string{"all"}, []string{"cost", "quality"}, "stable", ""},
	"loop_guard":                  {[]string{"adk-go", "samurai", "json"}, []string{"correctness", "safety"}, "stable", ""},
	"pii_leak_scanner":            {[]string{"all"}, []string{"security", "privacy"}, "stable", "docs/pii-detection.md"},
	"secret_exposure_scanner":     {[]string{"all"}, []string{"security"}, "stable", ""},
	"max_parallel_branches":       {[]string{"all"}, []string{"cost", "reliability"}, "stable", "docs/parallel-branches.md"},
	"deprecated_model":            {[]string{"all"}, []string{"correctness", "quality"}, "stable", "docs/deprecated-models.md"},
	"prompt_injection_sink":       {[]string{"all"}, []string{"security"}, "stable", "docs/prompt-injection.md"},
	"retry_storm":                 {[]string{"all"}, []string{"cost", "reliability"}, "stable", ""},
	"unbounded_tool_arg":          {[]string{"all"}, []string{"security", "safety"}, "stable", ""},
	"circular_dep_agents":         {[]string{"crewai", "langgraph"}, []string{"correctness"}, "stable", ""},
	"temperature_misuse":          {[]string{"all"}, []string{"quality"}, "stable", ""},
	"dynamic_node_construction":   {[]string{"langgraph", "crewai"}, []string{"correctness", "security"}, "stable", ""},
	"missing_eval_dataset":        {[]string{"all"}, []string{"quality", "governance"}, "stable", ""},
	"secret_in_prompt_template":   {[]string{"all"}, []string{"security"}, "stable", ""},
	"tool_description_missing":    {[]string{"langgraph", "crewai"}, []string{"quality"}, "stable", ""},
	"eval_missing":                {[]string{"all"}, []string{"governance"}, "stable", ""},
	"model_card_mismatch":         {[]string{"all"}, []string{"correctness"}, "stable", "docs/model-card-mismatch.md"},
	"human_gate_missing":          {[]string{"all"}, []string{"governance", "safety"}, "stable", ""},
}

// ListRuleManifests returns one RuleManifest per rule in `rules`,
// merging the runtime Meta() output (Name / Severity / Fixable) with the
// static table above (Frameworks / Tags / Stability / DocsURL) and the
// first non-empty line of RuleExplanations (Description).
//
// Takes the rule slice as input rather than reaching into infrastructure
// so that Onion is preserved: application depends only on domain.
// Callers (CLI, MCP server, web service) supply the rule slice via
// `infrastructure/factory.NewAnalyzerFactory().CreateAll()`.
//
// The returned slice is sorted by Name so the output is deterministic
// across calls — consumers (CLI, IDE rule list, SARIF) can diff
// catalogs across releases without sort artifacts.
func ListRuleManifests(rules []domain.AnalysisRule) []RuleManifest {
	out := make([]RuleManifest, 0, len(rules))
	for _, r := range rules {
		name := r.Name()
		manifest := RuleManifest{
			Name:        name,
			Description: firstLineSummary(name),
			Stability:   "stable",
		}
		// Severity + Fixable come from rules that implement the new
		// tiered interfaces (LocalRule / PathRule / GlobalRule). Legacy
		// AnalysisRule-only implementations don't expose Meta() — we
		// surface Info severity in that case so the catalog stays
		// complete rather than silently dropping the rule.
		if mp, ok := r.(metaProvider); ok {
			m := mp.Meta()
			manifest.Severity = m.Severity
			manifest.Fixable = m.Fixable
		} else {
			manifest.Severity = domain.Info
		}
		manifest.SeverityStr = manifest.Severity.String()
		if static, ok := staticRuleMeta[name]; ok {
			manifest.Frameworks = static.Frameworks
			manifest.Tags = static.Tags
			if static.Stability != "" {
				manifest.Stability = static.Stability
			}
			manifest.DocsURL = static.DocsURL
		}
		out = append(out, manifest)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// metaProvider is an internal duck-type interface satisfied by every
// rule that implements the tiered RuleMeta contract (LocalRule /
// PathRule / GlobalRule). We declare it here rather than in domain
// because it exists solely to bridge the legacy AnalysisRule contract
// to the new Manifest catalog — adding it to domain/rule.go would
// invite confusion about whether it's a new public interface.
type metaProvider interface {
	Meta() domain.RuleMeta
}

// firstLineSummary returns the descriptive first sentence of a rule's
// RuleExplanations entry, stripping the leading `<rule_name> — `
// prefix every entry uses. Falls back to the rule name itself if no
// explanation is registered (which only happens for rules whose
// explanation hasn't been written yet — caught by the catalog test).
func firstLineSummary(name string) string {
	text, ok := ExplainRule(name)
	if !ok {
		return name
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx := strings.Index(line, " — "); idx > 0 {
			return strings.TrimSpace(line[idx+len(" — "):])
		}
		return line
	}
	return name
}
