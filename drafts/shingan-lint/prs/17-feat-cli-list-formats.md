# PR: feat(cli): add --list-formats

**Title:** `feat(cli): add --list-formats for machine-readable format discovery`

**Branch suggestion:** `feat/cli-list-formats`

---

## Summary

Tiny addition: emit the list of supported input formats in a stable, machine-readable form.

## Usage

```bash
$ shingan --list-formats
json
langgraph
crewai
adk-go

$ shingan --list-formats --json
{
  "formats": [
    { "id": "json",      "directory_input": true,  "requires": [] },
    { "id": "langgraph", "directory_input": true,  "requires": ["python3"] },
    { "id": "crewai",    "directory_input": true,  "requires": ["python3"] },
    { "id": "adk-go",    "directory_input": true,  "requires": ["go"] }
  ]
}
```

## Motivation

- Editor integrations can populate selectors without scraping `--help`.
- CI scripts can branch on whether a runtime dependency is needed.

## Test Plan

- [x] Plain output is stable.
- [x] `--json` output is documented and tested.
