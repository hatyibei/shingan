# PR: feat(cli): add init command for JSON workflow template

**Title:** `feat(cli): add init command to scaffold a JSON workflow manifest`

**Branch suggestion:** `feat/cli-init`

---

## Summary

Lower the barrier to authoring JSON workflows by scaffolding a minimal valid manifest.

## Usage

```bash
shingan init --format json --output workflow.json
```

Generates:

```json
{
  "entry_node_id": "start",
  "nodes": [
    { "id": "start", "type": "llm", "name": "Planner" }
  ],
  "edges": []
}
```

## Changes

- New `init` subcommand.
- `--format` selects template (start with `json`, leave room for `langgraph`/`crewai`/`adk-go`).
- `--output` (default: `workflow.json`).
- `--force` to overwrite.

## Test Plan

- [x] `init` writes a file that passes `shingan analyze` without errors.
- [x] Refuses to overwrite without `--force`.
- [x] `--output -` writes to stdout.
