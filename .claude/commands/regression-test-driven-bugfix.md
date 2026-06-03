---
name: regression-test-driven-bugfix
description: Workflow command scaffold for regression-test-driven-bugfix in shingan.
allowed_tools: ["Bash", "Read", "Write", "Grep", "Glob"]
---

# /regression-test-driven-bugfix

Use this workflow when working on **regression-test-driven-bugfix** in `shingan`.

## Goal

Fixing a bug or edge case and adding regression tests to prevent recurrence.

## Common Files

- `CHANGELOG.md`
- `**/*.go`
- `**/*_test.go`
- `go.mod`
- `go.sum`

## Suggested Sequence

1. Understand the current state and failure mode before editing.
2. Make the smallest coherent change that satisfies the workflow goal.
3. Run the most relevant verification for touched files.
4. Summarize what changed and what still needs review.

## Typical Commit Signals

- Identify and fix the bug in the relevant implementation file(s).
- Add or update a regression test in the corresponding *_test.go file to cover the bug scenario.
- Update CHANGELOG.md to document the fix.
- Update go.mod/go.sum if dependencies are added or changed.

## Notes

- Treat this as a scaffold, not a hard-coded script.
- Update the command if the workflow evolves materially.