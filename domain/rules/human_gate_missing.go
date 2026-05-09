package rules

import (
	"fmt"
	"strings"

	"github.com/hatyibei/shingan/domain"
)

// HumanGateMissingChecker flags production-flagged workflow graphs that
// contain no Human-type node anywhere — i.e. nothing in the workflow
// requires human approval before externally-visible side effects (API
// calls, writes, money / data egress) happen.
//
// Tier: Local (ADR-007). Decision is graph-wide (any production signal
// + zero NodeTypeHuman), so we register OnGraph and emit a single
// graph-level Warning.
//
// ConfidenceReason: ReasonHeuristicPattern. The deploy signal we use
// (Config["env"]=="prod" / Config["deployment"]==true) is a naming
// heuristic, not a schema-bound contract; some teams use Config["stage"]
// or other custom keys we don't see. 0.6 Confidence reflects that.
//
// Detection criteria:
//
//  1. Deploy signal: ANY node has `Config["env"] in {"prod","production","staging"}`
//     OR `Config["deployment"] == true` OR `Config["deploy"] == true`
//     OR `Config["environment"] in {prod,…}` (same set as missing_eval_dataset).
//  2. Sensitive-action signal: ANY Tool node carries
//     `Config["category"]` matching `code_execution`, `api`, `mcp`,
//     `browser`, OR a `secret_exposure_scanner`-style pattern in its name
//     (`send`, `delete`, `transfer`, `payment`, `email`, `webhook`).
//     This second gate keeps the rule from firing on innocuous "production
//     pure-compute" graphs that don't actually do anything externally.
//  3. Human signal: ANY node has `Type == NodeTypeHuman`.
//  4. If (1) AND (2) AND NOT (3) → emit Warning.
//
// The rule complements `pii_leak_scanner` (which traces specific source→
// sink paths) by enforcing a graph-wide governance posture: production
// agents touching the outside world need a human in the loop somewhere,
// even if the specific PII-tainted path that pii_leak_scanner would
// catch isn't present.
type HumanGateMissingChecker struct{}

// NewHumanGateMissingChecker returns a ready-to-use checker.
func NewHumanGateMissingChecker() *HumanGateMissingChecker {
	return &HumanGateMissingChecker{}
}

// Name returns the unique rule identifier.
func (h *HumanGateMissingChecker) Name() string { return "human_gate_missing" }

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (h *HumanGateMissingChecker) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     h.Name(),
		Severity: domain.Warning,
		Fixable:  false,
	}
}

// Listener implements domain.LocalRule. The work is graph-wide so it
// happens in OnGraph; per-node visits are unused.
func (h *HumanGateMissingChecker) Listener(ctx *domain.RuleContext) domain.Listener {
	return domain.Listener{
		OnGraph: func(c *domain.RuleContext, g *domain.WorkflowGraph) {
			if f, ok := evaluateHumanGateMissing(g); ok {
				c.Report(f)
			}
		},
	}
}

// Analyze keeps the legacy AnalysisRule contract alive.
func (h *HumanGateMissingChecker) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	if f, ok := evaluateHumanGateMissing(graph); ok {
		return []domain.Finding{f}
	}
	return nil
}

// evaluateHumanGateMissing implements the criteria above. Returns ok=true
// with a single Finding when (deploy signal AND sensitive-action signal
// AND no human node) all hold.
func evaluateHumanGateMissing(g *domain.WorkflowGraph) (domain.Finding, bool) {
	if g == nil || len(g.Nodes) == 0 {
		return domain.Finding{}, false
	}

	deployNodeID, hasDeploy := findDeploySignal(g)
	if !hasDeploy {
		return domain.Finding{}, false
	}

	sensitiveID, hasSensitive := findSensitiveAction(g)
	if !hasSensitive {
		return domain.Finding{}, false
	}

	if hasHumanNode(g) {
		return domain.Finding{}, false
	}

	// Anchor the finding to the deploy node (governance scope) but cite
	// the sensitive action in the message so reviewers know what's at
	// stake.
	return domain.Finding{
		RuleName: "human_gate_missing",
		Severity: domain.Warning,
		NodeID:   deployNodeID,
		Message: fmt.Sprintf(
			"production-deployed graph performs sensitive action via node %q with no Human-in-the-loop approval node anywhere in the workflow",
			sensitiveID,
		),
		Suggestion: "Add a NodeTypeHuman node before sensitive actions (API writes, code execution, payments, data egress) when the graph is deployed to production / staging. Even a single graph-wide approval point is enough to satisfy the governance check; for finer scoping see pii_leak_scanner / eval_missing.",
		Confidence:       0.6,
		ConfidenceReason: domain.ReasonHeuristicPattern,
	}, true
}

// sensitiveCategories lists Config["category"] values whose mere presence
// in a Tool node graduates the workflow to "sensitive enough that a
// production deploy without a human gate warrants a Warning".
var sensitiveCategories = map[string]bool{
	"code_execution": true,
	"code_eval":      true,
	"api":            true,
	"mcp":            true,
	"browser":        true,
	"trigger":        true, // n8n webhook / trigger nodes
}

// sensitiveActionKeywords scan node names for verbs that imply external
// side effects. Used as a fallback when Config["category"] isn't set
// (e.g. ADK-Go tools whose category lives in the type system rather
// than Config).
var sensitiveActionKeywords = []string{
	"send", "post", "delete", "remove", "transfer", "payment", "charge",
	"email", "webhook", "execute", "deploy", "publish", "fire",
}

// findSensitiveAction returns the ID of the first Tool node whose
// category or name signals an externally-visible side effect.
func findSensitiveAction(g *domain.WorkflowGraph) (string, bool) {
	for id, n := range g.Nodes {
		if n == nil {
			continue
		}
		if n.Type != domain.NodeTypeTool {
			continue
		}
		if cat, _ := n.Config["category"].(string); cat != "" {
			if sensitiveCategories[strings.ToLower(strings.TrimSpace(cat))] {
				return id, true
			}
		}
		lname := strings.ToLower(n.Name)
		for _, kw := range sensitiveActionKeywords {
			if strings.Contains(lname, kw) {
				return id, true
			}
		}
	}
	return "", false
}

// hasHumanNode reports whether ANY node in g is NodeTypeHuman.
func hasHumanNode(g *domain.WorkflowGraph) bool {
	for _, n := range g.Nodes {
		if n != nil && n.Type == domain.NodeTypeHuman {
			return true
		}
	}
	return false
}

func init() {
	registerBuiltin(NewHumanGateMissingChecker())
}
