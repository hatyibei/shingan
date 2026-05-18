# PR: fix(cli): document and standardize exit codes

**Title:** `fix(cli): document and standardize exit codes`

**Branch suggestion:** `fix/cli-exit-codes`

---

## Summary

Observed: cycle-detection findings exit with code 2 today. Make the meaning of each exit code explicit so CI integrations can rely on it.

## Proposed mapping

| Code | Meaning |
|------|---------|
| 0    | No findings at or above the configured threshold |
| 1    | CLI / runtime / IO error |
| 2    | Findings at or above the configured threshold |
| 3    | Policy / config error (invalid `.shingan.yaml`, unknown rule, etc.) |
| 4    | Input parse / schema error |

## Changes

- Audit current exit codes; only change what's needed to match the table.
- Document under "Exit codes" in README.
- Add a table in `--help` epilogue (optional).

## Compatibility

If today's behavior already overloads 1 vs 2, list the precise change and call it out in the release notes — this is a breaking change for CI scripts that branch on exit codes.

## Test Plan

- [x] Each row in the table is exercised by at least one integration test.
- [x] `--fail-on never` always exits 0.
