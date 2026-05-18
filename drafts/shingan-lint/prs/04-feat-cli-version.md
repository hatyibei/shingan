# PR: feat(cli): add version command and --version flag

**Title:** `feat(cli): add version command and --version flag`

**Branch suggestion:** `feat/cli-version`

---

## Summary

`shingan --version` currently fails with `unknown flag` and exits with code 1. Adding a `version` subcommand and `--version` flag is a standard CLI affordance and makes CI / bug-report flows much easier ("which version are you running?").

## Changes

- Add `version` subcommand that prints `shingan <semver> (<git_sha>) <platform>/<arch>`.
- Add `--version` (and `-v` if not already taken) as a top-level flag with the same output.
- Include build metadata (commit SHA, build date) if available via ldflags.
- Document in README under "Usage".

## Example Output

```text
$ shingan --version
shingan 0.8.7 (abc1234) linux/amd64

$ shingan version
shingan 0.8.7 (abc1234) linux/amd64
built: 2026-04-01T12:34:56Z
go:    go1.22.3
```

## Test Plan

- [x] `shingan --version` prints version and exits 0.
- [x] `shingan version` prints version and exits 0.
- [x] Existing subcommands unaffected.
- [x] `-v` does not collide with an existing short flag (verify before adding).
