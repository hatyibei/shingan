> 🌐 Language: **English** | [日本語](./crewai.ja.md)

# CrewAI Support

> Phase 1 / second Python target — a Shingan parser for [CrewAI](https://github.com/crewAIInc/crewAI) Crew/Agent/Task definitions, reusing the LangGraph PythonWorker infrastructure (ADR-013).

## Overview

Shingan can statically analyse CrewAI workflows from a `.py` file that defines a `crewai.Crew` instance at module scope. Like LangGraph, the parser uses a long-lived Python subprocess + JSON-RPC bridge, so importing CrewAI itself only happens once per analysis run.

```
┌──────────────────────┐  newline-JSON RPC  ┌──────────────────────────┐
│ shingan (Go process) │ ◄─────────────────►│ scripts/export_crewai_…  │
│   CrewAIParser       │                    │ (Python long-lived       │
│   PythonWorker       │                    │  worker)                 │
└──────────────────────┘                    └──────────────────────────┘
```

The Go-side `PythonWorker` is the *exact same* implementation that powers `--format=langgraph`; ADR-013 made it framework-agnostic so each shim script lives independently in `scripts/`.

## Installation

CrewAI requires Python 3.10+ and Pydantic v2:

```bash
python3 -m pip install -r scripts/requirements-shim.txt
# or, at minimum:
python3 -m pip install "crewai>=0.50.0"
```

Even in environments without Python or CrewAI, building shingan and analysing other formats still works. The availability check only runs when you specify `--format=crewai`, and when it fails the command stops with a clear error message:

```text
create crewai parser: crewai parser: Python 3.x and `pip install crewai` (>=0.50.0) required for CrewAI format
```

## Usage

A single file:

```bash
shingan analyze --format crewai --input crew.py --output markdown
```

A directory (recursively scans every `.py` file and parses each independently per ADR-012):

```bash
shingan analyze --format crewai --input ./crews/ --output sarif --output-file findings.sarif
```

CI invocation with progressive baseline:

```bash
shingan analyze \
  --format crewai \
  --input ./crews \
  --baseline .shingan/baseline.json \
  --since main
```

## NodeType mapping (ADR-013)

| CrewAI concept | Shingan NodeType | Confidence | ConfidenceReason |
|---|---|---|---|
| `Agent(role=, goal=, backstory=, tools=[…])` | LLM | 1.0 | `exact_static_match` |
| `Task(description=, expected_output=, agent=A)` | Tool | 1.0 | `exact_static_match` |
| `Tool` (`@tool` / `BaseTool` subclass) | Tool (`Config["category"]` from heuristic) | 0.8 | `name_heuristic` |
| `Crew(process=Process.sequential)` | Tasks chained head-to-tail (Task[i] → Task[i+1]) | 1.0 | `exact_static_match` |
| `Crew(process=Process.hierarchical, manager_llm=)` | manager → every worker → manager (over-approximation) | 0.7 | `over_approximated_dynamic` |
| `Agent.tools[t]` | Edge `Agent → Tool` (unconditional; Edge.Condition is reserved for true control-flow conditions) | 1.0 | `exact_static_match` |
| `Task.agent = A` | Edge `Task → Agent` (unconditional; mental model: Task pulls in Agent during execution) | 1.0 | `exact_static_match` |
| `Agent(allow_delegation=True)` × ≥2 agents | Bidirectional delegate edges between every delegating pair | 0.6 | `over_approximated_dynamic` |

### Tool category heuristic

The shim uses substring matching on the tool's name + class to populate `Config["category"]`:

| Pattern in name / class | `Config["category"]` |
|---|---|
| `eval`, `exec`, `code_runner`, `code_interpreter`, `python_repl`, `shell`, `bash`, `subprocess` | `code_execution` |
| `http`, `api`, `request`, `fetch`, `rest` | `api` |
| `search`, `browser`, `scrape`, `web` | `tool` |
| (anything else) | `tool` (default) |

The classification is consumed by rules like `eval_missing` (which fires on `LLM → code_execution Tool` paths reachable through any Agent → Tool edge) and `unbounded_tool_arg` (which inspects the Pydantic schema embedded in `Config["args_schema"]`).

## Edge mapping

CrewAI's two `Process` modes are translated as follows:

### `Process.sequential`

```
entry = Task[0]
Task[0] ──seq──► Task[1] ──seq──► Task[2]
   │                │                │
   │ uses_agent     │ uses_agent     │ uses_agent
   ▼                ▼                ▼
 Agent[0]         Agent[1]         Agent[2]
   │                │                │
   ▼ uses_tool      ▼ uses_tool      ▼ uses_tool
 Tool[…]          Tool[…]          Tool[…]
```

All Tasks reach all Agents and all Tools transitively, so reachability rules (`unreachable_node`, `cycle_detection`) operate over the full graph.

### `Process.hierarchical`

```
entry = manager (synthetic LLM, modelled after `manager_llm` or `manager_agent`)
manager ──delegate──► Worker[k]   (Condition="delegate" — runtime LLM dispatch)
manager ─────────────► Task[i]    (manager dispatches each Task)
Task[i] ─────────────► assigned Agent
```

The manager → worker edges are over-approximated (the runtime LLM decides which worker to invoke, so we list every candidate), giving these edges Confidence 0.7 / Reason `over_approximated_dynamic`. The `worker → manager` "report" back-edge is **not** materialised — modelling the result-return path as a graph edge created false 2-node cycles that fired `cycle_detection` Critical on otherwise valid hierarchical workflows.

## Confidence and ConfidenceReason

CrewAI is statically declared *except* for hierarchical manager dispatch. The parser surfaces both regimes:

| Edge / node kind | Confidence | ConfidenceReason |
|---|---|---|
| `Task[i] → Task[i+1]` (sequential) | 1.0 | `exact_static_match` |
| `Task → Agent` (`uses_agent`) | 1.0 | `exact_static_match` |
| `Agent → Tool` (`uses_tool`) | 1.0 | `exact_static_match` |
| Tool category inferred from name / class | 0.8 | `name_heuristic` |
| `manager → worker` (hierarchical) | 0.7 | `over_approximated_dynamic` |
| Bidirectional delegate edges (≥2 delegating agents) | 0.6 | `over_approximated_dynamic` |

Findings produced under over-approximated edges should be filtered with `--min-confidence=0.7` if you want to suppress hierarchical-mode noise without losing the static-mode signal.

## Samples

Five reference samples live under `testdata/crewai/`:

| File | Pattern | Findings observed (crewai 1.14.4) |
|---|---|---|
| `simple_crew.py` | 1 Agent + 1 Task, `Process.sequential` | 1 Warning (`error_handler_checker` on the Task — no error-handling branch) |
| `sequential_pipeline.py` | 3 Agents + 3 Tasks, `Process.sequential` | 3 Warning (`error_handler_checker` on each Task in the chain) |
| `hierarchical.py` | 2 Agents + `manager_llm=LLM(model="gpt-4o-mini")`, `Process.hierarchical` | 2 Warning (`error_handler_checker` on each Task — no false `cycle_detection` since v0.8 dropped the `worker → manager` back-edge) |
| `multi_tool.py` | 1 Agent + 3 tools (web search / HTTP / `python_repl`) | 1 Critical (`eval_missing` on Agent → `python_repl` `code_execution` sink) + 2 Warning (`error_handler_checker` on the Task and on the tool-using Agent) + 1 Info (`pii_leak_scanner` 30% on the path Task → `http_api_request` external API) |
| `circular_delegation.py` | 2 Agents both with `allow_delegation=True` | 1 Critical (`cycle_detection` 100% on alpha — the bidirectional delegate cycle is real) + 3 Warning (`circular_dep_agents` 85% on alpha↔beta pair + `error_handler_checker` on each of the 2 Tasks) |

Run them with:

```bash
shingan analyze --format crewai --input testdata/crewai/multi_tool.py --output markdown
```

> **Note**: findings above are measured against `crewai==1.14.4`. Re-run after upgrading CrewAI versions and report drifts via the issue tracker.

## Example output (`multi_tool.py`)

```bash
$ shingan analyze --format crewai --input testdata/crewai/multi_tool.py --output markdown
# Shingan Analysis Report

## Summary

| Total | Critical | Warning | Info |
|-------|----------|---------|------|
| 4     | 1        | 2       | 1    |

## Critical

| Rule         | Node                       | Confidence | Message                                                                                                                                            |
|--------------|----------------------------|------------|----------------------------------------------------------------------------------------------------------------------------------------------------|
| eval_missing | crew::tool::python_repl    | 90%        | LLM node "crew::agent::multi_tool_assistant" reaches code-execution tool "crew::tool::python_repl" (no validation); LLM output flows into a code runner without sanitisation |

## Warning

| Rule                  | Node                                       | Confidence | Message                                                                                                          |
|-----------------------|--------------------------------------------|------------|------------------------------------------------------------------------------------------------------------------|
| error_handler_checker | crew::task::Answer_the_users_question-0    | 80%        | Tool node has no conditional outgoing edges: error handling is missing                                           |
| error_handler_checker | crew::agent::multi_tool_assistant          | 80%        | LLM node uses tool(s) but has no conditional outgoing edges: error handling for tool failures is missing         |

## Info

| Rule              | Node                          | Confidence | Message                                                                                                                                                                  |
|-------------------|-------------------------------|------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| pii_leak_scanner  | crew::tool::http_api_request  | 30%        | potential PII leak: path from RAG/PII node "crew::task::Answer_the_users_question-0" to external tool "crew::tool::http_api_request" (category="api") without Human gate |
```

## Design references

- ADR-013: CrewAI parser strategy — PythonWorker reuse
- ADR-009: long-lived worker + degraded mode
- ADR-008: ConfidenceReason two-axis quality model
- ADR-002: Onion + Factory parser extensibility

Implementation files:

- `scripts/export_crewai_server.py` (Python shim)
- `infrastructure/parser/python_worker.go` (subprocess wrapper, shared with LangGraph)
- `infrastructure/parser/crewai.go` (`WorkflowParser` implementation)
- `infrastructure/factory/parser.go` (Factory registration `case "crewai"`)
- `cmd/shingan/analyze.go` (`--format=crewai` flag + directory walk)
- `domain/testutil/generate.go` (`GenerateCrewAIGraph` for property tests)
- `cmd/shingan-gen/main.go` (`--pattern=crewai-simple` for sample generation)

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `Python … not found in PATH` | Python not installed | Install Python 3.10+ |
| `pip install crewai (>=0.50.0) required` | crewai not installed or below v0.50 | `pip install "crewai>=0.50.0"` |
| `parse_file …: ModuleNotFoundError: No module named 'crewai_tools'` | Custom Tool subclass imports a sibling module | Analyse in an environment that can run the target (a local venv is recommended) |
| Empty graph from analysis | `Crew` instance is built inside a function rather than at module scope | Move the `Crew(…)` call to module top-level (sub-crew / lazy crew construction is Phase 2 territory) |
| Bidirectional delegate edges look wrong | Two or more agents have `allow_delegation=True` | Either turn off delegation on agents that don't need it, or accept the over-approximation (Confidence 0.6) |

## Version compatibility

- `crewai >= 0.50.0`: tested via the parser shim (refreshed in CI as new versions ship)
- `crewai < 0.50.0`: unsupported (Pydantic v1 attribute accessors differ enough to make the shim brittle)
- `crewai >= 1.0` (future): if private attribute names change, the shim's `getattr`-everywhere fallbacks should absorb the difference, but actual API breaks need additional work
