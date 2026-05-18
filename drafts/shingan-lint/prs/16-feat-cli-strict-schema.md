# PR: feat(cli): add --strict-schema

**Title:** `feat(cli): add --strict-schema for input validation`

**Branch suggestion:** `feat/cli-strict-schema`

---

## Summary

Add an opt-in mode that rejects unknown fields, invalid node types, and other lenient-by-default schema deviations.

## Motivation

Lenient parsing is the right default (backward compatibility, gradual adoption), but teams that own their manifests want a tight contract.

## Usage

```bash
shingan analyze --input workflow.json --strict-schema
```

Fails (per #13: exit code 4) on:

- unknown top-level keys
- unknown node `type`
- unknown edge fields
- duplicate node `id`
- references to non-existent node `id` in edges

## Test Plan

- [x] Lenient default behavior unchanged.
- [x] Each strict check is exercised by a test.
- [x] Errors include JSON pointer / line info.
