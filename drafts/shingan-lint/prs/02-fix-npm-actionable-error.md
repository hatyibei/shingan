# PR: fix(npm): print actionable error when cached binary is missing

**Title:** `fix(npm): print actionable error when cached binary is missing`

**Branch suggestion:** `fix/npm-missing-binary-message`

**Closes:** Issue #XX (the silent-exit issue)

---

## Summary

Improve the npm wrapper error handling when the platform-specific `shingan` binary is missing or not executable.

Currently, if postinstall fails (network issues, GitHub Releases blocked, etc.), commands such as:

```bash
npx --package shingan-lint shingan --help
npx --package shingan-lint shingan analyze --input .
```

exit with code 1 and little or no diagnostic output. The user has no idea the failure is about a missing binary.

## Changes

- Check whether the expected cached binary exists and is executable before spawning it.
- If missing, print a clear error message including:
  - expected binary path
  - package version
  - platform / architecture
  - relevant env vars
  - manual remediation steps
- Preserve existing behavior of `SHINGAN_CACHE_DIR`, `XDG_CACHE_HOME`, `SHINGAN_DOWNLOAD_BASE`, and `SHINGAN_SKIP_POSTINSTALL`.
- CLI argument forwarding unchanged on the happy path.

## Example Error Output

```text
shingan-lint: platform binary not found

Expected:
  /home/user/.cache/shingan-lint/v0.8.7/shingan

Package:  shingan-lint@0.8.7
Platform: linux/amd64

The binary is normally downloaded during postinstall. If your environment
blocks GitHub Releases, install it manually or set SHINGAN_CACHE_DIR to a
pre-populated cache directory.

Manual install:
  mkdir -p ~/.cache/shingan-lint/v0.8.7
  curl -L https://github.com/hatyibei/shingan/releases/download/v0.8.7/shingan_0.8.7_linux_amd64.tar.gz \
    | tar -xz -C ~/.cache/shingan-lint/v0.8.7

Environment variables:
  SHINGAN_CACHE_DIR       override cache directory
  SHINGAN_DOWNLOAD_BASE   override release download base URL
  SHINGAN_SKIP_POSTINSTALL=1  skip automatic download
```

## Motivation

This improves the first-run experience for users in:

- restricted CI runners
- corporate networks
- air-gapped environments
- `npx` one-shot usage where postinstall logs are easy to miss

## Test Plan

- [x] Existing binary path still forwards arguments to the Go binary.
- [x] Missing binary prints the actionable error and exits non-zero.
- [x] `SHINGAN_CACHE_DIR` is reflected in the "Expected" path.
- [x] `SHINGAN_SKIP_POSTINSTALL=1` install + manual placement still works.
