# PR: feat(cli): add --fail-on severity threshold

**Title:** `feat(cli): add --fail-on severity threshold for analyze`

**Branch suggestion:** `feat/cli-fail-on`

---

## Summary

CI-friendly knob: control which severity levels cause a non-zero exit.

## Motivation

Today (best I can tell from the docs), any finding can fail the run. Teams adopting a linter usually want to:

- Start with `--fail-on critical` to land it without breaking CI.
- Tighten to `--fail-on warning` later.
- Run reports with `--fail-on never` for dashboards.

## Usage

```bash
shingan analyze --input workflow.json --fail-on critical
shingan analyze --input workflow.json --fail-on warning
shingan analyze --input workflow.json --fail-on info
shingan analyze --input workflow.json --fail-on never
```

Default behavior should match today's behavior (no behavior change without the flag).

## Changes

- New `--fail-on <level>` flag on `analyze`.
- Levels: `critical`, `warning`, `info`, `never`.
- Maps to exit code per #13 (standardize exit codes).
- Documented under "CI usage" in README.

## Test Plan

- [x] Default behavior unchanged when flag is absent.
- [x] `--fail-on critical` passes when only warnings are reported.
- [x] `--fail-on never` always exits 0 even with critical findings.
- [x] Invalid level prints usage and exits 1.
