---
name: feature-implementation-with-cli-and-infra
description: Workflow command scaffold for feature-implementation-with-cli-and-infra in shingan.
allowed_tools: ["Bash", "Read", "Write", "Grep", "Glob"]
---

# /feature-implementation-with-cli-and-infra

Use this workflow when working on **feature-implementation-with-cli-and-infra** in `shingan`.

## Goal

Implements a new feature that includes CLI commands, domain logic, infrastructure persistence, tests, and documentation.

## Common Files

- `cli/*.go`
- `cli/*_test.go`
- `domain/*.go`
- `domain/*_test.go`
- `infrastructure/*/*.go`
- `infrastructure/*/*_test.go`

## Suggested Sequence

1. Understand the current state and failure mode before editing.
2. Make the smallest coherent change that satisfies the workflow goal.
3. Run the most relevant verification for touched files.
4. Summarize what changed and what still needs review.

## Typical Commit Signals

- Add or modify CLI command implementation and its tests
- Add or modify domain logic and its tests
- Add or modify infrastructure layer for persistence and its tests
- Update or add documentation (docs/ and ADRs)
- Update CHANGELOG.md

## Notes

- Treat this as a scaffold, not a hard-coded script.
- Update the command if the workflow evolves materially.