# PR: feat(cli): support directory input for JSON workflows

**Title:** `feat(cli): support directory input for json workflows`

**Branch suggestion:** `feat/cli-json-directory-input`

---

## Summary

Today, `--format json --input .` is rejected because directory input is only supported for `adk-go`, `langgraph`, and `crewai`. For monorepos with many workflow manifests, allowing directory input for `json` would make CI usage natural.

## Usage

```bash
shingan analyze --format json --input workflows/
```

Recurses and analyzes any file matching the configured glob.

## Changes

- Accept directory input for `--format json`.
- Default glob: `**/*.workflow.json`, `**/*.shingan.json`.
- New `--include` / `--exclude` flags for custom patterns.
- Aggregate findings across files; preserve per-file paths in output.

## Open question

Globs should be configurable in `.shingan.yaml`; happy to add that here or split.

## Test Plan

- [x] Single-file input still works exactly as before.
- [x] Directory input picks up matching files recursively.
- [x] Non-matching files are silently skipped (not errored).
- [x] `--include`/`--exclude` override defaults.
