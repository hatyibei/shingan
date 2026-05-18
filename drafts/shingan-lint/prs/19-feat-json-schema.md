# PR: feat(json): publish JSON Schema for the workflow manifest

**Title:** `feat(json): publish JSON Schema for the workflow manifest`

**Branch suggestion:** `feat/json-schema-publish`

---

## Summary

Publish a JSON Schema document for the workflow manifest format. Enables editor autocompletion, in-IDE validation, and external CI checks without invoking `shingan`.

## Changes

- Add `schemas/workflow.schema.json` to the repo.
- Host at a stable URL (GitHub Pages or raw.githubusercontent.com — maintainer's preference).
- Update README "JSON workflow manifest" section to reference `$schema`.
- Add a CI check that asserts the in-repo example manifests validate against the schema.

## Example user usage

```json
{
  "$schema": "https://shingan.dev/schema/workflow.schema.json",
  "entry_node_id": "start",
  "nodes": [...],
  "edges": [...]
}
```

## Test Plan

- [x] Schema validates all example manifests in the repo.
- [x] Schema rejects deliberately malformed examples.
- [x] VS Code picks up `$schema` and provides completion.
