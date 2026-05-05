package rules

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hatyibei/shingan/domain"
)

// EvalMissing flags workflow paths in which an LLM node's output reaches a
// code-execution Tool sink (eval / exec / Function / code_interpreter / shell)
// without being parsed, validated, or gated by a Human-in-the-loop approver.
//
// Tier: Path (ADR-007). Sources are LLM nodes; Sinks are Tool nodes whose
// Config["category"] is "code_execution" / "code_eval", whose Config["tool"]
// is one of {eval, exec, code_interpreter, python_runner, shell}, or whose
// name matches an eval/exec/runner pattern.
//
// ConfidenceReason: ReasonHeuristicPattern. Sink classification relies on
// naming and config-key heuristics; the path traversal guarantees only
// structural reachability, not semantic taint flow.
//
// Severity rules (decided per (source, sink) path):
//   - LLM → ... → code-exec sink, no Condition / no Human in between
//     → Critical (Confidence 0.9)
//   - LLM → ... → Condition node → ... → code-exec sink
//     → Warning  (Confidence 0.6) — operator validates, but content is still
//       dynamic; flag as elevated-not-resolved.
//   - Any path passing through a NodeTypeHuman approver → skip (no finding).
//
// The forward-BFS frontier carries a `viaCondition` flag so per-path metadata
// drives Severity. Reverse-BFS would force the rule to repeatedly recompute
// path state per (source, sink) pair; the forward shape mirrors the human
// reading of the workflow ("does the LLM ever reach eval?").
type EvalMissing struct{}

// NewEvalMissing returns a ready-to-use EvalMissing.
func NewEvalMissing() *EvalMissing {
	return &EvalMissing{}
}

// Name returns the unique rule identifier.
func (e *EvalMissing) Name() string {
	return "eval_missing"
}

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (e *EvalMissing) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     e.Name(),
		Severity: domain.Critical,
		Fixable:  false,
	}
}

// evalSinkToolValues lists Config["tool"] values that mark a node as a
// code-execution sink. Lower-cased for case-insensitive comparison.
var evalSinkToolValues = map[string]bool{
	"eval":             true,
	"exec":             true,
	"code_interpreter": true,
	"python_runner":    true,
	"shell":            true,
}

// evalSinkCategories lists Config["category"] values that mark a node as a
// code-execution sink.
var evalSinkCategories = map[string]bool{
	"code_execution": true,
	"code_eval":      true,
}

// evalSinkNamePattern matches names that look like code-execution sinks. The
// alternatives mirror the values in evalSinkToolValues plus common synonyms
// (code_runner, bash) and are case-insensitive. The optional `[_]?` between
// "python"/"code" and "runner" lets PascalCase ("PythonRunner") and
// snake_case ("python_runner") both match.
var evalSinkNamePattern = regexp.MustCompile(`(?i)(eval|exec|code[_]?runner|python[_]?runner|shell|bash)`)

// isEvalSink reports whether n is classified as a code-execution Tool sink.
// The check examines, in order:
//  1. Config["category"] ∈ {code_execution, code_eval}
//  2. Config["tool"] ∈ {eval, exec, code_interpreter, python_runner, shell}
//  3. Name (or ID) matches evalSinkNamePattern
func isEvalSink(n *domain.Node) bool {
	if n == nil || n.Type != domain.NodeTypeTool {
		return false
	}
	if cat := strings.ToLower(stringConfig(n, "category")); evalSinkCategories[cat] {
		return true
	}
	if tool := strings.ToLower(stringConfig(n, "tool")); evalSinkToolValues[tool] {
		return true
	}
	if evalSinkNamePattern.MatchString(n.Name) {
		return true
	}
	if evalSinkNamePattern.MatchString(n.ID) {
		return true
	}
	return false
}

// Sources implements domain.PathRule. It returns every LLM node — these are
// the potential producers of unvalidated text that may end up at the sink.
func (e *EvalMissing) Sources(g *domain.WorkflowGraph) []*domain.Node {
	if g == nil {
		return nil
	}
	var out []*domain.Node
	for _, n := range g.Nodes {
		if n.Type == domain.NodeTypeLLM {
			out = append(out, n)
		}
	}
	return out
}

// Sinks implements domain.PathRule. It returns every Tool node classified as
// a code-execution sink.
func (e *EvalMissing) Sinks(g *domain.WorkflowGraph) []*domain.Node {
	if g == nil {
		return nil
	}
	var out []*domain.Node
	for _, n := range g.Nodes {
		if isEvalSink(n) {
			out = append(out, n)
		}
	}
	return out
}

// Propagate implements domain.PathRule. For each LLM source, run a forward BFS
// whose frontier records whether a Condition node has been crossed; emit one
// Finding per (source, sink) pair, downgrading Severity when the frontier
// reached the sink via a Condition. Paths through a Human node are skipped.
func (e *EvalMissing) Propagate(ctx *domain.PathContext) []domain.Finding {
	if ctx == nil || ctx.Graph == nil || len(ctx.Sources) == 0 || len(ctx.Sinks) == 0 {
		return nil
	}
	return runEvalMissingForwardBFS(ctx.Graph, ctx.Sources, ctx.Sinks)
}

