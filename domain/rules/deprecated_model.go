package rules

import (
	"fmt"

	"github.com/hatyibei/shingan/domain"
)

// modelStatus represents the lifecycle state of an LLM model.
type modelStatus int

const (
	modelActive     modelStatus = iota
	modelDeprecated             // still callable but scheduled for shutdown
	modelShutdown               // requests will fail at runtime
)

// deprecatedModels maps known deprecated/shutdown model names to their metadata.
var deprecatedModels = map[string]struct {
	status          modelStatus
	replacement     string
	deprecationDate string
}{
	// OpenAI — shutdown
	"gpt-3.5-turbo-0301":     {modelShutdown, "gpt-4o-mini", "2024-06-13"},
	"gpt-3.5-turbo-0613":     {modelShutdown, "gpt-4o-mini", "2024-09-13"},
	"gpt-3.5-turbo-16k-0613": {modelShutdown, "gpt-4o-mini", "2024-09-13"},
	"text-davinci-003":       {modelShutdown, "gpt-4o", "2024-01-04"},
	"text-davinci-002":       {modelShutdown, "gpt-4o", "2024-01-04"},
	"code-davinci-002":       {modelShutdown, "gpt-4o", "2024-01-04"},
	"gpt-4-0314":             {modelShutdown, "gpt-4o", "2024-06-13"},
	// OpenAI — deprecated (still callable)
	"gpt-4-32k":  {modelDeprecated, "gpt-4o", "2025-06-06"},
	"gpt-4-0613": {modelDeprecated, "gpt-4o", "2025-06-06"},

	// Anthropic — shutdown
	"claude-1":           {modelShutdown, "claude-3-5-sonnet", "2023-11-01"},
	"claude-1.3":         {modelShutdown, "claude-3-5-sonnet", "2023-11-01"},
	"claude-2":           {modelShutdown, "claude-3-5-sonnet", "2024-07-21"},
	"claude-2.0":         {modelShutdown, "claude-3-5-sonnet", "2024-07-21"},
	"claude-2.1":         {modelShutdown, "claude-3-5-sonnet", "2024-07-21"},
	"claude-instant-1":   {modelShutdown, "claude-3-haiku", "2024-07-21"},
	"claude-instant-1.2": {modelShutdown, "claude-3-haiku", "2024-07-21"},
	// Anthropic — deprecated (still callable)
	"claude-3-opus": {modelDeprecated, "claude-3-5-sonnet or claude-opus-4", "2025-10-01"},

	// Google — shutdown
	"gemini-pro":     {modelShutdown, "gemini-1.5-pro", "2025-02-15"},
	"text-bison-001": {modelShutdown, "gemini-1.5-flash", "2024-10-01"},
	"chat-bison-001": {modelShutdown, "gemini-1.5-flash", "2024-10-01"},
}

// DeprecatedModelChecker detects LLM nodes that reference deprecated or
// shutdown model names.
//
// Tier: Local (ADR-007) — decision per node, fits the 1-walk dispatcher.
// ConfidenceReason: ReasonExactStaticMatch (lookup against curated table).
//
// Severity rules:
//   - modelShutdown → Critical (confidence 1.0): runtime requests will fail.
//   - modelDeprecated → Warning (confidence 0.9): shutdown expected within ~6 months.
type DeprecatedModelChecker struct{}

// NewDeprecatedModelChecker returns a ready-to-use DeprecatedModelChecker.
func NewDeprecatedModelChecker() *DeprecatedModelChecker {
	return &DeprecatedModelChecker{}
}

// Name returns the unique rule identifier.
func (d *DeprecatedModelChecker) Name() string {
	return "deprecated_model"
}

// Meta returns the rule metadata used by the new tier-aware orchestrator.
func (d *DeprecatedModelChecker) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     d.Name(),
		Severity: domain.Warning,
		Fixable:  true,
	}
}

// Listener implements domain.LocalRule. It only listens for LLM nodes and
// emits a Finding when the model name matches a known deprecated/shutdown
// entry in deprecatedModels.
func (d *DeprecatedModelChecker) Listener(ctx *domain.RuleContext) domain.Listener {
	return domain.Listener{
		OnNode: map[domain.NodeType]domain.NodeHandler{
			domain.NodeTypeLLM: func(c *domain.RuleContext, n *domain.Node) {
				if f, ok := evaluateDeprecatedModel(n); ok {
					c.Report(f)
				}
			},
		},
	}
}

// Analyze keeps the legacy AnalysisRule contract alive for callers that have
// not yet migrated to the tier-aware orchestrator.
func (d *DeprecatedModelChecker) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	var findings []domain.Finding
	for _, node := range graph.Nodes {
		if node.Type != domain.NodeTypeLLM {
			continue
		}
		if f, ok := evaluateDeprecatedModel(node); ok {
			findings = append(findings, f)
		}
	}
	return findings
}

// evaluateDeprecatedModel returns the Finding for node if it references a
// deprecated/shutdown model. The ok return is false when the node is not
// flagged so the caller can skip the empty Finding.
func evaluateDeprecatedModel(node *domain.Node) (domain.Finding, bool) {
	model := stringConfig(node, "model")
	if model == "" {
		return domain.Finding{}, false
	}
	info, found := deprecatedModels[model]
	if !found {
		return domain.Finding{}, false
	}

	switch info.status {
	case modelShutdown:
		return domain.Finding{
			RuleName: "deprecated_model",
			Severity: domain.Critical,
			NodeID:   node.ID,
			Message: fmt.Sprintf(
				"model %q has been shut down (since %s). Requests will fail at runtime.",
				model, info.deprecationDate,
			),
			Suggestion:       fmt.Sprintf("Migrate to %s", info.replacement),
			Confidence:       1.0,
			ConfidenceReason: domain.ReasonExactStaticMatch,
		}, true
	case modelDeprecated:
		return domain.Finding{
			RuleName: "deprecated_model",
			Severity: domain.Warning,
			NodeID:   node.ID,
			Message: fmt.Sprintf(
				"model %q is deprecated (since %s). Expect shutdown in the next ~6 months.",
				model, info.deprecationDate,
			),
			Suggestion:       fmt.Sprintf("Migrate to %s", info.replacement),
			Confidence:       0.9,
			ConfidenceReason: domain.ReasonExactStaticMatch,
		}, true
	}
	return domain.Finding{}, false
}

func init() {
	registerBuiltin(NewDeprecatedModelChecker())
}
