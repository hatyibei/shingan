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

// Analyze performs DFS from the entry node and reports cycle findings.
func (c *CycleDetector) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}

	state := make(map[string]visitState, len(graph.Nodes))
	var findings []domain.Finding

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
				f := c.evaluateCycle(graph, target, currentPath)
				// evaluateCycle returns a zero-value Finding when the cycle is safe
				// (Control node with max_iterations < 100). Skip those.
				if f.RuleName != "" {
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

// evaluateCycle inspects the cycle-entry node and produces an appropriate Finding.
// path is the DFS ancestor stack at the point the back-edge was discovered.
func (c *CycleDetector) evaluateCycle(graph *domain.WorkflowGraph, cycleNodeID string, path []string) domain.Finding {
	node, ok := graph.Nodes[cycleNodeID]
	if !ok {
		// Defensive: node missing from map.
		return domain.Finding{
			RuleName:   c.Name(),
			Severity:   domain.Critical,
			NodeID:     cycleNodeID,
			Message:    fmt.Sprintf("cycle detected at unknown node %q", cycleNodeID),
			Suggestion: "Remove the cycle or add a Control node with max_iterations < 100.",
			Confidence: 1.0,
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
					Suggestion:  "Set MaxIterations on the Loop (LoopAgent) node to prevent infinite loops.",
					Confidence: 1.0,
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
					Suggestion: "Consider reducing max_iterations below 100 to limit long-running workflows.",
					Confidence: 1.0,
				}
			}
			// max_iterations is set and < 100 — safe loop, no finding.
			return domain.Finding{}
		}
		// No parent Loop/Control found — genuine graph definition error.
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
			Confidence: 1.0,
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
			Suggestion: "Set max_iterations to a value less than 100 on the Loop node.",
			Confidence: 1.0,
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
			Suggestion: "Set max_iterations to a valid integer less than 100.",
			Confidence: 1.0,
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
			Suggestion: "Consider reducing max_iterations below 100 or adding an early-exit condition.",
			Confidence: 1.0,
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
