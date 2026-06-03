---
name: feature-implementation-with-tests-and-docs
description: Workflow command scaffold for feature-implementation-with-tests-and-docs in shingan.
allowed_tools: ["Bash", "Read", "Write", "Grep", "Glob"]
---

# /feature-implementation-with-tests-and-docs

Use this workflow when working on **feature-implementation-with-tests-and-docs** in `shingan`.

## Goal

Implementing a new feature, adding tests, and updating documentation.

## Common Files

- `CHANGELOG.md`
- `**/*.go`
- `**/*_test.go`
- `docs/**/*.md`
- `README.md`
- `README.ja.md`

## Suggested Sequence

1. Understand the current state and failure mode before editing.
2. Make the smallest coherent change that satisfies the workflow goal.
3. Run the most relevant verification for touched files.
4. Summarize what changed and what still needs review.

## Typical Commit Signals

- Implement the new feature in the relevant Go source files.
- Add or update tests to cover the new feature.
- Update or create documentation files (docs/*.md, README.md, etc.) to describe the new feature.
- Update CHANGELOG.md to record the new feature.

## Notes

- Treat this as a scaffold, not a hard-coded script.
- Update the command if the workflow evolves materially.