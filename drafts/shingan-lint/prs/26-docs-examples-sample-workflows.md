# PR: docs(examples): add sample workflows directory

**Title:** `docs(examples): add sample workflows for each supported format`

**Branch suggestion:** `docs/examples-sample-workflows`

---

## Summary

Ship small runnable examples in the repo so users can try `shingan` without inventing a manifest.

## Proposed layout

```
examples/
  json/
    minimal.json
    cycle.json
    missing-error-handler.json
  langgraph/
    minimal/
      pyproject.toml
      src/app.py
  crewai/
    minimal/
      ...
  adk-go/
    minimal/
      ...
```

Each subtree includes a `README.md` with the exact `shingan analyze` command and the expected findings.

## CI integration

Add a `make examples` target (or scripted equivalent) that runs `shingan analyze` against every example and asserts the expected findings. This doubles as smoke coverage.

## Test Plan

- [x] Every example produces the documented findings.
- [x] Examples that should be clean produce zero findings.
- [x] CI runs the examples suite.
