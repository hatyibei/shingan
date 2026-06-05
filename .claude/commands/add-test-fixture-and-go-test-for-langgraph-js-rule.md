---
name: add-test-fixture-and-go-test-for-langgraph-js-rule
description: Workflow command scaffold for add-test-fixture-and-go-test-for-langgraph-js-rule in shingan.
allowed_tools: ["Bash", "Read", "Write", "Grep", "Glob"]
---

# /add-test-fixture-and-go-test-for-langgraph-js-rule

Use this workflow when working on **add-test-fixture-and-go-test-for-langgraph-js-rule** in `shingan`.

## Goal

Adds a new test fixture (typically a *_vuln.ts or *_safe.ts file) for langgraph-js, and updates Go tests to assert correct rule firing or non-firing.

## Common Files

- `testdata/langgraphjs/*.ts`
- `infrastructure/parser/langgraphjs_test.go`

## Suggested Sequence

1. Understand the current state and failure mode before editing.
2. Make the smallest coherent change that satisfies the workflow goal.
3. Run the most relevant verification for touched files.
4. Summarize what changed and what still needs review.

## Typical Commit Signals

- Create or update a test fixture in testdata/langgraphjs/ (e.g., prompt_injection_vuln.ts).
- Update infrastructure/parser/langgraphjs_test.go to add or broaden test assertions for the fixture.
- Run Go tests to verify correct rule behavior.

## Notes

- Treat this as a scaffold, not a hard-coded script.
- Update the command if the workflow evolves materially.