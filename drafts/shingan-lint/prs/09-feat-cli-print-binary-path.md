# PR: feat(cli): add --print-binary-path

**Title:** `feat(cli): add --print-binary-path to expose the underlying Go binary`

**Branch suggestion:** `feat/cli-print-binary-path`

---

## Summary

Add a wrapper-level flag that prints the absolute path to the resolved platform binary and exits. Useful for manual cache placement, CI debugging, and editor integrations.

## Example

```bash
$ shingan --print-binary-path
/home/user/.cache/shingan-lint/v0.8.7/shingan
```

If the binary is missing, exits non-zero with the actionable error from #02.

## Changes

- Wrapper handles `--print-binary-path` before spawning the binary.
- Document under "Usage".

## Test Plan

- [x] Prints the path and exits 0 when binary exists.
- [x] Prints actionable error and exits non-zero when missing.
- [x] Does not forward `--print-binary-path` to the underlying binary.
