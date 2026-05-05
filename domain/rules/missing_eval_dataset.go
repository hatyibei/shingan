package rules

import (
	"fmt"
	"strings"

	"github.com/hatyibei/shingan/domain"
)

// MissingEvalDatasetChecker flags workflow graphs whose Config indicates
// production / staging deployment but carries no eval dataset / benchmark
// reference anywhere in the graph. Production AI agents need regression
// eval to catch model upgrades that change behaviour without warning.
//
// Tier: Local (ADR-007). The decision needs a graph-wide view (any node's
// deploy flag + any node's eval reference), so the rule registers the
// `OnGraph` handler and emits a single graph-level Finding when the
// criteria match. Per-node visits are not required — this is a Local
// rule by tier definition because no path traversal or reverse adjacency
// is needed; the OnGraph hook is the supported way Local rules express
// graph-wide aggregation (see redundant.go for the established pattern).
//
// ConfidenceReason: ReasonHeuristicPattern. We rely on naming heuristics
// for both the deploy signal and the eval dataset key — neither is a
// schema-bound contract, so a 0.7 Confidence is appropriate.
//
// Detection criteria (option B from the design discussion):
//
//  1. Deploy signal: ANY node has `Config["deployment"] == true` OR
//     `Config["env"] in {"prod", "staging", "production"}` OR
//     `Config["deploy"] == true`.
//  2. Eval signal: ANY node has a non-empty `Config["eval_dataset"]` OR
//     `Config["test_set"]` OR `Config["benchmark"]` OR
//     `Config["eval"]` value (string or map).
//  3. If (1) is true and (2) is false → emit Warning.
//  4. If (1) is false → silent (skip; the rule does not police pre-prod
//     workflows).
type MissingEvalDatasetChecker struct{}

// NewMissingEvalDatasetChecker returns a ready-to-use checker.
func NewMissingEvalDatasetChecker() *MissingEvalDatasetChecker {
	return &MissingEvalDatasetChecker{}
}

// Name returns the unique rule identifier.
func (m *MissingEvalDatasetChecker) Name() string {
	return "missing_eval_dataset"
}

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (m *MissingEvalDatasetChecker) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     m.Name(),
		Severity: domain.Warning,
		Fixable:  false,
	}
}

// Listener implements domain.LocalRule. The work is graph-wide, so it
// happens in OnGraph. Per-node visits are unused.
func (m *MissingEvalDatasetChecker) Listener(ctx *domain.RuleContext) domain.Listener {
	return domain.Listener{
		OnGraph: func(c *domain.RuleContext, g *domain.WorkflowGraph) {
			if f, ok := evaluateMissingEvalDataset(g); ok {
				c.Report(f)
			}
		},
	}
}

// Analyze keeps the legacy AnalysisRule contract alive.
func (m *MissingEvalDatasetChecker) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	if f, ok := evaluateMissingEvalDataset(graph); ok {
		return []domain.Finding{f}
	}
	return nil
}

// evaluateMissingEvalDataset implements the criteria documented on the
// checker. Returns ok=true with a Finding when the deploy signal is
// present and the eval signal is absent.
func evaluateMissingEvalDataset(g *domain.WorkflowGraph) (domain.Finding, bool) {
	if g == nil || len(g.Nodes) == 0 {
		return domain.Finding{}, false
	}

	deployNodeID, hasDeploy := findDeploySignal(g)
	if !hasDeploy {
		return domain.Finding{}, false
	}

	if hasEvalSignal(g) {
		return domain.Finding{}, false
	}

	// NodeID points at the node that surfaced the deploy signal so the
	// reviewer knows where the production claim came from.
	return domain.Finding{
		RuleName: "missing_eval_dataset",
		Severity: domain.Warning,
		NodeID:   deployNodeID,
		Message: fmt.Sprintf(
			"workflow is configured for production/staging deployment (node %q) but no eval dataset / benchmark is declared anywhere in the graph",
			deployNodeID,
		),
		Suggestion:       "Production AI agents need regression eval to catch model upgrades that change behaviour. Add `Config[\"eval_dataset\"]` (or `test_set` / `benchmark` / `eval`) on any node, pointing to a versioned test set with expected outputs.",
		Confidence:       0.7,
		ConfidenceReason: domain.ReasonHeuristicPattern,
	}, true
}

// deployEnvValues holds the lower-cased Config["env"] values that count
// as a "production" signal. Anything else (dev / test / local / unknown)
// is treated as benign.
var deployEnvValues = map[string]bool{
	"prod":       true,
	"production": true,
	"staging":    true,
	"stg":        true,
}

// findDeploySignal scans every node for any field that indicates the
// workflow is deployed to production / staging. Returns the offending
// node's ID alongside ok=true. The traversal is order-independent
// (Nodes is a map) but stable enough — only the *presence* matters; the
// rule emits at most one finding per graph regardless of node order.
func findDeploySignal(g *domain.WorkflowGraph) (string, bool) {
	for id, n := range g.Nodes {
		if n == nil || n.Config == nil {
			continue
		}
		// Boolean flags
		if b, ok := n.Config["deployment"].(bool); ok && b {
			return id, true
		}
		if b, ok := n.Config["deploy"].(bool); ok && b {
			return id, true
		}
		// String env value
		if env, ok := n.Config["env"].(string); ok {
			if deployEnvValues[strings.ToLower(strings.TrimSpace(env))] {
				return id, true
			}
		}
		if env, ok := n.Config["environment"].(string); ok {
			if deployEnvValues[strings.ToLower(strings.TrimSpace(env))] {
				return id, true
			}
		}
	}
	return "", false
}

// evalDatasetKeys is the list of Config keys that signal an eval /
// benchmark / regression test set is configured for the workflow.
var evalDatasetKeys = []string{
	"eval_dataset",
	"test_set",
	"benchmark",
	"eval",
	"evals",
	"test_dataset",
	"regression_set",
}

// hasEvalSignal reports whether any node in g declares a non-empty value
// under any of evalDatasetKeys. Both string and map values are accepted
// (a string path / id, OR a structured `{ "name": ..., "version": ... }`
// reference).
func hasEvalSignal(g *domain.WorkflowGraph) bool {
	for _, n := range g.Nodes {
		if n == nil || n.Config == nil {
			continue
		}
		for _, key := range evalDatasetKeys {
			v, ok := n.Config[key]
			if !ok || v == nil {
				continue
			}
			switch tv := v.(type) {
			case string:
				if strings.TrimSpace(tv) != "" {
					return true
				}
			case map[string]any:
				if len(tv) > 0 {
					return true
				}
			case []any:
				if len(tv) > 0 {
					return true
				}
			}
		}
	}
	return false
}

func init() {
	registerBuiltin(NewMissingEvalDatasetChecker())
}
