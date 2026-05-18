# PR: feat(cli): extend explain to accept a finding payload

**Title:** `feat(cli): support explain --finding <file>`

**Branch suggestion:** `feat/cli-explain-finding`

---

## Summary

The existing `explain` command surfaces rule documentation. Extend it to accept a single finding from `analyze --format json` and produce a focused, actionable explanation including the offending node/edge context.

## Usage

```bash
shingan analyze --input workflow.json --output json > findings.json
jq '.findings[0]' findings.json > finding.json

shingan explain --finding finding.json
```

Output: rule summary, why it triggered for this specific input, suggested fix, doc URL.

## Changes

- `--finding <path>` (or `-` for stdin) on `explain`.
- Reuses the rule registry; no new rule metadata required.

## Test Plan

- [x] Round-trips: `analyze` → finding → `explain` produces a non-empty explanation.
- [x] Unknown rule id in finding → clear error.
- [x] Existing `explain <rule_id>` form unchanged.
