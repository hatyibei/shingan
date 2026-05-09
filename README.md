> 🌐 Language: **English** | [日本語](./README.ja.md)

# Shingan (心眼)

> **Your agent can spend money, leak data, and call tools before you notice. Shingan catches dangerous workflow structures before runtime.**

![Go version](https://img.shields.io/badge/Go-1.23-00ADD8?logo=go) ![License](https://img.shields.io/badge/License-MIT-green) ![CI](https://github.com/hatyibei/shingan/actions/workflows/ci.yml/badge.svg) [![npm](https://img.shields.io/npm/v/shingan-lint.svg)](https://www.npmjs.com/package/shingan-lint)

> **Status: Beta.** Shingan is under active development; v1.0 is targeted for late 2026 once `baseline` / `ignore` / severity-policy / PR-bot land. **Not yet recommended for production-critical CI gating** — informational use (`continue-on-error: true`) is the recommended integration mode today.

A Go-based static analyzer for AI agent workflows. It catches dangerous structures — infinite loops, unreachable nodes, missing error handlers, runaway cost paths, prompt-injection sinks, PII leak paths, code-execution from LLM output — **before the workflow ever runs**.

## Why Shingan

LLM orchestration is mainstream now, but the "design-time bug" detection layer is missing. Runtime observability (LangSmith / Langfuse) only tells you a thing happened *after* it cost you money or leaked data. n8n-only linters (FlowLint) miss everything else. **Shingan inspects the workflow graph before execution**, across LangGraph, CrewAI, ADK-Go, n8n, and custom JSON DSLs.

AI agents are unforgiving once they fan out: external API calls, browser automation, and code execution all leave irreversible side effects. Catching infinite loops, unreachable nodes, missing error handlers, PII leak paths, and prompt-injection sinks **before deploy** prevents the majority of cost blowups and incidents.

Every workflow framework reduces to the same primitive: a directed graph of nodes and edges. Shingan keeps that intermediate representation (IR) at the center of an Onion Architecture and runs 20+ rules that are framework-agnostic.

## Where Shingan stands today

A static analyzer wins or loses on **operational ergonomics** (how disruptive it is to your CI), not just rule count. Honest current state:

| Operational dimension | Shingan v0.8.3 | What you'd need before flipping CI to fail-on-finding |
|---|:---:|---|
| Multi-framework (LangGraph / CrewAI / n8n / ADK-Go / JSON / Samurai) | ✅ | — |
| AST-based fallback (factory / instance-method / `@CrewBase` / Flow) | ✅ | — |
| GitHub Action + SARIF + Code Scanning integration | ✅ | — |
| MCP + LSP (Cursor / Claude Code / Neovim / VS Code / LangGraph Studio) | ✅ | — |
| Severity × Confidence two-axis model | ✅ | — |
| Diff mode (`--since main`) + `--baseline` JSON | ✅ | — |
| `// shingan:ignore` line / file comments | ⏳ v0.9 | required for low-friction adoption |
| Severity-policy-as-code (per-rule / per-team) | ⏳ v0.9 | required for organisations with mixed risk tolerances |
| PR bot (inline comments on changed nodes) | ⏳ v0.10 | required for "informational → blocking" promotion |
| Org dashboard (cost / PII / cycle metrics over time) | ⏳ v0.10+ | required for AppSec / Platform team adoption |
| Public false-positive rate (measured against ≥100 OSS workflows) | ⏳ v0.9 | required for procurement / vendor evaluation |
| OWASP Agentic Top 10 — full mapping | ⏳ v0.9 | required for SOC 2 / ISO 42001 / enterprise auditors |
| Plugin SDK (community rules) | internal-only | will go public at v1.0 (ADR-010) |

So: today's recommended use is **`continue-on-error: true` informational CI** plus IDE feedback via the LSP. v0.9–v0.10 is closing the operational gap.

## OWASP Agentic AI — Top 10 (2025) coverage

The [OWASP Agentic AI Top 10 (2025)](https://genai.owasp.org/llmrisk/) lists ten failure modes specific to agentic LLM systems. Static analysis can only catch the *structural* class of these — runtime observability tools (LangSmith, Langfuse) cover the rest. Today's coverage:

| OWASP Agentic Top 10 (2025) | Class | Shingan rule(s) | Status |
|---|:---:|---|:---:|
| AAI01 — Memory poisoning | runtime | (out of static scope) | ❌ runtime-only |
| AAI02 — Tool misuse | structural | `eval_missing`, `unbounded_tool_arg`, `secret_in_prompt_template` | ✅ partial |
| AAI03 — Privilege compromise | structural | `circular_dep_agents`, `dynamic_node_construction` | ✅ partial |
| AAI04 — Resource overload | structural | `loop_guard`, `retry_storm`, `cost_estimation`, `redundant_llm_call` | ✅ |
| AAI05 — Cascading hallucination amplification | runtime | (out of static scope) | ❌ runtime-only |
| AAI06 — Intent breaking & goal manipulation | structural | `prompt_injection_sink`, `temperature_misuse` | ✅ partial |
| AAI07 — Misaligned & deceptive behaviors | runtime | (out of static scope, evaluation-only) | ❌ runtime-only |
| AAI08 — Repudiation & untraceability | structural | `error_handler_checker`, `missing_eval_dataset` | ✅ partial |
| AAI09 — Identity spoofing & impersonation | runtime / config | `model_card_mismatch`, `deprecated_model` | 🟡 partial |
| AAI10 — Overwhelming human in the loop | structural | `cycle_detection`, `unreachable_node` | ✅ partial |

Roadmap to full structural coverage (everything but AAI01 / AAI05 / AAI07, which are runtime-class): **v0.9** — see the [v0.9 plan in shingan-adr.md](./shingan-adr.md).

## Architecture

Onion Architecture — dependencies flow inward only.

```
┌─────────────────────────────────────────────┐
│  cmd/          DI wiring + entry points      │
│  ┌───────────────────────────────────────┐  │
│  │  infrastructure/   concrete adapters   │  │
│  │  ┌─────────────────────────────────┐  │  │
│  │  │  application/   use cases        │  │  │
│  │  │  ┌───────────────────────────┐  │  │  │
│  │  │  │  domain/  zero external dep│  │  │  │
│  │  │  └───────────────────────────┘  │  │  │
│  │  └─────────────────────────────────┘  │  │
│  └───────────────────────────────────────┘  │
└─────────────────────────────────────────────┘
```

| Layer | Responsibility | Allowed dependencies |
|---|---|---|
| domain/ | WorkflowGraph, rules, entity definitions | Standard library only |
| application/ | AnalysisOrchestrator, interface definitions | domain/ |
| infrastructure/ | Parsers, reporters, factory implementations | application/, domain/ |
| cmd/ | CLIs, DI wiring | infrastructure/ |

**Three Factory points**

- `AnalyzerFactory` — registers and creates analysis rules (`domain.AnalysisRule`)
- `ParserFactory` — switches parsers by input format (`application.WorkflowParser`)
- `ReporterFactory` — switches reporters by output format (`application.ReportFormatter`)

## Install

### npm (recommended, zero setup)

```bash
# one-shot run
npx shingan-lint analyze --format=langgraph ./agents/

# project-pinned
pnpm add -D shingan-lint
pnpm exec shingan analyze --since main

# global
npm install -g shingan-lint
shingan analyze --input ./testdata/buggy.json
```

[`shingan-lint`](https://www.npmjs.com/package/shingan-lint) is a thin Node wrapper. Its `postinstall` step downloads the platform-specific Go binary from GitHub Releases, verifies the SHA-256 checksum, and caches it under `~/.cache/shingan-lint/v<ver>/`. Linux, macOS, and Windows on amd64 / arm64 are all supported.

### Go install (Go developers)

```bash
go install github.com/hatyibei/shingan/cmd/shingan@latest
```

### Build from source

```bash
git clone https://github.com/hatyibei/shingan.git
cd shingan
go build -o shingan ./cmd/shingan
```

### Docker

```bash
docker pull ghcr.io/hatyibei/shingan:latest
docker run --rm -v "$(pwd)":/work ghcr.io/hatyibei/shingan analyze --input /work/buggy.json
```

## Usage

JSON input:

```bash
shingan analyze --format json --input workflow.json --output markdown
```

ADK-Go input:

```bash
shingan analyze --format adk-go --input ./agents/ --output markdown
```

LangGraph (Python) input:

```bash
# prerequisite: pip install langgraph
shingan analyze --format langgraph --input agent.py --output markdown
shingan analyze --format langgraph --input ./agents/ --output sarif --output-file findings.sarif
```

Exit codes: `0` = info-only or clean, `1` = warnings present, `2` = at least one critical.

CI integration (GitHub Actions):

```yaml
- name: Shingan check
  run: shingan analyze --format adk-go --input ./agents/
```

## Demo on real ADK-Go samples

`examples/real/` ships three samples written against `google.golang.org/adk v1.1.0`. Shingan detects the following findings on each:

| Sample | Rule | Severity | What it catches |
|---|---|---|---|
| examples/real/infinite_loop.go | cycle_detection | Critical | `loopagent.New` without `MaxIterations` — unbounded loop |
| examples/real/unreachable.go | unreachable_node | Warning | `orphan_analyzer` not wired into the orchestrator's `SubAgents` |
| examples/real/missing_handler.go | error_handler_checker | Warning | `planner` calls `browser_search` but has no conditional branch for failure |

Run them:

```bash
shingan analyze --format adk-go --input examples/real/infinite_loop.go --output markdown
# exit code 2 (Critical)

shingan analyze --format adk-go --input examples/real/unreachable.go --output markdown
# exit code 1 (Warning)

shingan analyze --format adk-go --input examples/real/missing_handler.go --output markdown
# exit code 2 (Critical: loop_guard + Warning: error_handler_checker)
```

**Notes on official ADK-Go SDK coverage:**

- Supports the `loopagent.New(loopagent.Config{AgentConfig: agent.Config{SubAgents: ...}})` shape (v1.1.0)
- Detects `LlmAgent` / `SequentialAgent` / `LoopAgent` `New()` constructor patterns via AST
- Resolves tool nodes registered through `functiontool.New(Config{Name: "..."}, handler)` by following `Config.Name` and ident references
- Uses a `go/types` second pass to read `functiontool.New[TArgs, TResults](...)` generic arguments and infer Tool category from the `TArgs` struct fields (v0.2.0+, via `ParseFile`). This is how `missing_handler.go`'s `browser_search` tool is correctly detected.
- `error_handler_checker` also fires when an LLM node carries a Tool edge but has no conditional branch (LLM→Tool pattern in ADK-Go)
- ADK-Go SDK v1.1.0 requires `go 1.25.0`+; reflected in `go.mod`'s minimum version

```bash
# E2E auto-verification under the demo build tag
go test -tags=demo -v -run TestDemo_ .
```

## Rules

| Rule ID | Detects | Max Severity | Confidence |
|---|---|---|---|
| cycle_detection | Cycles among non-Loop nodes; cycles inside `LoopAgent` scope | Critical | 1.0 (deterministic) |
| loop_guard | `LoopAgent` (Loop type) without `MaxIterations` set | Critical | 1.0 (deterministic) |
| unreachable_node | LLM/Tool nodes unreachable from the entry node | Warning | 1.0 (deterministic) |
| error_handler_checker | Missing error handling after external-I/O nodes | Critical | 0.8 (heuristic) |
| cost_estimation | Expensive LLM models inside loops; expensive models on trivial tasks | Warning | 0.7 (price drifts) |
| redundant_llm_call | Duplicate calls with the same `(prompt_template, model)` | Warning | 0.9 (exact match) |
| pii_leak_scanner | Path from RAG/PII source to external sink with no human gate | Warning | 0.6 (RAG) / 0.3 (name hint) |
| secret_exposure_scanner | Hardcoded API keys / secrets in `Node.Config` | Critical | 0.95 (Critical/Warning) / 0.5 (Info) |
| max_parallel_branches | A single node's fan-out (outgoing edge count) exceeds the threshold | Critical | 1.0 (Critical) / 0.9 (Warning) / 0.7 (Info) |
| deprecated_model | Shutdown or soon-to-be-deprecated LLM model names (OpenAI / Anthropic / Google) | Critical | 1.0 (shutdown) / 0.9 (deprecated soon) |
| temperature_misuse | LLM with `temperature > 0` paired with a deterministic task signature | Warning | 0.9 / 0.7 / 0.5 |
| model_card_mismatch | LLM whose declared `model` disagrees with `provider` / `base_url` | Critical | 1.0 (known prefix) / 0.4 (unknown) |
| prompt_injection_sink | user_input reaches an LLM system-prompt template (substitution → Critical / no substitution → Warning / non-system → Info) | Critical | 0.9 / 0.7 / 0.5 |
| eval_missing | LLM output reaches a code-execution tool (no validation → Critical / Condition gate → Warning / Human gate → skip) | Critical | 0.9 / 0.6 |
| dynamic_node_construction | `eval(`/`exec(`/`Function(`/etc. inside `Node.Config` (`body`/`fn`/`handler`/...) | Critical | 0.95 / 0.85 / 0.6 |
| retry_storm | Tool retry × parallelism = blast radius (≥100 → Critical, ≥30 → Warning, ≥10 → Info) | Critical | 0.9 / 0.7 / 0.5 |
| circular_dep_agents | Multi-agent A→B→A delegation cycle | Warning | 0.85 / 0.75 / 0.6 |
| unbounded_tool_arg | Tool argument schema fields without `maxLength` / `maxItems` / `maximum` | Warning | 0.7 / 0.5 / 0.4 |
| secret_in_prompt_template | Hardcoded credentials inside LLM prompt templates | Critical | 0.95 (exact) / 0.7 (JWT) |
| missing_eval_dataset | Production-flagged graph without an `eval_dataset` reference | Warning | 0.7 |

## Supported formats

### Input

| Format | Status | Notes |
|---|---|---|
| langgraph | **Phase 1 primary** (ADR-011) | Extracts Python `langgraph.graph.StateGraph` via long-lived Python subprocess + JSON-RPC. Requires `pip install langgraph` ([details](./docs/langgraph.md)) |
| adk-go | GA / maintained | AST analysis of Google ADK-Go (`google.golang.org/adk`) |
| json | GA | Shingan's native WorkflowGraph JSON |
| samurai | Alpha | Generic JSON-schema adapter for GUI workflow editors (extension example) |
| n8n | **Beta** | n8n workflow JSON export, pure Go (no Python / Node bridge) ([details](./docs/n8n.md)) |
| crewai | **Beta** | CrewAI Crew/Agent/Task definitions via Python long-lived subprocess + JSON-RPC. Requires `pip install "crewai>=0.50.0"` ([details](./docs/crewai.md)) |

### IDE / editor integrations

| Integration | Status | Notes |
|---|---|---|
| CLI (`shingan analyze`) | GA | Core experience, `--since` / `--baseline` supported |
| GitHub Action | GA | `action.yml`, emits SARIF for GitHub Code Scanning |
| MCP server (`shingan-mcp`) | GA | Callable from Claude Desktop / Cursor / LangGraph Studio |
| **LSP server (`shingan-lsp`)** | **Beta** | VS Code / Cursor / Neovim / Helix / Zed / JetBrains. SHA-256 LRU diff cache + degraded mode (ADR-009). See [docs/lsp.md](./docs/lsp.md) |
| VS Code extension (`vscode-shingan`) | Beta | `extensions/vscode-shingan/`, spawns `shingan-lsp` |

### Output

| Format | Content type | Use |
|---|---|---|
| json | application/json | API response, program-to-program |
| markdown | text/markdown | CLI, human-readable reports |
| sarif | application/sarif+json | GitHub Code Scanning integration |

## Roadmap

- **v0.1〜v0.5** (Apr 2026): JSON / ADK-Go / Samurai parsers, Confidence × Severity 2-axis, SARIF / GitHub Action, 9 rules ✓
- **v0.6** (May 2026): ESLint-style visitor + 3-tier split (ADR-006/007), shingan-lsp, shingan-mcp, LangGraph parser, 20 rules, `shingan-lint` npm distribution, tag→release→npm-publish automation ✓
- **v0.7** (May 2026): n8n parser (pure Go, JSON DSL), bilingual EN/JA docs ✓
- **v0.8** (May 2026): CrewAI parser (Python shim, reuses LangGraph PythonWorker), 6 frameworks total ✓
- **v0.9+**: Mastra parser (TypeScript bridge), 30+ rules, Plugin SDK preview, official site + demo video
- **v1.0**: 5+ frameworks × 25+ rules, Plugin SDK GA, Marketplace listing

## Development

```bash
go test ./...
go vet ./...
go build -o shingan ./cmd/shingan
make lint        # check_confidence_reason + go vet
```

When adding a new rule, see [docs/rule-authoring.md](./docs/rule-authoring.md).

## Documentation

- [Architecture](./docs/architecture.md)
- [Rule authoring guide (internal)](./docs/rule-authoring.md)
- **Framework parsers**: [LangGraph](./docs/langgraph.md) · [CrewAI](./docs/crewai.md) · [n8n](./docs/n8n.md)
- **Case studies (real OSS dogfood)**: [crewAI-examples](./docs/case-studies/crewAI-examples.md) · [n8n community workflows](./docs/case-studies/n8n-community-workflows.md) · [gpt-researcher](./docs/case-studies/gpt-researcher.md) — [index](./docs/case-studies/README.md)
- [LSP server (`shingan-lsp`) — VS Code / Neovim / Helix / Zed setup](./docs/lsp.md)
- [MCP server (`shingan-mcp`) — Claude Desktop / Cursor / LangGraph Studio setup](./docs/mcp-server.md)
- [SARIF output + GitHub Code Scanning integration](./docs/sarif-output.md)
- [diff mode + baseline (`--since` / `--baseline`)](./docs/diff-mode.md)
- [Confidence scoring](./docs/confidence-scoring.md)
- [cycle_detection technical note](./docs/cycle-detection-note.md)
- [All ADRs (001〜013)](./shingan-adr.md)

### Contributing → New rules

Internal contributors implementing new builtin rules should start with **[docs/rule-authoring.md](./docs/rule-authoring.md)**. It covers the Local / Path / Global three-tier templates (ADR-007), ConfidenceReason selection guide (ADR-008), the `check_confidence_reason.sh` linter, TDD patterns, and design notes for every existing rule. Per ADR-010, the Plugin SDK stays internal-only until v1.0 — external contributors should participate via fork → upstream PR.

## License

MIT
