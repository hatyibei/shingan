> 🌐 Language: **English** | [日本語](./parallel-branches.ja.md)

# max_parallel_branches — Parallel-Execution Fan-out Threshold Check

## Overview

The `max_parallel_branches` rule inspects the number of outgoing edges (fan-out degree)
from each node in a workflow graph and detects excessive parallel execution.

When ParallelAgent / LoopAgent / SequentialAgent fan out into many sub-agents simultaneously,
there is risk of API rate-limit overruns, resource exhaustion, and runaway costs.

## Detection Logic

For each node in the graph, count the outgoing edges (fan-out) and classify with the following thresholds.

| fan-out | Severity | Confidence | Description |
|---|---|---|---|
| >= 100 | **Critical** | 1.0 | Almost certain to trigger API rate-limit overruns or system failures |
| >= 20 | **Warning** | 0.9 | Likely to exceed parallel limits of many model APIs |
| >= 10 | **Info** | 0.7 | Caution required. Should be reviewed for production use |
| < 10 | (no finding) | — | Safe range |

## Exception — `max_concurrency` Setting

If a node's `Config["max_concurrency"]` is set, the node is considered to be explicitly
controlling concurrency, and the threshold check is skipped.

```json
{
  "id": "orchestrator",
  "name": "SafeOrchestrator",
  "type": "llm",
  "config": {
    "max_concurrency": 5
  }
}
```

With the configuration above, no finding is emitted even if fan-out is 100.

## Relationship with ParallelAgent / SequentialAgent

ADK-Go's `ParallelAgent` runs multiple `SubAgents` in parallel.
In Shingan's JSON format, a node playing the ParallelAgent role is represented as an orchestrator node,
and each outgoing edge to a SubAgent counts toward the fan-out.

```
orchestrator -> sub_agent_0
orchestrator -> sub_agent_1
...
orchestrator -> sub_agent_N   <- fan-out = N+1
```

For `SequentialAgent`, the chain structure leaves each node with fan-out = 1, so no finding is produced.

## Recommended Mitigations

### 1. Chunked Fan-out (Recommended)

Instead of fanning out into 100 workers directly, group them into chunks of 10 with intermediate coordinators.

```
orchestrator -> chunk_a (10 workers)
orchestrator -> chunk_b (10 workers)
orchestrator -> chunk_c (10 workers)
```

Each chunk has fan-out = 10 (Info) and the orchestrator itself has fan-out = 3 (no finding).

### 2. `max_concurrency` Setting

If your existing orchestrator already enforces rate-limit control internally,
setting `Config["max_concurrency"]` will suppress Shingan's detection.

```json
{
  "config": {
    "max_concurrency": 5
  }
}
```

## Test Data

| File | Contents |
|---|---|
| `testdata/parallel/high_fanout.json` | fan-out=100 -> Critical fires |
| `testdata/parallel/chunked.json` | Chunked fan-out structure -> no finding |
| `testdata/parallel/max_concurrency.json` | max_concurrency=5 -> no finding |

## Verification Commands

```bash
# Confirm Critical fires
./shingan analyze --format json --input testdata/parallel/high_fanout.json --output markdown

# Real-time generation with shingan-gen + analysis
./shingan-gen --pattern high-fanout --size 100 --seed 42 | ./shingan analyze --format json --input /dev/stdin --output markdown

# Warning threshold (fan-out=20)
./shingan-gen --pattern high-fanout --size 20 --seed 42 | ./shingan analyze --format json --input /dev/stdin --output markdown
```

## Related Rules

- `loop_guard` — Detects LoopAgent without `MaxIterations` set (infinite-loop prevention)
- `cost_estimation` — Estimates cost of expensive models inside loops
