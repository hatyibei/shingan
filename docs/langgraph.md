> 🌐 Language: **English** | [日本語](./langgraph.ja.md)

# LangGraph Support

> Phase 1 / primary front (ADR-011) — a Shingan parser for Python AI agents that analyses LangGraph `StateGraph` definitions.

## Overview

Shingan can statically analyse LangGraph-authored agent definitions **before they ever run**. It extracts the nodes, edges, conditional_edges, and entry_point built via the `langgraph.graph.StateGraph` API and converts them into Shingan's generic `WorkflowGraph`.

Implementation strategy: a **long-lived Python subprocess** plus JSON-RPC. Instead of forking a fresh Python interpreter from the Go process on every call, Shingan keeps one worker per session and exchanges newline-delimited JSON over stdin/stdout (consistent with the design in ADR-009).

```
┌──────────────────────┐  newline-JSON RPC  ┌────────────────────────┐
│ shingan (Go process) │ ◄─────────────────►│ scripts/export_…py     │
│   LangGraphParser    │                    │ (Python long-lived     │
│   PythonWorker       │                    │  worker)               │
└──────────────────────┘                    └────────────────────────┘
```

## Installation

To enable the LangGraph parser you need Python 3.10+ and the `langgraph` package:

```bash
python3 -m pip install -r scripts/requirements-shim.txt
# or, at minimum:
python3 -m pip install "langgraph>=0.2.0"
```

Even in environments without Python or langgraph, building shingan and analysing other formats (json/adk-go/samurai) still works. The availability check only runs when you specify `--format=langgraph`, and when it fails the command stops with a clear error message:

```text
create langgraph parser: langgraph parser: Python 3.x and `pip install langgraph` required for LangGraph format
```

## Usage

A single file:

```bash
shingan analyze --format langgraph --input agent.py --output markdown
```

A directory (recursively scans every `.py` file and merges the results):

```bash
shingan analyze --format langgraph --input ./agents/ --output sarif --output-file findings.sarif
```

Typical CI invocation (progressive adoption):

```bash
shingan analyze \
  --format langgraph \
  --input ./agents \
  --baseline .shingan/baseline.json \
  --since main
```

## Supported LangGraph features

| Feature | Support | Notes |
|---|---|---|
| `StateGraph(State)` instance detection | OK | Picks the first instance per module |
| `add_node(name, fn)` | OK | SourcePos populated via `inspect.getsourcefile/getsourcelines` |
| `add_edge(from, to)` | OK | Static edge |
| `add_conditional_edges(from, fn, mapping)` | OK (over-approximation) | Each mapping key is recorded as an `Edge.Condition` and every candidate is emitted as an edge |
| `START` / `END` sentinels | Virtualised (not materialised as nodes) | Treated as pseudo sentinels just like LangGraph itself: the `x` in `add_edge(START, x)` is promoted to `entry_node_id`, and `add_edge(y, END)` is dropped. This is a critical adjustment to keep Shingan's `loop_guard` / `reachability` rules from misfiring (it also avoids the `NodeTypeControl` ⇒ `NodeTypeLoop` backward-compat alias) |
| `set_entry_point(...)` / `entry_point` attribute | OK | Falls back to reading the graph object's `entry_point` attribute when `add_edge(START, ...)` cannot supply it |
| `MessageGraph` / `Graph` subclasses | Partial | Detected by class-name match (with a fallback to private attributes such as `_nodes`) |
| Graphs constructed via `builder.compile()` | OK | Reaches the StateGraph through the compiled object's `.builder` / `.graph` attribute |

### Unsupported / known limitations

- **Dynamic `add_node` (constructed at runtime)**: graphs that are not assembled by the time the module is imported cannot be detected. Typical LangGraph code constructs the graph at the module top level, so this rarely matters in practice.
- **Subgraphs (StateGraph as node)**: child graphs are deferred to Phase 2 (ADR-013). Today only the parent StateGraph is expanded.
- **ReAct's dynamic tool selection**: when `should_continue()` returns a target chosen at runtime, only the candidates enumerated in the mapping appear as edges. If the tool actually invoked is outside the mapping, the parser cannot see it (the over-approximation limit, ADR-013).
- **Multi-module layouts**: `parse_file` temporarily prepends `os.path.dirname(path)` to `sys.path` for the target `.py`. Imports from other locations depend on the runtime `sys.path`.

