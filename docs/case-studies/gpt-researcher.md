> 🌐 Language: **English** (Japanese translation welcome — see [issue tracker](https://github.com/hatyibei/shingan/issues))

# Case study: `assafelovic/gpt-researcher`

## Repo

[github.com/assafelovic/gpt-researcher](https://github.com/assafelovic/gpt-researcher) — a popular Python LangGraph-based research agent (~16k stars). Multi-agent layout: `multi_agents/agents/{researcher,writer,publisher,reviser,reviewer,editor,human,orchestrator}.py`, with `ChiefEditorAgent._create_workflow(self, agents)` building the LangGraph at runtime.

## Setup

```bash
npm install -g shingan-lint@latest
python3 -m pip install langgraph beautifulsoup4 markdown gpt-researcher
git clone https://github.com/assafelovic/gpt-researcher /tmp/gpt-researcher
```

## Findings sweep (Shingan v0.8.5 — bounded-cycle aware)

> **Update (2026-05-06):** an earlier draft of this case study described
> the `planner ↔ human` cycle as a Critical finding. Since v0.8.5,
> `cycle_detection` distinguishes "structural cycle without exit" from
> "bounded cycle with conditional exit" and downgrades the latter to
> Warning. The gpt-researcher cycle has an `accept` branch that exits
> to `researcher`, so it falls into the bounded category — Warning is
> the more honest classification. The remediation recommendation below
> still holds; only the severity label changed.

| File | Findings | Severity | Notable |
|---|---|---|---|
| `multi_agents/agents/orchestrator.py` | **7** | 4 Warning + 3 Info | `cycle_detection` Warning on `planner` (bounded cycle, exit via `researcher`) |
| `multi_agents/agents/editor.py` | (parses, near-clean) | — | inner sub-graph follows the same instance-method pattern |
| `multi_agents/main.py` | 0 | — | Top-level wiring without StateGraph |

### The bounded cycle worth a maintainer conversation

```
[warning] cycle_detection (90%) on planner
  bounded cycle through non-Loop node "planner" (type=tool):
  the cycle has an exit branch, but no explicit max_iterations /
  recursion_limit guard
[warning] error_handler_checker (80%) on browser / planner / researcher
```

The cycle is the **human-in-the-loop revision flow**:

```python
workflow.add_conditional_edges(
    'human',
    lambda review: "accept" if review['human_feedback'] is None else "revise",
    {"accept": "researcher", "revise": "planner"},
)
```

`human → revise → planner → human → revise → planner → …` is bounded
only by the human eventually clicking "accept" or by langgraph's
default `recursion_limit=25`. The exit branch (`accept → researcher`)
is what kept this from being a structural deadlock — but the silent
bound is still risky if:

- the human reviewer is automated (e.g. another LLM acting as a reviewer),
- the human is malicious / accidentally rejecting indefinitely, or
- the test suite keeps feeding "revise" with no termination check.

**Recommendation we'd surface as a GitHub Issue (drafted, not yet filed):**

> Title: `cycle_detection: planner ↔ human revision loop has no explicit max_revisions`
>
> Body: Shingan static analysis surfaces a bounded cycle on the
> `human → revise → planner` path of `ChiefEditorAgent._create_workflow`.
> The cycle has an `accept` exit branch but is otherwise bounded only
> by the user clicking "accept" — automated reviewers or runaway
> revision requests will silently hit langgraph's default
> `recursion_limit=25` rather than producing a clear "max revisions
> exceeded" error. Consider:
> 1. Adding a `revisions_count` field to ResearchState and short-circuiting `human` to `researcher` after N revisions, or
> 2. Setting an explicit `max_revisions` config + raising a typed exception when exceeded, or
> 3. Documenting the recursion_limit dependency for users wiring automated reviewers.
> Not a security bug; a UX / robustness improvement. Shingan analysis trace: …

(Issue body is ready to file when the maintainer relationship is established — per the case-study methodology, we don't open unsolicited PRs against external repos.)

## Bugs in Shingan this case study fixed

The orchestrator file did, however, expose **three real layout-handling bugs** in Shingan's Python shim that we shipped fixes for in v0.8.1 and v0.8.2:

### 1. Package-aware `sys.path`

Initially `import langgraph_supervisor` (the user's own package) raised `ModuleNotFoundError`. The shim only put the file's immediate parent on `sys.path`; for `multi_agents/agents/orchestrator.py` to do `from .utils.views import …` and `from ..memory.research import …`, the package root (first ancestor without `__init__.py`) needed to be on `sys.path` too.

[Commit `66b1337`](https://github.com/hatyibei/shingan/commit/66b1337)

### 2. Dotted module name + parent package registration

Even with `sys.path` fixed, loading the file under a synthetic name like `_shingan_user_<encoded>` failed every relative import (`from . import WriterAgent`) because Python's import resolver couldn't find a parent package context.

Fix: load under the file's REAL dotted name (`multi_agents.agents.orchestrator`) and register stub parent packages in `sys.modules` so relative imports resolve.

[Commit `6871a9e`](https://github.com/hatyibei/shingan/commit/6871a9e)

### 3. Missing-dep error UX

When the user's module imported a third-party library that wasn't installed (gpt-researcher pulls in `bs4`, `markdown`, `unstructured`, `exa_py`, `langchain-tavily`, …), the shim previously surfaced a confusing `import _shingan_user_<encoded> failed` message instead of the real package name.

Fix: when `ModuleNotFoundError.name` is **not** a prefix of the user's own dotted path (i.e. it's a third-party dep gap, not a layout issue), bubble up `Run pip install <name>` directly.

[Commit `c663ef9`](https://github.com/hatyibei/shingan/commit/c663ef9)

## Take

gpt-researcher is exactly the kind of OSS Shingan must analyse to justify its existence — a multi-agent LangGraph workflow with 7+ specialised agents, used by tens of thousands of developers, with real production deployments.

**v0.8.3 ships the AST fallback that unlocks this surface.** The runtime path can't safely call `ChiefEditorAgent._create_workflow(self, agents)` (required positional args, side-effects), so we now also walk the syntax tree for `StateGraph(...).add_node(...).add_edge(...)` patterns regardless of containing function/method. The first **bounded-cycle** finding in real OSS — initially Critical, reclassified to Warning by the v0.8.5 cycle-exit refinement — landed here.

## How to add Shingan to your gpt-researcher fork

While v0.8.x can't yet extract the workflow graph from `_create_workflow`, the parser **does** correctly detect and surface dependency gaps + layout issues, which is itself a useful CI signal. Add to `.github/workflows/shingan.yml`:

```yaml
- name: Static-analyse agent files
  run: |
    npx shingan-lint@latest analyze \
      --format langgraph \
      --input ./multi_agents/agents/ \
      --output markdown \
      || true   # informational only until v0.9 lands AST fallback
```
