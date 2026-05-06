> 🌐 Language: **English** | [日本語](./circular-dep-agents.ja.md)

# circular_dep_agents — Design & Implementation Doc

> **Target version**: Phase 2 (v0.6 series, reliability batch)
> **Implementation file**: `domain/rules/circular_dep_agents.go`
> **Tests**: `domain/rules/circular_dep_agents_test.go` (13 cases)
> **Tier (ADR-007)**: Path rule — implements Sources / Sinks / Propagate (Sinks is nil)

---

## 1. Background & Motivation

In multi-agent workflows (LangGraph, ADK, CrewAI, AutoGen, etc.), it is common for agent A to delegate work to agent B, and for B to hand it back to A based on its judgment — a so-called **delegation cycle**:

- A decides "the analysis is insufficient" and re-issues the request to B
- B decides "I lack authority" and bounces it back to A
- Without a depth/budget guard, this becomes infinite delegation → token waste / context explosion / fatal latency

`circular_dep_agents` **detects this statically from the graph in advance**. Unlike `cycle_detection` (Global), which inspects back-edges across the entire graph, this rule **focuses on cycles between agent nodes** to issue warnings in the context that matters for agent workflows.

> **Relationship with cycle_detection (intentional overlap)**: cycle_detection reports any back-edge in the whole graph at Critical severity, whereas circular_dep_agents complements it at Warning severity for the agent-delegation pattern. The two rules are designed to be complementary along Severity (Critical vs Warning) and Suggestion (graph cleanup vs orchestrator pattern).

---

## 2. Detection Targets

### 2.1 Source (agent nodes)

Among `NodeType.LLM` nodes, those matching either of the following two signals are treated as agents:

| Signal | Example |
|---|---|
| `Config["agent_role"]` is a non-empty string | `"agent_role": "planner"` (LangGraph / ADK convention) |
| `Config["sub_agents"]` is non-nil | `"sub_agents": ["worker_a", "worker_b"]` (orchestrator declarations) |

Tool nodes (e.g. `transfer_to_agent` tools) are treated as **routers**, not agents:
- Even if intermediate nodes are transfer tools, the path agent → tool → agent is still folded into the cycle
- However, the agent count **excludes** tools, so "2 agents in cycle" means "2 distinct agent IDs"

### 2.2 Cycle Detection Algorithm

Starting from each source (agent), DFS forward through outgoing edges and look for paths that return to the source itself. The number of **distinct agent IDs on the cycle** drives the Severity:

```
distinct agents on cycle = K
K == 1 (self-edge or tool-mediated revisit)
    → if direct self-edge (A → A) → Info (self-reference)
    → if via tools only         → not detected (cycle_detection's domain)
K == 2                          → Warning (2-agent delegation cycle)
K >= 3                          → Warning (multi-agent delegation cycle)
```

### 2.3 Severity / Confidence Matrix

| Situation | Severity | Confidence | ConfidenceReason | Rationale |
|---|---|---|---|---|
| 2-agent cycle (A → B → A) | **Warning** | 0.85 | `exact_static_match` | Once agents are identified, cycle detection is deterministic; the most typical pattern |
| 3+ agent cycle (A → B → C → A) | **Warning** | 0.75 | `exact_static_match` | Three or more hops increase the chance of an "intentional hand-off pattern", so we lower it to 0.75 |
| Single agent self-reference (A → A) | **Info** | 0.6 | `heuristic_pattern` | Self-recursion (re-planning, etc.) can be intentional |
| Single agent in cycle via tools only | (does not fire) | — | — | This is cycle_detection's territory |

> **Rationale for ConfidenceReason choice (ADR-008)**:
> - For 2-agent / 3+ agent cycles, once agent identification succeeds, the graph cycle detection is deterministic — hence `exact_static_match`
> - Self-reference is more likely intentional recursion (planner re-plan / iterative refinement), so we keep it modest with `heuristic_pattern`
> - **Agent identification itself is heuristic** (matching Config key names), but suppressing Severity to Warning already absorbs that uncertainty — we do not reflect it in the Reason

---

## 3. Detection Algorithm

```
Step 1: Sources(g) — set of NodeType.LLM nodes carrying agent_role / sub_agents (O(V))
Step 2: Sinks(g) is nil. Run forward DFS from each source.
Step 3: For each source:
        a. If a direct self-edge (src → src) exists, emit one Self-reference Info finding
        b. DFS for paths where next.To == src
        c. When found, extract distinct agent IDs along the path (sort + dedup)
        d. Decide Severity and emit only when agent count >= 2 (count == 1 is a
           tool-mediated cycle and is skipped)
        e. Dedup key is (sorted agent IDs) or "SELF::<id>"
Step 4: After scanning every source, return findings
```

**Complexity**: O(agents × (V+E)). In typical multi-agent workflows agents << V, so effectively O(V+E).

**Dedup**: Cycles fire only once per `(sorted agent set)` key. A 2-agent cycle (A,B) is detected twice (once from A, once from B), but the findings are folded into a single entry.

---

## 4. Implementation Design Decisions

### 4.1 Why this lives at the Path tier

- Local rules cannot see graph adjacency
- Doing whole-graph DFS at the Global tier would mean redoing **agent identification + cycle detection** every time (sharing a DFS with cycle_detection still leaves the agent filter as path-specific)
- Running DFS from each agent at the Path tier finds cycles in the agent-induced subgraph with the minimum necessary work

