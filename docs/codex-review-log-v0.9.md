# Codex full audit log — v0.8.7 → c3c6ef7 → ...

> Internal review record. Each slice fed to codex with a focused
> failure-mode prompt; findings triaged and tracked here.

## Slice index

| # | Slice | Files in scope | Status |
| - | --- | --- | --- |
| A | Plugin SDK public API | `plugin/`, `version/` | ✅ done — see below |
| B | CLI runtime extract | `cli/` | pending |
| C | Policy enforcement + analyze flow | `application/policy.go`, `cli/analyze.go` flow | pending |
| D | SARIF reporter | `infrastructure/reporter/sarif*.go` | pending |
| E | ADK-Go parser | `infrastructure/parser/adkgo*.go`, `domain/graph.go` | pending |
| F | Rule catalog + domain | `application/rule_catalog.go`, NodeType marshalling | pending |
| G | Tests & fixtures meta | `*_test.go` new, `testdata/` new | pending |

## Slice A: Plugin SDK public API

**13 findings** (High=4, Medium=5, Low=4). Critical=0.

| # | Severity | Site | Action |
|---|---|---|---|
| 1 | High | Manifest missing Description / Fixable | **Fixed**: fields added, ListRuleManifests consumes them, example plugin updated. |
| 3 | High | Name validation by prefix only — Unicode/whitespace/path-sep bypass | **Fixed**: `validNameSuffix` regex `^[a-z][a-z0-9_]{0,63}$`. |
| 5 | High | Invalid binary version silently passed compat check | **Fixed**: non-dev invalid Version returns ErrBadVersion. |
| 9 | High | ExperimentalPrefix duplicated, no drift test | **Fixed**: `TestExperimentalPrefix_MatchesSDK` in application/. |
| 4 | Medium | Tags per-entry validation (empty / control chars) | **Fixed**: TrimSpace + control-char reject. |
| 6 | Medium | Leading `v` in version.Version rejected | **Fixed**: `strings.TrimPrefix(binary, "v")` before semver compare. |
| 12 | Medium | Validation errors lack rule name | **Fixed**: every error wrapped with `rule %q:`. |
| 2 | Medium | `Frameworks []string` raw (vs typed enum) | **Backlog**: API ergonomics improvement, defer to v0.10 design. |
| 8 | Medium | `experimental:<builtin>` collision is dead code | **Accepted**: documented v0.x behavior, revisit at v1.0. |
| 10 | Medium | RegisteredRules order is init() order | **Accepted**: docstrings already note non-determinism; ListRuleManifests sorts. |
| 6, 7, 11, 13 | Low | Minor edge cases or no-issue confirmations | **Accepted**. |

**Regression tests added (4):**
- `TestRegister_NameValidationStrict` — 11 bad-name cases
- `TestRegister_TagsEntryValidation` — empty/whitespace/control-char tags
- `TestCheckShinganVersion_InvalidBinaryRejected` — non-dev "main" binary
- `TestCheckShinganVersion_VPrefixOnBinaryAccepted` — `v0.9.5` normalisation
- `TestExperimentalPrefix_MatchesSDK` — drift check

All suite green.
