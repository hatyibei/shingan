# PR: feat(output): redact secret-like values in findings

**Title:** `feat(output): redact secret-like values in findings`

**Branch suggestion:** `feat/output-redact-secrets`

---

## Summary

If `secret_exposure_scanner` (or similar rules) ever surfaces a token, API key, or password as part of a finding's offending value, that data flows into logs, CI artifacts, and SARIF uploads. Default behavior should redact it; raw values should require an opt-in.

## Changes

- Default: any value that matches the secret-detection heuristics is replaced with `***` plus a short tag (`***[AWS_ACCESS_KEY]`, `***[GENERIC_TOKEN]`).
- New flag: `--show-secrets` to disable redaction (with a clear warning in stderr).
- Applies to all output formats: text, JSON, SARIF, Markdown.

## Motivation

- Avoids leaking credentials into CI logs.
- Avoids leaking into Code Scanning UIs that may render values.
- Sensible default; users who need the raw value can still get it explicitly.

## Test Plan

- [x] Default output never includes raw matched secret strings.
- [x] `--show-secrets` emits the raw value and prints a stderr warning.
- [x] Snapshot tests cover all output formats.
