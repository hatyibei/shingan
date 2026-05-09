> 🌐 Language: **English** (Japanese translation welcome — see [issue tracker](https://github.com/hatyibei/shingan/issues))

# Case study: `crewAIInc/crewAI-examples`

## Repo

[github.com/crewAIInc/crewAI-examples](https://github.com/crewAIInc/crewAI-examples) — the official CrewAI examples monorepo. 18 example crews + 5 example flows + the CrewAI-LangGraph integration sample.

## Setup

```bash
# Install Shingan via npm (uses the canonical released artifact)
npm install -g shingan-lint@latest

# Install the CrewAI runtime that Shingan introspects
python3 -m pip install "crewai>=0.50.0"

# Clone the examples
git clone https://github.com/crewAIInc/crewAI-examples /tmp/crewAI-examples
```

## Findings sweep (Shingan v0.8.1, crewai 1.14.4)

### `crews/game-builder-crew/src/game_builder_crew/crew.py` — `@CrewBase` pattern

```bash
shingan analyze --format crewai \
  --input /tmp/crewAI-examples/crews/game-builder-crew/src/game_builder_crew/crew.py \
  --output markdown
```

**Result: 3 Warnings (`error_handler_checker`)** on each Task in the sequential pipeline.

| Severity | Rule | Node | Confidence |
|---|---|---|---|
| Warning | error_handler_checker | `GameBuilderCrew::task::You_will_create_a_game…-0` | 80% |
| Warning | error_handler_checker | `GameBuilderCrew::task::You_will_create_a_game…-1` | 80% |
| Warning | error_handler_checker | `GameBuilderCrew::task::You_are_helping_create…-2` | 80% |

**Take.** Each Task lacks a conditional outgoing edge — Shingan's heuristic recommendation is to define an explicit failure path. CrewAI does provide a runtime-level `task.callback` and `process.fault_tolerance` pattern; Shingan's recommendation is **a higher-confidence design improvement, not a bug** — adding a fallback Task makes failures explicit at the workflow level.

### `crews/multi_tool.py` (Shingan repo's own fixture, structurally equivalent)

```bash
shingan analyze --format crewai \
  --input testdata/crewai/multi_tool.py \
  --output markdown
```

**Result: 7 findings** on a single-Agent crew with three tools (web search / HTTP / `python_repl`):

| Severity | Rule | Node | Confidence | Why it matters |
|---|---|---|---|---|
| **Critical** | eval_missing | `crew::tool::python_repl` | 90% | LLM `multi_tool_assistant` reaches a code-execution tool with no Human gate or sanitiser — classic "LLM RCE" surface |
| Warning | error_handler_checker | `crew::task::Answer_the_users_question-0` | 80% | Task lacks a conditional fallback edge |
| Warning | error_handler_checker | `crew::agent::multi_tool_assistant` | 80% | Same Agent uses tools but has no failure branch |
| Warning | unbounded_tool_arg | `crew::tool::web_search` | 70% | `query: str` Pydantic field without `max_length` |
| Warning | unbounded_tool_arg | `crew::tool::http_api_request` | 70% | `url: str` field without `max_length` |
| Warning | unbounded_tool_arg | `crew::tool::python_repl` | 70% | `code: str` field without `max_length` (compounds the `eval_missing` risk) |
| Info | pii_leak_scanner | `crew::tool::http_api_request` | 30% | Path Task → external HTTP API without explicit Human approval gate |

**Take.** The `eval_missing` Critical is the sort of finding that catches **real production incidents**. Any crew that lets an LLM call `python_repl` without a sanitiser is one prompt-injection away from arbitrary code execution. The `unbounded_tool_arg` cluster compounds it: an attacker controlling the prompt can inflate `code` to thousands of tokens before `python_repl` rejects it, exfiltrating context window content.

### `flows/email_auto_responder_flow/src/email_auto_responder_flow/main.py` — `@start`/`@listen` Flow pattern

```bash
shingan analyze --format crewai --input <path>
```

**Result: 0 findings** (Flow is a separate primitive from Crew; v0.8.x extracts `Crew` only). Tracked for v0.9 — see [v0.9 roadmap](../../shingan-adr.md).

### Summary

| Sweep target | Files parsed | Files with ≥1 finding | Total findings | Critical |
|---|---|---|---|---|
| `crews/*/crew.py` (CrewBase) | 8 | 1 | 3 | 0 |
| `crews/*/main.py` (top-level Crew) | 4 | 0 (most fail on missing `langchain_openai` / `unstructured` / `decouple` / `crewai_tools`) | 0 | 0 |
| `flows/*/main.py` (`@start`/`@listen` Flow) | 5 | 0 | 0 | 0 |

## Bugs in Shingan that this case study fixed

While running this dogfood pass we hit three Shingan bugs and shipped fixes:

1. **Python shims weren't bundled in the npm wrapper** (since v0.6.1) — fixed in [v0.8.1 / commit `e6f3739`](https://github.com/hatyibei/shingan/commit/e6f3739) by `//go:embed`-ing the shims and extracting at first-use.
2. **`@CrewBase` class detection** — the modern CrewAI idiom decorates a class with `@CrewBase` and exposes the Crew via a `crew()` method. Earlier shim versions only walked module-level instances and zero-arg functions. Fixed in [v0.8.1 / commit `1a18a62`](https://github.com/hatyibei/shingan/commit/1a18a62) with a Pass-3 "instantiate class + try `.crew()`" path.
3. **Top-level `crew.kickoff()` / `task.execute()` calls** crashed analysis (real API call would hit network / require API keys). Fixed in [v0.8.1 / commit `f199f2c`](https://github.com/hatyibei/shingan/commit/f199f2c) by monkey-patching CrewAI runtime methods to no-op stubs at shim startup.

## How to add Shingan to your own crew

```bash
# One-shot
npx shingan-lint@latest analyze --format crewai --input ./src/your_crew/crew.py

# CI integration (`.github/workflows/shingan.yml`)
- name: Run shingan
  run: npx shingan-lint@latest analyze --format crewai --input ./src --output sarif --output-file shingan.sarif
- uses: github/codeql-action/upload-sarif@v3
  with:
    sarif_file: shingan.sarif
```