// Analyze keeps the legacy AnalysisRule contract alive. The forward BFS does
// not need precomputed reverse adjacency, so the legacy path simply rebuilds
// Sources / Sinks and dispatches to the same shared traversal as Propagate.
func (e *EvalMissing) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	sources := e.Sources(graph)
	sinks := e.Sinks(graph)
	if len(sources) == 0 || len(sinks) == 0 {
		return nil
	}
	return runEvalMissingForwardBFS(graph, sources, sinks)
}

// frontierState records per-path metadata for the BFS. We use a small struct
// rather than two parallel maps so adding new gate types (sanitizer / parser)
// later is a one-field change.
type frontierState struct {
	viaCondition bool
}

// runEvalMissingForwardBFS performs the path traversal shared by Propagate
// and the legacy Analyze fallback. For each LLM source it walks forward; the
// frontier's viaCondition flag flips true when the BFS crosses a Condition
// node, and any path passing a Human node is dropped (no expansion past it).
//
// At each sink reached, we emit one Finding stamped with the per-path
// severity decided by viaCondition.
func runEvalMissingForwardBFS(graph *domain.WorkflowGraph, sources []*domain.Node, sinks []*domain.Node) []domain.Finding {
	sinkIDs := make(map[string]*domain.Node, len(sinks))
	for _, s := range sinks {
		sinkIDs[s.ID] = s
	}

	// Build forward adjacency once per legacy call. Path-walker callers use
	// ctx.Reverse, but eval_missing is forward-flow; we maintain our own
	// adjacency rather than relying on the shared reverse map. The cost is
	// O(E) per Analyze() call, identical to pii_leak's reverse build cost.
	forward := make(map[string][]string, len(graph.Nodes))
	for _, edge := range graph.Edges {
		forward[edge.From] = append(forward[edge.From], edge.To)
	}

	var findings []domain.Finding
	// Track emitted (source, sink) pairs so a node reachable via two
	// branches does not produce duplicate Findings.
	emitted := make(map[string]bool)

	for _, src := range sources {
		// visitedAt records, for each visited node, the strongest frontier
		// that has reached it. We model "strongest" as "viaCondition=false";
		// that way, a later via-Condition arrival is dropped (already a
		// Critical was reachable). This keeps Severity stable.
		type stateKey struct {
			node         string
			viaCondition bool
		}
		visited := make(map[stateKey]bool)

		type queueEntry struct {
			node  string
			state frontierState
		}
		queue := []queueEntry{{node: src.ID, state: frontierState{}}}
		visited[stateKey{src.ID, false}] = true

		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]

			// If this node is a sink, emit and stop expanding from it
			// (a sink that is itself a code-exec endpoint terminates the
			// dangerous path).
			if sinkNode, ok := sinkIDs[cur.node]; ok && cur.node != src.ID {
				key := src.ID + "|" + cur.node
				if !emitted[key] {
					emitted[key] = true
					findings = append(findings, buildEvalMissingFinding(src, sinkNode, cur.state.viaCondition))
				}
				continue
			}

			for _, next := range forward[cur.node] {
				nextNode, ok := graph.Nodes[next]
				if !ok {
					continue
				}
				// Human gate: do not expand past it. The path is considered
				// safe as long as the human reviews the code before exec.
				if nextNode.Type == domain.NodeTypeHuman {
					continue
				}

				nextState := cur.state
				if nextNode.Type == domain.NodeTypeCondition {
					nextState.viaCondition = true
				}

				k := stateKey{node: next, viaCondition: nextState.viaCondition}
				if visited[k] {
					continue
				}
				// If a non-via-Condition route already reached `next`, do
				// not re-enter via the via-Condition route — the existing
				// Critical-eligible path dominates the downgrade.
				if !nextState.viaCondition {
					// no dominating Critical-eligible route yet; record this
					visited[k] = true
				} else if visited[stateKey{node: next, viaCondition: false}] {
					// dominated; skip
					continue
				} else {
					visited[k] = true
				}

				queue = append(queue, queueEntry{node: next, state: nextState})
			}
		}
	}

	return findings
}

// buildEvalMissingFinding assembles the Finding for a (source, sink) pair.
// viaCondition selects the (Severity, Confidence) tuple per the rule's table.
func buildEvalMissingFinding(source *domain.Node, sink *domain.Node, viaCondition bool) domain.Finding {
	severity := domain.Critical
	confidence := 0.9
	gate := "no validation"
	if viaCondition {
		severity = domain.Warning
		confidence = 0.6
		gate = "Condition gate"
	}

	msg := fmt.Sprintf(
		"eval_missing: LLM node %q reaches code-execution tool %q (%s); LLM output flows into a code runner without sanitisation",
		source.ID, sink.ID, gate,
	)
	suggestion := "LLM output flows directly into a code-execution tool. Validate / parse / sandbox the output; never `eval()` raw model text. Consider Tool-use with a structured tool-call schema, or insert a Human approval gate."

	return domain.Finding{
		RuleName:         "eval_missing",
		Severity:         severity,
		NodeID:           sink.ID,
		Message:          msg,
		Suggestion:       suggestion,
		Confidence:       confidence,
		ConfidenceReason: domain.ReasonHeuristicPattern,
	}
}

func init() {
	registerBuiltin(NewEvalMissing())
}
