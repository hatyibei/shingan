> 🌐 Language: **English** | [日本語](./n8n.ja.md)

# n8n Support

> Phase 1 / JSON-DSL track — a Shingan parser for [n8n](https://n8n.io) workflow exports.

## Overview

Shingan can statically analyse n8n workflows from a `.json` workflow export — the file you get from **Workflow → Download** in the n8n editor, or from `n8n export:workflow --id=<n>` on the CLI. Unlike LangGraph (Python) or ADK-Go, n8n authors workflows in a JSON DSL, so no runtime is required to extract the graph.

The parser is **pure Go**: no Python shim, no Node bridge. That keeps n8n the cheapest framework to integrate against in CI.

```
┌──────────────────────┐    json.Unmarshal    ┌──────────────────────────┐
│ shingan (Go process) │ ──────────────────► │  workflow.json           │
│   N8nParser          │                      │  (n8n export)            │
└──────────────────────┘                      └──────────────────────────┘
```

## Installation

No additional dependencies — n8n analysis works out of the box once `shingan` is installed:

```bash
shingan analyze --format n8n --input workflow.json
```

## Usage

A single file:

```bash
shingan analyze --format n8n --input workflow.json --output markdown
```

Directory analysis is **not** supported for n8n in v0.7 (each export is one workflow per file). Pass each export individually, or use a shell loop:

```bash
for f in n8n-exports/*.json; do
  shingan analyze --format n8n --input "$f" --output sarif --output-file "${f%.json}.sarif"
done
```

CI invocation with progressive baseline:

```bash
shingan analyze \
  --format n8n \
  --input n8n-exports/customer_support.json \
  --baseline .shingan/baseline.json \
  --since main
```

## NodeType mapping

n8n's type strings (e.g. `n8n-nodes-base.openAi`, `n8n-nodes-base.if`) are mapped to Shingan's canonical NodeType enum so framework-agnostic rules can fire unchanged.

| n8n type substring | Shingan NodeType | `Config["category"]` |
|---|---|---|
| `openai`, `chatgpt`, `anthropic`, `claude`, `gemini`, `vertex`, `bedrock`, `ollama`, `mistral`, `cohere`, `huggingface` | LLM | — |
| `n8n-nodes-langchain` | LLM | — |
| `ai-agent`, `ai_agent`, `.agent`, `agent.`, `/agent` | LLM | — |
| `llm` (catch-all) | LLM | — |
| `.code`, `executecommand`, `function`, `pythonfunction` | Tool | `code_execution` |
| `.if`, `.switch`, `filter`, `router` | Condition | — |
| `webhook`, `trigger` | Tool | `trigger` |
| `httprequest`, `http`, `.api`, `rest` | Tool | `api` |
| (anything else) | Tool | `api` (default) |

The matcher is **case-insensitive and substring-based**, so future n8n type names (`OpenAi2`, `chatGptAdvanced`) keep working without code changes.

## Edge mapping

n8n's `connections.<source>.main` is a **2-D array**:

- Outer index = output port (0 = pass / true; 1 = fail / false on `if` nodes)
- Inner index = parallel destinations from that port

For Shingan edges:

| Source NodeType | Port 0 → Edge.Condition | Port 1 → Edge.Condition | Port n>1 → Edge.Condition |
|---|---|---|---|
| Condition (`if`, `switch`) | `"true"` | `"false"` | `"branch_<n>"` |
| Anything else | `""` (unconditional) | `"branch_1"` | `"branch_<n>"` |

This lets `cycle_detection`, `unreachable_node`, and `error_handler_checker` reason about the workflow exactly as they would for an ADK-Go `LoopAgent` or a LangGraph `add_conditional_edges`.

## Entry-node detection

n8n exports do not declare an entry node. Shingan picks one in this order:

1. The first node whose `Config["category"] == "trigger"` (a `webhook`/`*Trigger`/`manualTrigger` node).
2. The first node with no incoming `main` edges.
3. The first node in the array.

## Disabled nodes

n8n nodes flagged `"disabled": true` are silently skipped, along with any edge that touches them. This mirrors n8n's runtime behaviour (a disabled node is not executed) and avoids reporting findings on dead code.

## Supported features

| Feature | Support | Notes |
|---|---|---|
| `nodes[]` array | OK | Parameters carried through as `Config` |
| `connections.<source>.main` | OK | Multi-port + parallel destinations both honoured |
| `connections.<source>.ai_*` (langchain sub-tools, memory, output parsers) | Skipped | Treated as decoration; no edges emitted |
| `disabled: true` | OK | Node and incident edges dropped |
| Triggers: `webhook`, `*Trigger`, `manualTrigger` | OK | Promoted to entry node |
| `if` / `switch` condition nodes | OK | Branch labels `true` / `false` / `branch_<n>` |
| Sub-workflows (`executeWorkflow`) | Out of scope (v0.7) | Resolved as a regular Tool; the called workflow is not inlined |
| Pinned data (`pinData`) | Ignored | Static analysis only inspects structure, not test data |

## Confidence and ConfidenceReason

n8n is fully static — there is no dynamic graph construction analogous to LangGraph's `conditional_edges` callback — so the parser emits high-confidence findings:

| Edge / node kind | Confidence | ConfidenceReason |
|---|---|---|
| `connections.<source>.main` edge | 1.0 | `exact_static_match` |
| NodeType inferred from substring match | 0.8 | `name_heuristic` |
| Default Tool fallback (unknown type) | 0.6 | `name_heuristic` |

## Samples

Five reference samples live under `testdata/n8n/`:

| File | Pattern | Findings observed |
|---|---|---|
| `simple_chain.json` | Webhook → ChatGPT → HTTP Request | 2 Warning — `error_handler_checker` on Webhook (trigger) + ChatGPT (LLM with downstream tool) |
| `branching.json` | Webhook → ChatGPT → IF → (Slack / Email) | 1 Warning — `error_handler_checker` on Webhook (the IF branches each terminate cleanly) |
| `loop.json` | Schedule → Fetch → Split In Batches → Process Item ↺ | 2 Critical (`cycle_detection` on Split In Batches; `retry_storm` on Process Item with `retries=5 × parallelism=20`) + 4 Warning (`error_handler_checker` on every linear node) |
| `multi_step.json` | Webhook → Vector Search → Embed → Generator → Output | 4 Warning — `error_handler_checker` on the trigger, the API tool, and both LLM stages |
| `ai_agent.json` | langchain `aiAgent` reaching a code-execution tool | 1 Critical (`eval_missing`: Agent → Code Tool path) + 2 Warning (`error_handler_checker` on Webhook and Agent); langchain sub-tool wires (`ai_languageModel`, `ai_memory`, `ai_tool`) skipped as decoration |

Run them with:

```bash
shingan analyze --format n8n --input testdata/n8n/simple_chain.json --output markdown
```

## Example output (`simple_chain.json`)

```bash
$ shingan analyze --format n8n --input testdata/n8n/simple_chain.json --output markdown
# Shingan Analysis Report

## Summary

| Total | Critical | Warning | Info |
|-------|----------|---------|------|
| 2     | 0        | 2       | 0    |

## Warning

| Rule                  | Node    | Confidence | Message                                                                                            |
|-----------------------|---------|------------|----------------------------------------------------------------------------------------------------|
| error_handler_checker | Webhook | 80%        | Tool node "Webhook" (category="trigger") has no conditional outgoing edges: error handling is missing |
| error_handler_checker | ChatGPT | 80%        | LLM node "ChatGPT" uses tool(s) but has no conditional outgoing edges: error handling for tool failures is missing |
```

(exit code: `2` — Warnings only, no Critical)

## Design references

- ADR-002: Onion + Factory parser extensibility
- ADR-003: WorkflowGraph IR (canonical NodeType enum)
- ADR-008: Two-dimensional ConfidenceReason

Implementation files:

- `infrastructure/parser/n8n.go` — `WorkflowParser` implementation (pure Go)
- `infrastructure/parser/n8n_test.go` — table-driven tests + edge cases
- `infrastructure/factory/parser.go` — Factory registration `case "n8n"`
- `cmd/shingan/analyze.go` — `--format=n8n` flag
- `domain/testutil/generate.go` — `GenerateN8nGraph` for property tests
- `cmd/shingan-gen/main.go` — `--pattern=n8n-simple` for sample generation

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `n8n parser: unmarshal: …` | File is not valid n8n export JSON | Re-export from the n8n editor (Workflow → Download) |
| Empty graph | All nodes are `disabled: true` | Re-enable at least one node, or remove `--input` filter |
| Edges missing on `if` node | Reading `connections.<src>.ai_*` instead of `.main` | Verify the JSON has `main` edges (langchain-only nodes are decoration) |

## Version compatibility

- n8n exports from **v1.x** (current LTS): tested
- n8n exports from **v0.x** (legacy): the schema is similar; substring-based NodeType matching keeps working, but the older `connections.<src>` shape is not exercised by the test fixtures
- Sub-workflow inlining (`executeWorkflow`): tracked as a Phase 2 follow-up
