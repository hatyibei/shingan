---
name: bugfix-with-test
description: Workflow command scaffold for bugfix-with-test in shingan.
allowed_tools: ["Bash", "Read", "Write", "Grep", "Glob"]
---

# /bugfix-with-test

Use this workflow when working on **bugfix-with-test** in `shingan`.

## Goal

Fixes a bug in CLI or infrastructure code and adds or updates regression tests.

## Common Files

- `cli/*.go`
- `cli/*_test.go`
- `infrastructure/*/*.go`
- `infrastructure/*/*_test.go`

## Suggested Sequence

1. Understand the current state and failure mode before editing.
2. Make the smallest coherent change that satisfies the workflow goal.
3. Run the most relevant verification for touched files.
4. Summarize what changed and what still needs review.

## Typical Commit Signals

- Update CLI or infrastructure code to fix the bug
- Add or update relevant tests to cover the bug scenario

## Notes

- Treat this as a scaffold, not a hard-coded script.
- Update the command if the workflow evolves materially.