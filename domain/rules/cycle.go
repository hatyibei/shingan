// Package rules contains static analysis rules for Shingan's workflow graph analysis.
// All rules implement the domain.AnalysisRule interface and have no external dependencies.
package rules

import (
	"fmt"
	"strconv"

	"github.com/hatyibei/shingan/domain"
)

// visitState tracks the DFS traversal state of a node.
type visitState int

const (
	unvisited  visitState = iota // node not yet seen
	inProgress                   // node is on the current DFS stack (back edge target = cycle)
	completed                    // node and all descendants have been fully explored
)

// CycleDetector detects cycles in a WorkflowGraph and evaluates whether each
// cycle is guarded by a Loop node (LoopAgent equivalent) with a safe
// max_iterations bound.
//
// Tier: Global (ADR-007) — DFS over the entire graph, single pass.
// ConfidenceReason: ReasonExactStaticMatch (deterministic back-edge detection).
//
// Severity rules:
//   - Loop/Control node in cycle, max_iterations not set → Critical
//   - Loop/Control node in cycle, max_iterations >= 100 → Warning
//   - Loop/Control node in cycle, max_iterations < 100 → no finding (safe loop)
//   - Condition node in cycle → Critical (graph definition error: conditions must not loop)
//   - Non-Loop node forms a cycle inside a Loop with no max_iterations → Critical (unbounded)
//   - Non-Loop node forms a cycle inside a Loop with max_iterations >= 100 → Info
//   - Non-Loop node forms a cycle inside a Loop with max_iterations < 100 → no finding (safe)
//   - Non-Loop node forms a cycle with no parent Loop guard → Critical (graph error)
type CycleDetector struct{}

// NewCycleDetector returns a ready-to-use CycleDetector.
func NewCycleDetector() *CycleDetector {
	return &CycleDetector{}
}

// Name returns the unique rule identifier.
func (c *CycleDetector) Name() string {
	return "cycle_detection"
}

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (c *CycleDetector) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     c.Name(),
		Severity: domain.Critical,
		Fixable:  false,
	}
}

// AnalyzeGlobal implements domain.GlobalRule. It is a thin alias around
// Analyze so the orchestrator can route this rule through the GlobalWalker.
func (c *CycleDetector) AnalyzeGlobal(graph *domain.WorkflowGraph) []domain.Finding {
	return c.Analyze(graph)
}

// Analyze performs DFS from the entry node and reports cycle findings.
func (c *CycleDetector) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}

	state := make(map[string]visitState, len(graph.Nodes))
	var findings []domain.Finding
	// Dedupe back-edges that close on the same cycle entry: data-enrichment
	// (LangGraph dogfood) had three distinct conditional branches all
	// returning to `call_agent_model` (route_after_agent → "tools" |
	// "reflect" | "call_agent_model"), producing three identical
	// `cycle_detection` warnings on the same node. Engineers reviewing
	// the SARIF report scroll past duplicates rather than reading
	// individual messages, so we collapse to one Finding per cycle
	// entry node and let the suggestion text describe the bound.
	seenCycle := make(map[string]bool, len(graph.Nodes))

	// DFS closure — returns findings detected on the path rooted at nodeID.
	// path holds the current DFS stack (ancestors of nodeID, not including nodeID itself).
	var dfs func(nodeID string, path []string)
	dfs = func(nodeID string, path []string) {
		state[nodeID] = inProgress
		// Extend path with the current node for descendants to inspect.
		currentPath := append(path, nodeID)

		for _, edge := range graph.OutgoingEdges(nodeID) {
			target := edge.To

			switch state[target] {
			case inProgress:
				// Back edge: we found a cycle. The target node is the entry
				// point of the cycle and is the one responsible for bounding it.
				// Pass currentPath so evaluateCycle can locate a parent Control node.
				if seenCycle[target] {
					break
				}
				f := c.evaluateCycle(graph, target, currentPath)
				// evaluateCycle returns a zero-value Finding when the cycle is safe
				// (Control node with max_iterations < 100). Skip those.
				if f.RuleName != "" {
					seenCycle[target] = true
					findings = append(findings, f)
				}

			case unvisited:
				// Target node not yet visited, recurse.
				if _, exists := graph.Nodes[target]; exists {
					dfs(target, currentPath)
				}
				// If target is not in Nodes map, it is a dangling reference —
				// out of scope for CycleDetector, skip silently.

			case completed:
				// Already fully explored; no cycle via this edge.
			}
		}

		state[nodeID] = completed
	}

	// Start DFS from the entry node.
	if _, ok := graph.Nodes[graph.EntryNodeID]; ok {
		dfs(graph.EntryNodeID, nil)
	}

	// Also visit nodes not reachable from the entry to catch isolated cycles.
	for id := range graph.Nodes {
		if state[id] == unvisited {
			dfs(id, nil)
		}
	}

	return findings
}

