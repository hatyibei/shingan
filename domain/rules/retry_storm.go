package rules

import (
	"fmt"

	"github.com/hatyibei/shingan/domain"
)

// RetryStorm flags Tool nodes whose retry configuration combined with the
// surrounding parallelism produces a high blast radius — i.e. the maximum
// number of upstream calls that can be issued against the wrapped external
// API in a single workflow run.
//
// Tier: Path (ADR-007). The rule needs to know the parallelism that hits the
// retry-configured Tool, and that lives one hop upstream (incoming edges,
// parent Loop's max_iterations). Local listeners cannot see the neighbourhood,
// so Path is the right tier even though we never actually traverse a path
// from a sink — Sinks() is intentionally nil and we evaluate per-source on
// Propagate, mirroring CostAnalyzer.
//
// ConfidenceReason:
//   - blast >= 100 → ReasonExactStaticMatch (numeric multiplication of two
//     declared config values is deterministic).
//   - blast >= 30  → ReasonHeuristicPattern (parallelism estimate combines
//     fan-in count and config hints; the worst-case picture is conservative).
//   - blast >= 10  → ReasonHeuristicPattern (same reason as Warning).
//
// Severity rules (using blast = retries × parallelism):
//   - blast >= 100 → Critical (Confidence 0.9)
//   - blast >= 30  → Warning  (Confidence 0.7)
//   - blast >= 10  → Info     (Confidence 0.5)
//   - blast < 10   → no finding
//
// Sources are NodeTypeTool nodes whose Config carries `retries`,
// `max_retries`, or `retry_count` >= 3 (a single retry is treated as a normal
// transient-failure handler and not a storm risk). Sinks are intentionally
// empty: blast radius is a per-source property, not a path property.
type RetryStorm struct{}

// NewRetryStorm returns a ready-to-use RetryStorm rule.
func NewRetryStorm() *RetryStorm {
	return &RetryStorm{}
}

// Name returns the unique rule identifier.
func (r *RetryStorm) Name() string {
	return "retry_storm"
}

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (r *RetryStorm) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     r.Name(),
		Severity: domain.Warning,
		Fixable:  false,
	}
}

// retryConfigKeys lists the Config keys that, when present and >= 3, mark a
// Tool node as a retry-storm source candidate. Lower values are treated as
// normal transient-failure tolerance and ignored.
var retryConfigKeys = []string{"retries", "max_retries", "retry_count"}

// retryThreshold is the minimum retry count that promotes a Tool node to a
// rule source. Below this, retries are considered normal transient handling.
const retryThreshold = 3

// retryStormBlastCritical / Warning / Info are the blast-radius cutoffs.
const (
	retryStormBlastCritical = 100
	retryStormBlastWarning  = 30
	retryStormBlastInfo     = 10
)

// retryCount returns the effective retry count for a Tool node and whether
// the node qualifies as a retry-storm source (retries >= retryThreshold).
// The returned key is the Config field that won so the message can quote it.
func retryCount(node *domain.Node) (count int, key string, ok bool) {
	if node == nil || node.Config == nil {
		return 0, "", false
	}
	for _, k := range retryConfigKeys {
		if v, present := intConfig(node, k); present && v >= retryThreshold {
			return v, k, true
		}
	}
	return 0, "", false
}

// Sources implements domain.PathRule. It returns Tool nodes that carry a
// retry-storm-grade retry config.
func (r *RetryStorm) Sources(g *domain.WorkflowGraph) []*domain.Node {
	if g == nil {
		return nil
	}
	var out []*domain.Node
	for _, n := range g.Nodes {
		if n.Type != domain.NodeTypeTool {
			continue
		}
		if _, _, ok := retryCount(n); ok {
			out = append(out, n)
		}
	}
	return out
}

// Sinks implements domain.PathRule. RetryStorm is per-source — there is no
// sink concept — so we return nil.
func (r *RetryStorm) Sinks(g *domain.WorkflowGraph) []*domain.Node { return nil }

// Propagate implements domain.PathRule. For each retry-configured source, it
// estimates the parallelism (fan-in / max_concurrency / upstream loop bound)
// and emits a Finding when blast = retries × parallelism crosses the
// info / warning / critical thresholds.
func (r *RetryStorm) Propagate(ctx *domain.PathContext) []domain.Finding {
	if ctx == nil || ctx.Graph == nil {
		return nil
	}
	return runRetryStormChecks(ctx.Graph, ctx.Sources)
}

