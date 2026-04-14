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
// cycle is guarded by a Control node (LoopAgent equivalent) with a safe
// max_iterations bound.
//
// Severity rules:
//   - Control node in cycle, max_iterations not set → Critical
//   - Control node in cycle, max_iterations >= 100 → Warning
//   - Control node in cycle, max_iterations < 100 → no finding (safe loop)
//   - Non-Control node forms a cycle → Critical (graph definition error)
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
	var dfs func(nodeID string)
	dfs = func(nodeID string) {
		state[nodeID] = inProgress

		for _, edge := range graph.OutgoingEdges(nodeID) {
			target := edge.To

			switch state[target] {
			case inProgress:
				// Back edge: we found a cycle. The target node is the entry
				// point of the cycle and is the one responsible for bounding it.
				f := c.evaluateCycle(graph, target)
				// evaluateCycle returns a zero-value Finding when the cycle is safe
				// (Control node with max_iterations < 100). Skip those.
				if f.RuleName != "" {
					findings = append(findings, f)
				}

			case unvisited:
				// Target node not yet visited, recurse.
				if _, exists := graph.Nodes[target]; exists {
					dfs(target)
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
		dfs(graph.EntryNodeID)
	}

	// Also visit nodes not reachable from the entry to catch isolated cycles.
	for id := range graph.Nodes {
		if state[id] == unvisited {
			dfs(id)
		}
	}

	return findings
}

// evaluateCycle inspects the cycle-entry node and produces an appropriate Finding.
func (c *CycleDetector) evaluateCycle(graph *domain.WorkflowGraph, cycleNodeID string) domain.Finding {
	node, ok := graph.Nodes[cycleNodeID]
	if !ok {
		// Defensive: node missing from map.
		return domain.Finding{
			RuleName:   c.Name(),
			Severity:   domain.Critical,
			NodeID:     cycleNodeID,
			Message:    fmt.Sprintf("cycle detected at unknown node %q", cycleNodeID),
			Suggestion: "Remove the cycle or add a Control node with max_iterations < 100.",
		}
	}

	if node.Type != domain.NodeTypeControl {
		// A cycle whose entry point is not a Control node is always a graph error.
		return domain.Finding{
			RuleName: c.Name(),
			Severity: domain.Critical,
			NodeID:   cycleNodeID,
			Message: fmt.Sprintf(
				"cycle detected at non-Control node %q (type=%v): graph definition error",
				cycleNodeID, node.Type,
			),
			Suggestion: "Cycles must be managed by a Control (LoopAgent) node. " +
				"Review the graph edges or add a Control node to guard the loop.",
		}
	}

	// Control node — check max_iterations.
	raw, exists := node.Config["max_iterations"]
	if !exists || raw == nil {
		return domain.Finding{
			RuleName: c.Name(),
			Severity: domain.Critical,
			NodeID:   cycleNodeID,
			Message: fmt.Sprintf(
				"Control node %q has a cycle but max_iterations is not set: risk of infinite loop",
				cycleNodeID,
			),
			Suggestion: "Set max_iterations to a value less than 100 on the Control node.",
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
				"Control node %q has max_iterations set to an unparseable value %q: risk of infinite loop",
				cycleNodeID, fmt.Sprint(raw),
			),
			Suggestion: "Set max_iterations to a valid integer less than 100.",
		}
	}

	if maxIter >= 100 {
		return domain.Finding{
			RuleName: c.Name(),
			Severity: domain.Warning,
			NodeID:   cycleNodeID,
			Message: fmt.Sprintf(
				"Control node %q has max_iterations=%d (>= 100): high iteration count may cause long-running or expensive workflows",
				cycleNodeID, maxIter,
			),
			Suggestion: "Consider reducing max_iterations below 100 or adding an early-exit condition.",
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
