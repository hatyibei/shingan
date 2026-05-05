package rules

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hatyibei/shingan/domain"
)

// CircularDepAgents flags multi-agent workflows in which one agent can transfer
// control (directly or via a transfer tool) to another agent that can transfer
// control back. Without an explicit `max_handoffs` budget or an orchestrator
// pattern, this risks infinite delegation between agents.
//
// Tier: Path (ADR-007). The detection requires a cycle search restricted to
// agent nodes, which is more involved than a single-node decision but cheaper
// than the general cycle_detection over the whole graph. Sources are agent
// nodes; Sinks are unused (we walk forward from each source). The Path tier
// gives us the place to share `agentSet` across the per-source DFS calls.
//
// Relationship with cycle_detection (Global): the two rules INTENTIONALLY
// overlap. cycle_detection fires Critical for any structural back-edge, while
// circular_dep_agents fires Warning specifically for the agent-delegation
// pattern. Surfacing both makes it easy to spot delegation cycles while still
// flagging the broader graph error.
//
// ConfidenceReason policy (ADR-008):
//   - 2-agent and 3+ agent cycles → ReasonExactStaticMatch.
//     Rationale: once a node is identified as an agent, the cycle structure
//     itself is deterministic (DFS back-edge over the agent-induced subgraph).
//   - Self-reference (single agent → itself) → ReasonHeuristicPattern.
//     Rationale: self-recursion can be intentional (a planner that re-plans),
//     so we treat the static signal more cautiously and rate-limit the noise
//     by emitting Info severity with a heuristic_pattern tag.
//
// Severity rules:
//   - 2-agent cycle (A → B → A)        → Warning, Confidence 0.85, exact_static_match
//   - 3+ agent cycle (A → B → C → A)   → Warning, Confidence 0.75, exact_static_match
//   - self-reference (A → A)           → Info,    Confidence 0.6,  heuristic_pattern
//
// A "cycle that contains only one agent and several tools" is NOT a delegation
// cycle (no second agent to delegate to). That case is silently ignored here
// and remains the responsibility of cycle_detection.
type CircularDepAgents struct{}

// NewCircularDepAgents returns a ready-to-use CircularDepAgents rule.
func NewCircularDepAgents() *CircularDepAgents {
	return &CircularDepAgents{}
}

// Name returns the unique rule identifier.
func (c *CircularDepAgents) Name() string {
	return "circular_dep_agents"
}

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (c *CircularDepAgents) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     c.Name(),
		Severity: domain.Warning,
		Fixable:  false,
	}
}

// isAgentNode reports whether a node is a multi-agent delegation participant.
// It accepts two signals (either suffices):
//
//  1. Config["agent_role"] is a non-empty string (most LangGraph / ADK
//     templates), or
//  2. Config["sub_agents"] is set to a non-nil value (parent agents that
//     declare sub-agents are themselves agents).
//
// Both signals are inspected only on NodeTypeLLM. Tool nodes are never agents
// even if they carry a `transfer_to_agent` name — a transfer tool is the
// router, not a participant.
func isAgentNode(n *domain.Node) bool {
	if n == nil || n.Type != domain.NodeTypeLLM || n.Config == nil {
		return false
	}
	if role := stringConfig(n, "agent_role"); role != "" {
		return true
	}
	if v, ok := n.Config["sub_agents"]; ok && v != nil {
		return true
	}
	return false
}