// Analyze keeps the legacy AnalysisRule contract alive.
func (r *RetryStorm) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	return runRetryStormChecks(graph, r.Sources(graph))
}

// runRetryStormChecks evaluates each retry-storm source and emits Findings.
// Shared by Propagate and the legacy Analyze fallback so behaviour stays
// identical across both call paths.
func runRetryStormChecks(graph *domain.WorkflowGraph, sources []*domain.Node) []domain.Finding {
	var findings []domain.Finding
	for _, src := range sources {
		retries, retryKey, ok := retryCount(src)
		if !ok {
			continue
		}
		par := estimateParallelism(graph, src)
		blast := retries * par

		var (
			severity   domain.Severity
			confidence float64
			reason     domain.ConfidenceReason
		)
		switch {
		case blast >= retryStormBlastCritical:
			severity, confidence, reason = domain.Critical, 0.9, domain.ReasonExactStaticMatch
		case blast >= retryStormBlastWarning:
			severity, confidence, reason = domain.Warning, 0.7, domain.ReasonHeuristicPattern
		case blast >= retryStormBlastInfo:
			severity, confidence, reason = domain.Info, 0.5, domain.ReasonHeuristicPattern
		default:
			continue
		}

		findings = append(findings, domain.Finding{
			RuleName: "retry_storm",
			Severity: severity,
			NodeID:   src.ID,
			Message: fmt.Sprintf(
				"Tool node %q has %s=%d with parallelism=%d → blast radius %d",
				src.ID, retryKey, retries, par, blast,
			),
			Suggestion: fmt.Sprintf(
				"retry %d × parallelism %d = blast radius %d. Consider exponential backoff (`backoff_factor`), a circuit breaker, or a shared rate limiter to avoid stampeding the upstream API.",
				retries, par, blast,
			),
			Confidence:       confidence,
			ConfidenceReason: reason,
		})
	}
	return findings
}

// estimateParallelism returns the worst-case (max) parallelism estimate that
// can hit `src` in a single workflow run. It combines three signals and
// returns the maximum, treating each as an independent upper bound:
//
//  1. The fan-in count: number of incoming edges (concurrent upstream calls).
//  2. The source's own Config["max_concurrency"] if set.
//  3. Any directly-upstream Loop node's Config["max_iterations"] (each
//     iteration produces one inflight call).
//
// All three are upper bounds — the actual concurrency a runtime sees may be
// strictly lower — so taking the max is conservative but matches the
// "worst-case storm" framing of the rule. A minimum of 1 is enforced so a
// pure self-contained source still has blast = retries × 1.
func estimateParallelism(graph *domain.WorkflowGraph, src *domain.Node) int {
	par := 1

	// Signal 1: source's own max_concurrency.
	if v, ok := intConfig(src, "max_concurrency"); ok && v > par {
		par = v
	}

	// Signals 2 & 3: scan incoming edges.
	fanIn := 0
	for _, e := range graph.Edges {
		if e.To != src.ID {
			continue
		}
		fanIn++
		if pred, exists := graph.Nodes[e.From]; exists {
			// Upstream Loop's max_iterations.
			if pred.Type == domain.NodeTypeLoop {
				if v, ok := intConfig(pred, "max_iterations"); ok && v > par {
					par = v
				}
			}
			// Upstream node's max_concurrency (e.g. a parallel orchestrator
			// declaring its own fan-out cap one hop above the leaf tool).
			if v, ok := intConfig(pred, "max_concurrency"); ok && v > par {
				par = v
			}
		}
	}
	if fanIn > par {
		par = fanIn
	}
	return par
}

// intConfig returns Config[key] coerced to int alongside a presence flag.
// JSON-decoded numbers arrive as float64; we accept both as well as the
// signed integer types that hand-built test graphs use.
func intConfig(node *domain.Node, key string) (int, bool) {
	if node == nil || node.Config == nil {
		return 0, false
	}
	v, ok := node.Config[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float32:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

func init() {
	registerBuiltin(NewRetryStorm())
}
