# PR: docs(langgraph): document dependency and environment requirements

**Title:** `docs(langgraph): document Python and import requirements`

**Branch suggestion:** `docs/langgraph-requirements`

---

## Summary

`shingan analyze --format langgraph --input .` currently fails with `langgraph framework not installed/importable` when the target environment does not have LangGraph importable. This is correct behavior, but unexpected for users coming from "lint my source code without running it" CLIs.

## Changes

Add a "LangGraph parsing" subsection to the README.

````markdown
### LangGraph parsing

`--format langgraph` introspects your project by importing it. That means the
environment in which you run `shingan` must satisfy your project's dependencies
(LangGraph, plus anything imported at module load).

Typical usage from a project venv:

```bash
source .venv/bin/activate
pip install -e .
shingan analyze --format langgraph --input .
```

Or with `uv`:

```bash
uv run shingan analyze --format langgraph --input .
```

If you want a static (non-importing) analysis path, see issue #XX.
````

## Test Plan

- [x] No code changes.
- [x] Commands documented run successfully against a sample LangGraph project.