// Sources implements domain.PathRule. Returns every agent node, sorted by ID
// for deterministic output. The ordering matters because we emit one finding
// per agent that participates in a cycle, and a stable order keeps test
// expectations and reporter output reproducible.
func (c *CircularDepAgents) Sources(g *domain.WorkflowGraph) []*domain.Node {
	if g == nil {
		return nil
	}
	var out []*domain.Node
	for _, n := range g.Nodes {
		if isAgentNode(n) {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Sinks implements domain.PathRule. CircularDepAgents drives traversal forward
// from each source so it does not consume sinks; we return nil.
func (c *CircularDepAgents) Sinks(g *domain.WorkflowGraph) []*domain.Node { return nil }

// Propagate implements domain.PathRule. It walks forward from each agent
// source via DFS, looking for a path that lands back on an agent (forming a
// delegation cycle).
func (c *CircularDepAgents) Propagate(ctx *domain.PathContext) []domain.Finding {
	if ctx == nil || ctx.Graph == nil || len(ctx.Sources) == 0 {
		return nil
	}
	return runCircularDepAgentsDFS(ctx.Graph, ctx.Sources)
}

// Analyze keeps the legacy AnalysisRule contract alive.
func (c *CircularDepAgents) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	sources := c.Sources(graph)
	if len(sources) == 0 {
		return nil
	}
	return runCircularDepAgentsDFS(graph, sources)
}

// runCircularDepAgentsDFS executes the cycle detection. For each agent source
// (in deterministic ID order), a DFS walks forward via outgoing edges until it
// re-encounters the source — the discovered agent set then drives Severity:
//
//   - one agent only and it is the source itself ⇒ self-reference (Info)
//   - exactly two distinct agents on the cycle path ⇒ 2-agent cycle (Warning, 0.85)
//   - three or more distinct agents ⇒ 3+ agent cycle (Warning, 0.75)
//
// The sequence of cycles is deduped per `(severity, sortedAgentSet)` so two
// agents in a 2-cycle do not emit two near-duplicate findings.
func runCircularDepAgentsDFS(graph *domain.WorkflowGraph, sources []*domain.Node) []domain.Finding {
	agentIDs := make(map[string]bool, len(sources))
	for _, n := range sources {
		agentIDs[n.ID] = true
	}

	type cycleInfo struct {
		members []string // sorted agent IDs forming the cycle
		path    []string // first-found path the cycle was discovered on
		isSelf  bool
	}

	emitted := make(map[string]bool)
	var findings []domain.Finding

	report := func(start *domain.Node, info cycleInfo) {
		// Dedup by (sorted agent members + isSelf flag).
		key := strings.Join(info.members, "|")
		if info.isSelf {
			key = "SELF::" + start.ID
		}
		if emitted[key] {
			return
		}
		emitted[key] = true

		var (
			severity   domain.Severity
			confidence float64
			reason     domain.ConfidenceReason
			msg        string
			suggestion string
		)

		switch {
		case info.isSelf:
			severity = domain.Info
			confidence = 0.6
			reason = domain.ReasonHeuristicPattern
			msg = fmt.Sprintf(
				"agent %q references itself (self-handoff). This may be intentional recursion; verify a depth/budget guard.",
				start.ID,
			)
			suggestion = fmt.Sprintf(
				"Agent %q can hand off to itself. If this is intentional self-recursion (e.g. iterative planner), set an explicit `max_handoffs` budget; otherwise switch to a top-level orchestrator that controls the loop.",
				start.ID,
			)
		case len(info.members) == 2:
			severity = domain.Warning
			confidence = 0.85
			reason = domain.ReasonExactStaticMatch
			a, b := info.members[0], info.members[1]
			msg = fmt.Sprintf(
				"agents %q and %q form a 2-agent delegation cycle (%s)",
				a, b, strings.Join(info.path, " → "),
			)
			suggestion = fmt.Sprintf(
				"Agent %s can transfer control to agent %s which can transfer back to %s. Without a depth/budget guard this risks infinite delegation. Consider an `orchestrator` pattern (top-level agent makes all transfer decisions) or explicit `max_handoffs`.",
				a, b, a,
			)
		default: // 3+
			severity = domain.Warning
			confidence = 0.75
			reason = domain.ReasonExactStaticMatch
			msg = fmt.Sprintf(
				"%d agents form a delegation cycle: %s",
				len(info.members), strings.Join(info.path, " → "),
			)
			suggestion = fmt.Sprintf(
				"Agents %s form a delegation cycle. Without a depth/budget guard this risks infinite delegation. Consider an `orchestrator` pattern (top-level agent makes all transfer decisions) or explicit `max_handoffs`.",
				strings.Join(info.members, ", "),
			)
		}

		findings = append(findings, domain.Finding{
			RuleName:         "circular_dep_agents",
			Severity:         severity,
			NodeID:           start.ID,
			Message:          msg,
			Suggestion:       suggestion,
			Confidence:       confidence,
			ConfidenceReason: reason,
		})
	}

	for _, src := range sources {
		// Self-reference detection: single edge src → src. The DFS below would
		// find this too, but isolating it keeps the agent-set cardinality
		// branching simpler.
		selfDetected := false
		for _, e := range graph.OutgoingEdges(src.ID) {
			if e.To == src.ID {
				selfDetected = true
				break
			}
		}
		if selfDetected {
			report(src, cycleInfo{members: []string{src.ID}, path: []string{src.ID, src.ID}, isSelf: true})
			// continue: a self-edge does not preclude a longer cycle, but
			// almost always coincides with one only when the user explicitly
			// designed it that way. Either way the dedup key is different.
		}

		// DFS forward looking for a path that returns to src.
		visited := map[string]bool{src.ID: true}
		var path []string
		path = append(path, src.ID)

		var dfs func(node string) bool
		dfs = func(node string) bool {
			for _, e := range graph.OutgoingEdges(node) {
				next := e.To
				if next == src.ID && len(path) >= 2 {
					// Found a cycle that returns to src. Collect distinct
					// agent IDs along path (path includes src once at index 0).
					seen := make(map[string]bool)
					var members []string
					for _, id := range path {
						if agentIDs[id] && !seen[id] {
							seen[id] = true
							members = append(members, id)
						}
					}
					// Only multi-agent cycles. Single-agent cycles via tools
					// are not delegation cycles.
					if len(members) >= 2 {
						sort.Strings(members)
						fullPath := append(append([]string{}, path...), src.ID)
						report(src, cycleInfo{members: members, path: fullPath})
						return true
					}
					// Single-agent cycle via tools: not a delegation cycle.
					continue
				}
				if visited[next] {
					continue
				}
				if _, exists := graph.Nodes[next]; !exists {
					continue
				}
				visited[next] = true
				path = append(path, next)
				if dfs(next) {
					return true
				}
				path = path[:len(path)-1]
				delete(visited, next)
			}
			return false
		}

		dfs(src.ID)
	}

	return findings
}

func init() {
	registerBuiltin(NewCircularDepAgents())
}
