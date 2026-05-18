# PR: docs(rules): add before/after examples for each built-in rule

**Title:** `docs(rules): add before/after examples for each built-in rule`

**Branch suggestion:** `docs/rules-before-after`

---

## Summary

`shingan list-rules` reports ~22 built-in rules. Each rule deserves a page (or section) with:

- one-line summary
- a minimal manifest that triggers the rule
- a minimal manifest that does not

Adoption hinges on users understanding *why* a finding is raised and how to silence it the right way.

## Proposed layout

```
docs/
  rules/
    cycle_detection.md
    missing_error_handler.md
    ...
```

Each file:

````markdown
# cycle_detection

Detects cycles in the workflow graph.

## Bad

```json
{ "entry_node_id": "a", "nodes": [...], "edges": [{ "from": "a", "to": "b" }, { "from": "b", "to": "a" }] }
```

## Good

```json
{ "entry_node_id": "a", "nodes": [...], "edges": [{ "from": "a", "to": "b" }] }
```

## Fix

Break the cycle, or model the loop with explicit iteration nodes.
````

## Scope

This is a big-but-shallow PR. I'd propose:

1. Land the directory + template first (this PR).
2. Fill in 4–6 rules to seed the pattern.
3. Follow-up PRs (or community contributions) fill the rest.

## Test Plan

- [x] Every documented bad example produces the expected finding.
- [x] Every documented good example produces no finding for that rule.
- [x] Doc links from #21 (rule doc URIs) resolve.
