# PR: docs(positioning): clarify shingan as pre-deployment static analysis for AI workflow graphs

**Title:** `docs(positioning): clarify shingan as pre-deployment static analysis for AI workflow graphs`

**Branch suggestion:** `docs/positioning-tagline`

---

## Summary

Tighten the top-of-README positioning so first-time visitors understand what `shingan` is, what category it sits in, and how it differs from observability / eval / guardrails tools.

## Motivation

`shingan` occupies a specific slot — **pre-deployment static analysis for AI workflow graphs** — that is distinct from:

- Observability (LangSmith, Phoenix, OpenTelemetry GenAI) → tells you what happened.
- Eval / scoring (LangSmith eval, Promptfoo, DeepEval) → tells you how a run scored.
- Runtime guardrails (Guardrails AI, NeMo Guardrails, prompt firewalls) → blocks at execution.

`shingan` sits before all of these: read the workflow definition, fail the CI build if the graph contains unsafe structure, before anything runs.

Making this slot explicit helps:

- New users decide in 10 seconds whether to keep reading.
- CI / Platform Engineering teams find it via the keywords they actually search.
- Differentiates from observability-first tools that may add lint-style features later.

## Proposed README header

````markdown
# shingan

**Pre-deployment static analysis for AI workflow graphs.**

Catch cycles, leaks, and unsafe tool paths in your agent workflows before they run.
CI-friendly. Framework-neutral. SARIF-compatible.

Supported inputs: JSON workflow manifest, LangGraph, CrewAI, ADK-Go.

```bash
npx --yes --package shingan-lint shingan analyze --format json --input workflows
```

## Why shingan

| Layer | Tools | What they answer |
|-------|-------|------------------|
| Pre-deployment static analysis | **shingan** | Should this workflow ever ship? |
| Runtime guardrails | Guardrails AI, NeMo Guardrails | Should this specific call be blocked? |
| Observability | LangSmith, Phoenix, OTel GenAI | What did the agent actually do? |
| Eval | Promptfoo, DeepEval, LangSmith eval | How well did it score? |

Observability tells you what happened. Shingan tells you what should never be deployed.
````

## Optional follow-ups (not in this PR)

- A short "Rules at a glance" table linking to per-rule docs (see PR #24).
- A diagram showing where shingan sits in a typical CI pipeline.
- Multi-language tagline below the H1 for the Japanese README:
  > AIエージェントワークフローの静的解析。実行前に、無限ループ・情報漏えい・危険なツール経路を検出する。

## Open questions for maintainer

- "agentic" was deliberately avoided in the tagline because it's likely to date quickly. Open to using it if you prefer.
- The comparison table names other tools; happy to drop names if you'd rather not pull other projects into the README.

## Test Plan

- [x] Docs-only change.
- [x] All claims in the tagline match what the CLI actually does today.
- [x] No code changes.
