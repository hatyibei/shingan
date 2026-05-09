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

## Findings sweep (Shingan v0.8.2)

| File | Findings | Why |
|---|---|---|
| `multi_agents/agents/orchestrator.py` | 0 | StateGraph constructed inside `ChiefEditorAgent._create_workflow(self, agents)` — instance method with required `agents` parameter; out of v0.8.x parser scope |
| `multi_agents/agents/editor.py` | 0 | Same pattern |
| `multi_agents/main.py` | 0 | Top-level wiring without StateGraph |

> **0 findings here is itself the take-away.** gpt-researcher's workflow is OOP-structured: the StateGraph lives inside an instance method that takes runtime arguments. Shingan v0.8.x can detect zero-arg factory functions and `@CrewBase` classes but not arbitrary instance methods with required parameters. v0.9's AST-based fallback parser is on track to address this — it walks the syntax tree for `StateGraph(...).add_node(...)` patterns regardless of containing function/method signature.

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

gpt-researcher is exactly the kind of OSS Shingan must analyse to justify its existence — a multi-agent LangGraph workflow with 7+ specialised agents, used by tens of thousands of developers, with real production deployments. v0.8.x can't extract its graph yet, but the dogfood pass revealed and fixed three layout bugs that affect **every** package-style LangGraph user, not just gpt-researcher.

The v0.9 AST fallback parser is the unlock for getting findings here. We'll re-run this case study at v0.9 release.

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
