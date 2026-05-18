# PR: fix(cli): clarify directory input error for json format

**Title:** `fix(cli): improve error message when json format gets a directory input`

**Branch suggestion:** `fix/cli-directory-error-message`

---

## Summary

Stand-alone, smaller alternative to #14 if directory support for json is out of scope.

Today's error: `directory input is only supported for adk-go, langgraph, and crewai formats`.

It's accurate but doesn't tell users what to do next.

## Proposed message

```text
shingan analyze: --format json expects a single file, not a directory.

Got:      ./workflows
Solution: pass a file path, e.g. --input workflows/main.json

(Directory input is supported for: adk-go, langgraph, crewai.)
```

## Changes

- Update only the error string. No behavior change.

## Test Plan

- [x] Snapshot test for the new error.
- [x] Existing format-specific errors unchanged.
