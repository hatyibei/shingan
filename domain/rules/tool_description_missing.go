package rules

import (
	"fmt"
	"strings"

	"github.com/hatyibei/shingan/domain"
)

// ToolDescriptionMissingChecker flags Tool nodes that lack a usable
// description. LLM agents pick which tool to call from the description
// text alone — a missing or 1-word description means the model is
// guessing, which leads to wrong-tool selection (high cost / hallucinated
// arguments / wrong API hit).
//
// Tier: Local (ADR-007). The check is per-node and self-contained: each
// Tool node's Config is inspected independently with no need for path
// traversal or graph-wide aggregation.
//
// ConfidenceReason: ReasonHeuristicPattern. "Sufficient description" is
// inherently fuzzy — we use a conservative threshold (≥10 chars after
// trim, not just the tool name repeated) but accept that some perfectly-
// good 1-word names ("search") will trigger. Confidence 0.6 reflects
// that.
//
// Detection criteria:
//
//  1. Node.Type == NodeTypeTool.
//  2. None of {description, doc, summary, help} keys in Config carries
//     a non-empty trimmed string of length ≥ 10.
//  3. The tool's Name (or Config["tool_name"]) doesn't already double
//     as a description (multi-word natural-language sentence — e.g.
//     "Send email to recipient" passes even with no description field).
//
// The rule is silent on Tool nodes whose `category` is `trigger`
// (webhooks / schedulers don't need an LLM-facing description) and on
// nodes whose Config has `_shingan_ignore` containing this rule.
type ToolDescriptionMissingChecker struct{}

// NewToolDescriptionMissingChecker returns a ready-to-use checker.
func NewToolDescriptionMissingChecker() *ToolDescriptionMissingChecker {
	return &ToolDescriptionMissingChecker{}
}

// Name returns the unique rule identifier.
func (t *ToolDescriptionMissingChecker) Name() string { return "tool_description_missing" }

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (t *ToolDescriptionMissingChecker) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     t.Name(),
		Severity: domain.Info,
		Fixable:  false,
	}
}

// Listener implements domain.LocalRule. Per-Tool-node check.
func (t *ToolDescriptionMissingChecker) Listener(ctx *domain.RuleContext) domain.Listener {
	return domain.Listener{
		OnNode: map[domain.NodeType]domain.NodeHandler{
			domain.NodeTypeTool: func(c *domain.RuleContext, n *domain.Node) {
				if f, ok := evaluateToolDescription(n); ok {
					c.Report(f)
				}
			},
		},
	}
}

// Analyze keeps the legacy AnalysisRule contract alive.
func (t *ToolDescriptionMissingChecker) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	out := make([]domain.Finding, 0)
	for _, n := range graph.Nodes {
		if f, ok := evaluateToolDescription(n); ok {
			out = append(out, f)
		}
	}
	return out
}

const minDescriptionLen = 10

// descriptionKeys holds the Config field names that count as a tool
// description for LLM tool-selection purposes.
var descriptionKeys = []string{"description", "doc", "summary", "help", "purpose"}

// evaluateToolDescription implements the criteria above.
func evaluateToolDescription(n *domain.Node) (domain.Finding, bool) {
	if n == nil || n.Type != domain.NodeTypeTool {
		return domain.Finding{}, false
	}
	// Triggers / webhooks aren't LLM-facing — exempt.
	if cat, _ := n.Config["category"].(string); cat == "trigger" {
		return domain.Finding{}, false
	}
	// Honour node-level ignore (n8n JSON parity).
	if rules, ok := n.Config["_shingan_ignore"].([]any); ok {
		for _, r := range rules {
			if s, ok := r.(string); ok && (s == "*" || s == "tool_description_missing") {
				return domain.Finding{}, false
			}
		}
	}

	// Pass 1: any of the description keys provides a sufficient string.
	for _, key := range descriptionKeys {
		if s, ok := n.Config[key].(string); ok {
			if len(strings.TrimSpace(s)) >= minDescriptionLen {
				return domain.Finding{}, false
			}
		}
	}

	// Pass 2: the Name itself reads as a natural-language description
	// (≥3 space-separated words). Saves false positives on tools whose
	// authors used the Name field as the description.
	if isNaturalLanguageName(n.Name) {
		return domain.Finding{}, false
	}

	return domain.Finding{
		RuleName: "tool_description_missing",
		Severity: domain.Info,
		NodeID:   n.ID,
		Message: fmt.Sprintf(
			"Tool node %q has no usable description (Config[\"description\" / \"doc\" / \"summary\" / \"help\"] are all missing or shorter than %d chars). LLM agents pick tools from description text — without one, the model guesses.",
			n.ID, minDescriptionLen,
		),
		Suggestion:       "Add Config[\"description\"] = \"<one-line natural-language sentence describing what this tool does, when to use it, and what it returns>\". 2-3 sentences is the sweet spot for tool-use selection.",
		Confidence:       0.6,
		ConfidenceReason: domain.ReasonHeuristicPattern,
	}, true
}

// isNaturalLanguageName returns true when `name` looks like a sentence
// rather than a snake_case / camelCase identifier — i.e. has ≥3 words
// separated by whitespace. "Send email to recipient" passes;
// "send_email" / "sendEmail" fails (those are identifiers, not docs).
func isNaturalLanguageName(name string) bool {
	if name == "" {
		return false
	}
	fields := strings.Fields(name)
	return len(fields) >= 3
}

func init() {
	registerBuiltin(NewToolDescriptionMissingChecker())
}
