# PR: feat(cli): add doctor diagnostic command

**Title:** `feat(cli): add doctor command for environment diagnostics`

**Branch suggestion:** `feat/cli-doctor`

---

## Summary

Add a `shingan doctor` command that diagnoses common first-run issues — missing binary, wrong cache path, unreachable GitHub Releases, missing language runtimes for `langgraph`/`crewai`/`adk-go` parsers.

## Motivation

When `shingan-lint` fails to work in a restricted environment, users need a single command that surfaces all the relevant state. This is particularly valuable for the npm wrapper, which can fail silently or produce confusing errors when the platform binary is missing.

## What `doctor` reports

```text
$ shingan doctor

shingan-lint version: 0.8.7
binary:               /home/user/.cache/shingan-lint/v0.8.7/shingan   [OK]
platform:             linux/amd64

environment:
  SHINGAN_CACHE_DIR:       (unset)
  SHINGAN_DOWNLOAD_BASE:   (unset)
  SHINGAN_SKIP_POSTINSTALL: (unset)
  HTTPS_PROXY:             (unset)

connectivity:
  https://github.com/hatyibei/shingan/releases  [OK 200]

parsers:
  json:      [OK]
  langgraph: [WARN — python3 not found in PATH]
  crewai:    [WARN — python3 not found in PATH]
  adk-go:    [OK — go 1.22.3]

policy:
  .shingan.yaml: (not found in current directory)

Run with --json for machine-readable output.
```

## Changes

- New `cmd/doctor` command.
- `--json` flag for CI consumption.
- Exit code: `0` if all OK, `1` if any required check fails, `2` if only warnings.

## Test Plan

- [x] `doctor` runs without external dependencies.
- [x] Connectivity check has a 5s timeout so it doesn't hang in offline environments.
- [x] `--json` output is stable schema (documented).