`Sinks` is unused, but the `PathRule` interface allows Sinks to be nil — same shape as CostAnalyzer / RetryStorm.

### 4.2 Why we tolerate overlap with cycle_detection

It is **intentional** that both rules report the same cycle at different Severities:

- cycle_detection emits Critical based on a logical determination of whether "the graph structure may produce an infinite loop" (combination of loop_guard + max_iterations + parent Loop)
- circular_dep_agents emits Warning for cycles that only have meaning in the runtime context of "**agent-to-agent transfer**"
- Through Severity and Suggestion (graph cleanup vs orchestrator pattern), users can act on two distinct pieces of advice

This is also consistent with the three-tier separation of ADR-007: cycle_detection sits at Global (whole-graph DFS), circular_dep_agents at Path (agent-induced subgraph), with different complexity profiles.

### 4.3 Why "single agent + tools cycle" does not fire

A cycle like `agent_a → tool_a → tool_b → agent_a` is **not agent-to-agent delegation** (the tools are simply recursing). Warning on this from circular_dep_agents would mass-produce false positives, so we require distinct agent count >= 2. Only the lone-agent self-edge `agent_a → agent_a` is reported, at Info, because the recursion intent is visible.

### 4.4 Why self-reference is Info

A planner that "calls itself again to refine the plan" is a legitimate pattern that appears frequently in LangGraph's `tool_node` loop or AutoGen's reflection pattern. Reporting at Critical / Warning would generate massive false positives, so we keep it as Info + heuristic_pattern — noise that "you can ignore if you don't care". `--min-confidence 0.7` suppresses it entirely.

---

## 5. Recommended Mitigation Patterns

### 5.1 Orchestrator Pattern (recommended)

```python
# OK: orchestrator monopolizes transfer authority
class OrchestratorAgent:
    sub_agents = [PlannerAgent(), WorkerAgent()]

    def step(self, state):
        if state.needs_planning:
            return self.sub_agents[0].plan(state)  # planner returns only to orchestrator
        return self.sub_agents[1].execute(state)

# planner and worker cannot transfer to each other (only via orchestrator)
```

### 5.2 max_handoffs Budget

```python
# OK: cap the number of handoffs
class PlannerAgent:
    max_handoffs = 5  # forced termination after 5 hand-offs

    def transfer(self, target):
        if self.handoff_count >= self.max_handoffs:
            raise StopIteration("max_handoffs exceeded")
        ...
```

### 5.3 Depth Budget

```python
# OK: cut off by depth
def execute(state, depth=0):
    if depth >= MAX_DEPTH:
        return state
    next_agent = pick(state)
    return next_agent.execute(state, depth + 1)
```

### 5.4 Library-Specific Support Examples

| Library | Recommended API |
|---|---|
| LangGraph | `compile(checkpointer=..., interrupt_before=[...])` for human-in-the-loop break points |
| OpenAI Swarm | `max_turns` parameter to bound turn count |
| AutoGen | `Agent.max_consecutive_auto_reply` |
| CrewAI | `Crew(max_iter=N)` |
| ADK-Go | `AgentConfig.MaxSteps` (implementation-dependent) |

---

## 6. Known False Positives / False Negatives

### False Positive

- **Intentional bidirectional orchestrator-sub_agent**: `orchestrator → planner → orchestrator` is treated as a cycle by this rule but is legitimate as the orchestrator pattern. Severity is held at Warning, and users can suppress it via `--min-confidence 0.9`.
- **Conditional edges that never actually loop**: branches like `if step == 1 → planner, else → output` look like cycles statically because both edges exist, but the loop never materializes at runtime. Confidence 0.85 already accounts for this level of uncertainty.

### False Negative

- **Custom keys for agent identification**: Frameworks that declare agents via `Config["role"]` / `Config["agent_id"]` etc. are not detected. We plan to extend the agent-identification DSL in the second half of Phase 2.
- **Dynamically generated agents**: Patterns that add nodes at runtime via `LangGraph.add_node()` are not present at parse time and cannot be detected. We plan to handle them via the LangGraph parser extension path (Phase 1 P).
- **Transfers written as direct LLM calls instead of tools**: Unless both LLM nodes explicitly declare `agent_role` in Config, they are not in the source set and are missed.

---

## 7. Related Rules / ADRs

- **ADR-006**: ESLint visitor pattern — contrast with Local rules. This rule is at the Path tier.
- **ADR-007**: Local / Path / Global three-tier separation — basis for this rule sitting at the Path tier, plus the complexity-profile difference vs cycle_detection.
- **ADR-008**: ConfidenceReason — using `exact_static_match` (multi-agent cycle) and `heuristic_pattern` (self-ref) discriminately.
- `cycle_detection` (Global) — complementary rule that reports any back-edge in the entire graph at Critical severity. **Designed to deliberately overlap** so both rules can fire.
- `loop_guard` (Local) — detects missing `max_iterations` on standalone Loop nodes. Not an agent cycle, but a sibling lineage of "infinite loop" rules.
- `prompt_injection_sink` (Path) — used together, this raises value when an attack surface emerges in the middle of agent-to-agent transfers.
