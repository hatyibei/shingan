# PR: feat(npm): retry binary download at runtime

**Title:** `feat(npm): retry binary download when cached executable is missing`

**Branch suggestion:** `feat/npm-runtime-download`

---

## Summary

If postinstall fails (e.g. `npx` with `--ignore-scripts`, transient network failure), the wrapper currently has no recovery path. Add a runtime fallback: on the first invocation, if the cached binary is missing, attempt the download once before erroring out.

## Notes for reviewer

This is a slightly bigger change than #02 (actionable-error), and the two are complementary. Suggest landing #02 first as a no-op-on-success safety net, then this one as the actual recovery.

## Changes

- After the missing-binary check, attempt one download using the same logic as postinstall.
- Honor `SHINGAN_SKIP_POSTINSTALL=1` to disable runtime fallback as well (rename: `SHINGAN_SKIP_DOWNLOAD`?).
- Show a `Downloading shingan v0.8.7 for linux/amd64...` line before the spawn.
- On failure, fall through to the actionable error from #02.

## Test Plan

- [x] Happy path: cached binary present → no download, normal spawn.
- [x] Missing binary + network OK → downloads and spawns.
- [x] Missing binary + network blocked → actionable error, no infinite loop.
- [x] `SHINGAN_SKIP_DOWNLOAD=1` → does not attempt download, errors immediately.