// findParentControl scans path (the DFS ancestor stack) in reverse to find the
// nearest Loop or deprecated Control node that governs the cycle. Returns nil if not found.
// NodeTypeCondition is intentionally excluded: condition branches are not loop guards.
func (c *CycleDetector) findParentControl(graph *domain.WorkflowGraph, path []string) *domain.Node {
	for i := len(path) - 1; i >= 0; i-- {
		n, ok := graph.Nodes[path[i]]
		if ok && isLoopNode(n.Type) {
			return n
		}
	}
	return nil
}

// cycleHasExit reports whether the cycle reachable from cycleNodeID has
// any outgoing edge that leaves the cycle's strongly-connected
// component. The set of "in-cycle" nodes is approximated by the
// segment of `path` from cycleNodeID's first occurrence onward —
// these are the nodes on the current DFS ancestor stack that close
// back to cycleNodeID.
//
// An exit exists when any in-cycle node has an outgoing edge whose
// destination is NOT also in-cycle. This is the structural shape of
// a "tool-calling agent" pattern: chatbot → conditional({tools, END})
// → tools → chatbot, where the conditional's "END" branch is the
// exit. The cycle is bounded at runtime by the conditional's
// decision plus the framework's `recursion_limit` default.
func (c *CycleDetector) cycleHasExit(graph *domain.WorkflowGraph, cycleNodeID string, path []string) bool {
	if graph == nil {
		return false
	}
	cycleSet := make(map[string]bool)
	startIdx := -1
	for i, id := range path {
		if id == cycleNodeID {
			startIdx = i
			break
		}
	}
	if startIdx < 0 {
		// Self-loop — cycleNodeID points at itself. Single-node "cycle".
		cycleSet[cycleNodeID] = true
	} else {
		for i := startIdx; i < len(path); i++ {
			cycleSet[path[i]] = true
		}
		// The cycle closes back to cycleNodeID; ensure it's in the set.
		cycleSet[cycleNodeID] = true
	}
	for _, e := range graph.Edges {
		if !cycleSet[e.From] {
			continue
		}
		if !cycleSet[e.To] {
			return true
		}
	}
	// In addition to a structural exit edge, recognise nodes flagged
	// `config.has_end_branch=true` by the LangGraph parser. END /
	// `__end__` is a sentinel rather than a real node, so an
	// `add_conditional_edges("src", router_fn)` returning
	// `Literal[END, "back_to_loop"]` would otherwise look exit-less.
	// The parser sets `has_end_branch` on `src` when it encounters any
	// such sentinel destination so the cycle is correctly classified
	// as bounded. Dogfood: company-researcher
	// `route_from_reflection` (Literal[END, "research_company"]).
	for id := range cycleSet {
		n, ok := graph.Nodes[id]
		if !ok || n == nil {
			continue
		}
		if v, ok := n.Config["has_end_branch"]; ok {
			if b, ok := v.(bool); ok && b {
				return true
			}
		}
	}
	return false
}

