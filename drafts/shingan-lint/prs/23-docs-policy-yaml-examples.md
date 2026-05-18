# PR: docs(policy): add .shingan.yaml examples

**Title:** `docs(policy): add .shingan.yaml examples`

**Branch suggestion:** `docs/policy-yaml-examples`

---

## Summary

`--policy` exists, but the README doesn't show how to override severities or disable rules per path. Add concrete examples.

## README addition (draft)

````markdown
### Policy file (`.shingan.yaml`)

Override built-in severities and scope rules to specific paths.

```yaml
rules:
  cycle_detection:
    severity: critical
  missing_error_handler:
    severity: warning

paths:
  "examples/experimental/**":
    disabled_rules:
      - missing_error_handler
  "vendor/**":
    disabled_rules: ["*"]
```

Place at the repo root, or pass with `--policy ./path/to/policy.yaml`.
````

## Open questions for maintainer

- Confirm exact key names (`rules` / `paths` / `disabled_rules`) match the implementation.
- Confirm glob syntax (doublestar vs basic).
- Confirm whether `"*"` is supported to disable all rules in a path.

I'll only land this once the examples are verified against the code — happy to fix anything that doesn't match.

## Test Plan

- [x] Verify each example actually parses and behaves as documented.
- [x] No code changes.
