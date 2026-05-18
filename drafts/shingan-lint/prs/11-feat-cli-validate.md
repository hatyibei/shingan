# PR: feat(cli): add validate command

**Title:** `feat(cli): add validate command for schema-only checks`

**Branch suggestion:** `feat/cli-validate`

---

## Summary

Separate "is this input well-formed?" from "are there findings?". Today, `analyze` conflates the two — a parse error and a critical finding both produce non-zero exits, which is awkward for editor integrations and CI pre-checks.

## Usage

```bash
shingan validate --format json --input workflow.json
```

- Exit 0: input parses, no schema violations.
- Exit 1: CLI / runtime error.
- Exit 2: schema or structural error in the input (with location info).

No rule analysis is run.

## Changes

- New `validate` subcommand.
- Shares the parser stack with `analyze` but stops after structural validation.
- `--format` and `--input` accept the same values as `analyze`.

## Test Plan

- [x] Valid manifest exits 0.
- [x] Unknown node type or missing `entry_node_id` exits 2 with file/line.
- [x] No rule findings are emitted.
