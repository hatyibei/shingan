---
name: add-security-rule-support-for-langgraph-js
description: Workflow command scaffold for add-security-rule-support-for-langgraph-js in shingan.
allowed_tools: ["Bash", "Read", "Write", "Grep", "Glob"]
---

# /add-security-rule-support-for-langgraph-js

Use this workflow when working on **add-security-rule-support-for-langgraph-js** in `shingan`.

## Goal

Implements extraction logic for a new security rule in the langgraph-js parser shim, adds corresponding test fixtures (safe and vuln), and updates Go tests to verify rule firing and regression.

## Common Files

- `infrastructure/parser/shims/export_langgraphjs_server.mjs`
- `testdata/langgraphjs/*_safe.ts`
- `testdata/langgraphjs/*_vuln.ts`
- `infrastructure/parser/langgraphjs_test.go`

## Suggested Sequence

1. Understand the current state and failure mode before editing.
2. Make the smallest coherent change that satisfies the workflow goal.
3. Run the most relevant verification for touched files.
4. Summarize what changed and what still needs review.

## Typical Commit Signals

- Update infrastructure/parser/shims/export_langgraphjs_server.mjs to extract new config or schema for the rule.
- Add or update test fixtures in testdata/langgraphjs/ (e.g., *_safe.ts and *_vuln.ts) to exercise both safe and vulnerable cases.
- Update or create Go test assertions in infrastructure/parser/langgraphjs_test.go to check that the rule fires or does not fire as expected.
- Run regression tests to ensure no unintended changes to existing fixtures.

## Notes

- Treat this as a scaffold, not a hard-coded script.
- Update the command if the workflow evolves materially.