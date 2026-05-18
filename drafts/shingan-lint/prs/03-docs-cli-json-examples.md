# PR: docs(cli): add JSON workflow input examples

**Title:** `docs(cli): add JSON workflow input examples`

**Branch suggestion:** `docs/json-input-examples`

---

## Summary

`--format json` is the default input format, but the expected manifest structure is not documented. New users have to guess fields like `entry_node_id`, `nodes`, `edges`, edge `from` / `to`.

## Changes

- Add a "JSON workflow manifest" section to the README.
- Add a minimal valid example.
- Add a slightly larger example showing branching and tool nodes.
- Link to it from the `--format` flag documentation.

## README addition (draft)

````markdown
### JSON workflow manifest

`--format json` (default) expects a single JSON file describing the workflow graph.

Minimal example:

```json
{
  "entry_node_id": "start",
  "nodes": [
    { "id": "start",  "type": "llm",  "name": "Planner" },
    { "id": "search", "type": "tool", "name": "Search"  }
  ],
  "edges": [
    { "from": "start", "to": "search" }
  ]
}
```

Branching example:

```json
{
  "entry_node_id": "router",
  "nodes": [
    { "id": "router",  "type": "llm",   "name": "Router" },
    { "id": "search",  "type": "tool",  "name": "Search" },
    { "id": "answer",  "type": "llm",   "name": "Answer" },
    { "id": "summary", "type": "llm",   "name": "Summary" }
  ],
  "edges": [
    { "from": "router", "to": "search" },
    { "from": "router", "to": "answer" },
    { "from": "search", "to": "summary" },
    { "from": "answer", "to": "summary" }
  ]
}
```

Then run:

```bash
shingan analyze --format json --input workflow.json
```
````

## Test Plan

- [x] Examples parse successfully with the current CLI.
- [x] No code changes.
