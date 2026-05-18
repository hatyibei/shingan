# PR: feat(langgraph): static discovery without importing the app

**Title:** `feat(langgraph): support static discovery without importing app dependencies`

**Branch suggestion:** `feat/langgraph-static`

**Recommended:** Open Issue first; this is a larger change with design tradeoffs.

---

## Summary

Today's `--format langgraph` path requires importing the target project, which means:

- All transitive dependencies must be installed.
- Side effects at import time run on the linter's host.
- Linting third-party code in CI (e.g. a dependency audit) is awkward.

Propose a `--static` mode that walks the source AST to extract `StateGraph` / `add_node` / `add_edge` calls without executing user code.

## Usage

```bash
shingan analyze --format langgraph --input . --static
```

## Tradeoffs

| Aspect | Import mode (today) | Static mode (proposed) |
|--------|---------------------|------------------------|
| Accuracy | High (real graph) | Lower (heuristic; misses dynamic graphs) |
| Setup cost | High (deps required) | Low |
| Side-effect risk | Yes | None |
| CI friendliness | Medium | High |

Static mode is best-effort. When it cannot resolve a graph, it should emit a clear "static analysis incomplete" diagnostic instead of crashing.

## Suggested scope

1. Land a minimal AST walker that recognizes the most common LangGraph patterns.
2. Add a feature matrix in the docs listing what static mode supports vs. doesn't.
3. Leave import mode as the default.

## Test Plan

- [x] A representative set of LangGraph projects produces graphs comparable to import mode.
- [x] Source files that perform side-effects at import are not executed in static mode.
- [x] Import mode behavior is unchanged when `--static` is absent.
