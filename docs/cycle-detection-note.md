> 🌐 Language: **English** | [日本語](./cycle-detection-note.ja.md)

# `cycle_detection` — the "non-Control node" message

## Update (2026-04-14)

All three options below were implemented in v0.2:

- **Option A (message wording)** — when a cycle is detected, walk back through the DFS path looking for a parent Control node (e.g. `LoopAgent`). If one is found, the message becomes `"cycle detected inside Control node \"<parent>\" via sub-agent \"<node>\""`.
- **Option B (severity split)** — inside a parent Control with no `max_iterations` configured → `Warning`, with `max_iterations >= 100` → `Info`, no parent Control at all → `Critical` (genuine graph-definition error).
- **Option C (independent `LoopGuardChecker` rule)** — `domain/rules/loopguard.go` adds a dedicated rule that flags Control nodes missing `max_iterations` as Critical, registered as `"loop_guard"` in the analyzer factory (now 6+ rules total).

Example output for `infinite_loop_unbounded.go`:

```
loop_guard      | Critical | unbounded_loop | LoopAgent "unbounded_loop" has no MaxIterations configured — potential infinite loop
cycle_detection | Warning  | classifier     | cycle detected inside Control node "unbounded_loop" via sub-agent "classifier"
```

## Original behaviour (pre-v0.2)

Running Shingan on samples like `testdata/real/infinite_loop.go` or `examples/runtime/infinite_loop_unbounded.go` — both **`LoopAgent`s without `MaxIterations`** — produced this finding:

```
cycle_detection Critical
  node: classifier
  message: cycle detected at non-Control node "classifier" (type=llm): graph definition error
```

The message says "cycle on a non-Control node", but the cycle is actually inside a `LoopAgent` (a Control node). This reads as misleading at first glance.

## Technical background

### How the ADK-Go parser expands nodes

The ADK-Go parser (`infrastructure/parser/adkgo.go`) expands `loopagent.New(Config{ AgentConfig: agent.Config{ SubAgents: [A, B] } })` into:

- The `LoopAgent` itself → one node (`NodeTypeControl`, `Config["max_iterations"]`)
- Each sub-agent `[A, B]` → its own node (`NodeTypeLLM` etc.)
- Edges from the `LoopAgent` to A and B
- **Loop-back edges A → B → A** (last sub-agent back to first)

At this point the cycle is `A → B → A`. Since A and B are `NodeTypeLLM`, the cycle is detected on a "non-Control node".

### Severity logic in `CycleDetector` (`domain/rules/cycle.go`)

```go
if cycleNode.Type != domain.NodeTypeControl {
    // Critical: graph definition error
}
// Otherwise check max_iterations on the Control node
```

The check looks at the **first node where the cycle is closed**, not at the enclosing `LoopAgent`. With sub-agent expansion, that first node is an LLM agent, so the result lands on Critical.

## Is this a spec or a bug?

**Intended behaviour**, for two reasons:

1. **Visibility** — users need to understand which sub-agents are repeating in order to fix the loop. Pointing at the actual node in the cycle (`classifier`) gives a clearer fix target than pointing at the `LoopAgent` wrapper.
2. **Catches cycles outside `LoopAgent` too** — if the user accidentally creates a circular reference inside a `SequentialAgent`, the same rule should still fire. Looking at "is this node inside a managed loop?" generalises better than "is the cycle node a Control node?".

The wording, however, was the weak point — `"graph definition error"` is wrong when a `LoopAgent` is managing the cycle. v0.2 fixed that.

## Why the v0.2 split helps

| Concern | Rule that catches it | Severity |
|---------|----------------------|----------|
| `LoopAgent` with no `max_iterations` | `loop_guard` | Critical |
| Cycle inside a managed `LoopAgent`, bounded | `cycle_detection` | Warning / Info |
| Cycle outside any Control node | `cycle_detection` | Critical |

Splitting `cycle_detection` and `loop_guard` separates "is there a loop?" from "is the loop bounded?", so users see one finding per concern instead of one ambiguous message.

## Open follow-ups

- [x] v0.2: include parent Control context (`LoopAgent` name) in `cycle_detection` messages — **shipped**
- [x] v0.2: introduce the standalone `loop_guard` rule — **shipped**
- [ ] v0.x: have the ADK-Go parser carry the `LoopAgent → SubAgents` "managed-by" relationship as edge metadata (e.g. `Edge.Kind = "loop_managed"`) so future rules don't need to re-derive it.

The follow-ups are reproducible via `examples/runtime/infinite_loop_unbounded.go`, so Shingan's own test suite can regression-test them.