## Confidence and ConfidenceReason

In Phase 1 the parser fills the `WorkflowGraph` `metadata.conditional_edge_reason` with `over_approximated_dynamic`. Surfacing it on each Finding waits until Track R (the visitor-pattern refactor) lands (ADR-006/008).

Expected combinations:

| Node / edge kind | Confidence | ConfidenceReason (after Track R) |
|---|---|---|
| `add_edge(a, b)` | 1.0 | `exact_static_match` |
| Each mapping value of `add_conditional_edges` | 0.8 | `over_approximated_dynamic` |
| `START` → `entry_point` bridge | 1.0 | `exact_static_match` |
| NodeType inferred from handler name (`tool` / `llm`) | 0.6 | `name_heuristic` |

## Samples

There are five reference samples under `testdata/langgraph/`:

| File | Pattern | Expected findings |
|---|---|---|
| `simple_chain.py` | Three nodes in series (START → classify → respond → END) | none (clean) |
| `branching.py` | 3-way branch via `add_conditional_edges` | none (clean; over-approximation surfaces every branch edge) |
| `react_loop.py` | model⇄tools loop, with a termination condition | `cycle_detection` (Critical) / `loop_guard` (Warning) |
| `rag.py` | RAG retrieval → LLM → outbound webhook | `pii_leak_scanner` (Warning, after Track R) |
| `multi_agent.py` | Supervisor + 3 workers, each worker loops back to the supervisor | findings around `cycle_detection` |

The expected `WorkflowGraph` for each sample lives under `testdata/langgraph/expected/*.json` (used by the E2E golden tests).

## Example output (`react_loop.py`)

```bash
$ shingan analyze --format langgraph --input testdata/langgraph/react_loop.py --output markdown
# Findings (2)

## Critical: cycle_detection
- Node: tools → model
- Confidence: 1.0 (DFS back-edge)
- Message: cycle detected: tools → model → tools

## Warning: loop_guard
- Node: model
- Confidence: 0.8 (heuristic)
- Message: cyclic component has no max_iterations guard
```

(exit code: `2`)

## Design references

- ADR-011: Pivot to LangGraph as the primary front
- ADR-009: LSP diff execution + degraded mode (long-lived workers)
- ADR-008: Two-dimensional ConfidenceReason
- ADR-002: Onion + Factory parser extensibility

Implementation files:

- `scripts/export_langgraph_server.py` (Python shim)
- `infrastructure/parser/python_worker.go` (subprocess wrapper)
- `infrastructure/parser/langgraph.go` (`WorkflowParser` implementation)
- `infrastructure/factory/parser.go` (Factory registration `case "langgraph"`)
- `cmd/shingan/analyze.go` (`--format=langgraph` flag + directory walk)

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `Python … not found in PATH` | Python not installed | Install Python 3.10+ |
| `pip install langgraph required` | langgraph not installed | `pip install langgraph` |
| `parse_file …: ModuleNotFoundError: No module named 'foo'` | Target depends on external imports | Analyse in an environment that can run the target (a local venv is recommended) |
| `call "parse_file" timed out after 30s` | Large module / heavy import | Extend the timeout via `WithCallTimeout` (tweak the LSP / CLI configuration) |
| Empty graph from analysis | StateGraph isn't at the module top level | The build-inside-a-function pattern is Phase 2 territory |

## Version compatibility

- `langgraph >= 0.2.0`: tested (refreshed in CI as new versions ship)
- `langgraph < 0.2.0`: unsupported due to API mismatch
- `langgraph >= 1.0` (future): if private attribute names change, the shim's `_nodes` / `_edges` / `_branches` fallbacks should absorb the difference, but actual API breaks need additional work

The LangGraph API is still young, so the shim is written to be **API-tolerant** (heavy use of `getattr`, no `isinstance`). If a version bump breaks the API, updating the shim's `_extract_*` functions is usually all that's needed.
