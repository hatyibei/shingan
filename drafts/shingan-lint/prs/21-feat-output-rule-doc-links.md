# PR: feat(output): include rule documentation URIs in findings

**Title:** `feat(output): include rule documentation URIs in findings`

**Branch suggestion:** `feat/output-rule-doc-links`

---

## Summary

Every finding emits its `rule_id` but does not include a link to the rule's documentation. Add `help_uri` so GitHub Code Scanning, editor integrations, and human readers can jump straight to the explanation.

## Output change

```json
{
  "rule_id": "cycle_detection",
  "severity": "warning",
  "message": "...",
  "help_uri": "https://github.com/hatyibei/shingan/blob/main/docs/rules/cycle_detection.md"
}
```

SARIF gains `helpUri` per rule (already part of the spec — `tool.driver.rules[].helpUri`).

## Changes

- Add a `doc_url` field to the rule registry; populate for all built-ins.
- Emit `help_uri` in JSON output and `helpUri` in SARIF.
- Markdown output links the rule name.
- A CI check ensures every registered rule has a non-empty `doc_url`.

## Test Plan

- [x] Each built-in rule has a populated `doc_url`.
- [x] JSON / SARIF / Markdown outputs include the link.
- [x] SARIF output validates against the schema.