// evaluateCycle inspects the cycle-entry node and produces an appropriate Finding.
// path is the DFS ancestor stack at the point the back-edge was discovered.
func (c *CycleDetector) evaluateCycle(graph *domain.WorkflowGraph, cycleNodeID string, path []string) domain.Finding {
	node, ok := graph.Nodes[cycleNodeID]
	if !ok {
		// Defensive: node missing from map.
		return domain.Finding{
			RuleName:         c.Name(),
			Severity:         domain.Critical,
			NodeID:           cycleNodeID,
			Message:          fmt.Sprintf("cycle detected at unknown node %q", cycleNodeID),
			Suggestion:       "Remove the cycle or add a Control node with max_iterations < 100.",
			Confidence:       1.0,
			ConfidenceReason: domain.ReasonExactStaticMatch,
		}
	}

	if !isLoopNode(node.Type) {
		// A cycle whose entry point is not a Loop/Control node.
		// Check whether a parent Loop/Control node manages this cycle.
		parentControl := c.findParentControl(graph, path)
		if parentControl != nil {
			// The cycle is inside a Loop (LoopAgent) node.
			// Severity depends on max_iterations of the parent Loop node.
			raw, exists := parentControl.Config["max_iterations"]
			if !exists || raw == nil {
				// No max_iterations on the parent Loop — unbounded cycle → Critical.
				return domain.Finding{
					RuleName: c.Name(),
					Severity: domain.Critical,
					NodeID:   cycleNodeID,
					Message: fmt.Sprintf(
						"cycle detected inside Loop node %q via sub-agent %q — MaxIterations not set: risk of infinite loop",
						parentControl.ID, cycleNodeID,
					),
					Suggestion:       "Set MaxIterations on the Loop (LoopAgent) node to prevent infinite loops.",
					Confidence:       1.0,
					ConfidenceReason: domain.ReasonExactStaticMatch,
				}
			}
			maxIter, err := toInt(raw)
			if err != nil || maxIter >= 100 {
				return domain.Finding{
					RuleName: c.Name(),
					Severity: domain.Info,
					NodeID:   cycleNodeID,
					Message: fmt.Sprintf(
						"cycle detected inside Loop node %q via sub-agent %q (max_iterations >= 100)",
						parentControl.ID, cycleNodeID,
					),
					Suggestion:       "Consider reducing max_iterations below 100 to limit long-running workflows.",
					Confidence:       1.0,
					ConfidenceReason: domain.ReasonExactStaticMatch,
				}
			}
			// max_iterations is set and < 100 — safe loop, no finding.
			return domain.Finding{}
		}
		// No parent Loop/Control found. Check whether the cycle has an
		// exit branch — i.e. some node in the cycle has an outgoing edge
		// to a target outside the cycle (typically a conditional `END`
		// branch in LangGraph's tool-calling agent pattern). If so, the
		// cycle is bounded by runtime (the conditional eventually picks
		// the exit branch, plus framework `recursion_limit` defaults), so
		// downgrade Critical → Warning. Without an exit, the cycle is
		// genuinely unbounded → keep Critical.
		if hasExit := c.cycleHasExit(graph, cycleNodeID, path); hasExit {
			return domain.Finding{
				RuleName: c.Name(),
				Severity: domain.Warning,
				NodeID:   cycleNodeID,
				Message: fmt.Sprintf(
					"bounded cycle through non-Loop node %q (type=%v): the cycle has an exit branch, but no explicit max_iterations / recursion_limit guard",
					cycleNodeID, node.Type,
				),
				Suggestion: "The cycle is bounded by a conditional exit (typical LangGraph tool-calling agent pattern). " +
					"Consider explicit `max_iterations` on a Loop wrapper or `graph.compile(recursion_limit=...)` " +
					"to surface the bound at the graph level rather than relying on the framework default.",
				Confidence:       0.9,
				ConfidenceReason: domain.ReasonExactStaticMatch,
			}
		}
		return domain.Finding{
			RuleName: c.Name(),
			Severity: domain.Critical,
			NodeID:   cycleNodeID,
			Message: fmt.Sprintf(
				"cycle detected at non-Loop node %q (type=%v): graph definition error",
				cycleNodeID, node.Type,
			),
			Suggestion: "Cycles must be managed by a Loop (LoopAgent) node. " +
				"Review the graph edges or add a Loop node to guard the cycle.",
			Confidence:       1.0,
			ConfidenceReason: domain.ReasonExactStaticMatch,
		}
	}

	// Loop/Control node — check max_iterations.
	raw, exists := node.Config["max_iterations"]
	if !exists || raw == nil {
		return domain.Finding{
			RuleName: c.Name(),
			Severity: domain.Critical,
			NodeID:   cycleNodeID,
			Message: fmt.Sprintf(
				"Loop node %q has a cycle but max_iterations is not set: risk of infinite loop",
				cycleNodeID,
			),
			Suggestion:       "Set max_iterations to a value less than 100 on the Loop node.",
			Confidence:       1.0,
			ConfidenceReason: domain.ReasonExactStaticMatch,
		}
	}

	maxIter, err := toInt(raw)
	if err != nil {
		// Could not parse the value; treat as missing.
		return domain.Finding{
			RuleName: c.Name(),
			Severity: domain.Critical,
			NodeID:   cycleNodeID,
			Message: fmt.Sprintf(
				"Loop node %q has max_iterations set to an unparseable value %q: risk of infinite loop",
				cycleNodeID, fmt.Sprint(raw),
			),
			Suggestion:       "Set max_iterations to a valid integer less than 100.",
			Confidence:       1.0,
			ConfidenceReason: domain.ReasonExactStaticMatch,
		}
	}

	if maxIter >= 100 {
		return domain.Finding{
			RuleName: c.Name(),
			Severity: domain.Warning,
			NodeID:   cycleNodeID,
			Message: fmt.Sprintf(
				"Loop node %q has max_iterations=%d (>= 100): high iteration count may cause long-running or expensive workflows",
				cycleNodeID, maxIter,
			),
			Suggestion:       "Consider reducing max_iterations below 100 or adding an early-exit condition.",
			Confidence:       1.0,
			ConfidenceReason: domain.ReasonExactStaticMatch,
		}
	}

	// max_iterations is set and < 100 — safe loop, no finding.
	return domain.Finding{}
}

// toInt converts common numeric types and string representations to int.
func toInt(v any) (int, error) {
	switch val := v.(type) {
	case int:
		return val, nil
	case int64:
		return int(val), nil
	case float64:
		return int(val), nil
	case string:
		n, err := strconv.Atoi(val)
		if err != nil {
			return 0, fmt.Errorf("cannot parse %q as int: %w", val, err)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("unsupported type %T for max_iterations", v)
	}
}

func init() {
	registerBuiltin(NewCycleDetector())
}
