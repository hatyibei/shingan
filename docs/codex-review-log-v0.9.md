# Codex full audit log — v0.8.7 → c3c6ef7 → ...

> Internal review record. Each slice fed to codex with a focused
> failure-mode prompt; findings triaged and tracked here.

## Slice index

| # | Slice | Files in scope | Status |
| - | --- | --- | --- |
| A | Plugin SDK public API | `plugin/`, `version/` | ✅ done — see below |
| B | CLI runtime extract | `cli/` | ✅ done — see below |
| C | Policy enforcement + analyze flow | `application/policy.go`, `cli/analyze.go` flow | ✅ done — see below |
| D | SARIF reporter | `infrastructure/reporter/sarif*.go` | ✅ done — see below |
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

## Slice B: CLI runtime extract

**4 findings + 6 OK confirmations** (High=1, Medium=2, Low=1).

| # | Severity | Site | Action |
|---|---|---|---|
| 1 | High | `cli/analyze.go` os.Exit(code) in RunE | **Fixed**: typed `exitCodeError`, cli.Run translates without process-kill; root + subcommands set SilenceErrors/SilenceUsage. |
| 2 | Medium | analyze writes to os.Stdout, ignores SetOut | **Fixed**: `analyzeFlags.stdout io.Writer` threaded from `cmd.OutOrStdout()`; writeOutput accepts the writer. |
| 3 | Low | stderr writes ignore SetErr | **Fixed (partial)**: `analyzeFlags.stderr` plumbed but not yet substituted at every os.Stderr call site — covered by writer threading scaffold. |
| 4 | Medium | Plugin side-effect import contaminates cli test binary | **Backlog**: architectural — proper isolation needs build tags or subprocess test. Mutex-protected registry means current contamination is benign; defer. |

**Regression tests added (2):**
- `TestRun_ReturnsAnalysisExitCode` — cli.Run returns 2 on Critical without os.Exit
- `TestRun_CleanExitsZero` — 0 exit + captured stdout via SetOut

All other categories returned OK (no findings): flag-precedence, NewRootCmd reusability, subcommand naming, MarkFlagRequired, help-text consistency.

Full suite green.

## Slice C: Policy enforcement + analyze flow

**5 findings**: Medium=2, Low=3. **No fail-fast bypass detected** (confirmed Codex round-2 work held).

| # | Severity | Site | Action |
|---|---|---|---|
| 1 | Medium | plugins: validation weaker than Register grammar | **Fixed**: duplicated `validPluginsSuffix` regex with `TestPluginNameSuffix_MatchesSDK` drift check. |
| 2 | Medium | Whitespace in plugins entries silently misclassified | **Fixed**: explicit rejection with dedicated error message. |
| 3 | Low | No dedupe of plugins list | **Backlog**: cosmetic. |
| 4 | Low | `policyExplicit` dead code | **Fixed**: deleted. |
| 5 | Low | Nearest-policy-wins not documented | **Backlog**: docs polish. |

**Regression tests added (2):**
- `TestPluginNameSuffix_MatchesSDK` — uppercase/hyphen/path-sep/space rejected
- `TestVerifyRequiredPlugins_WhitespaceRejected`

All suite green.

## Slice D: SARIF reporter

**4 findings**: Medium=3, Low=1.

| # | Severity | Site | Action |
|---|---|---|---|
| 1 | Medium | helpUri accepts relative DocsURL (SARIF requires absolute URI) | **Fixed**: only emit when `url.Parse(...).IsAbs()`. Existing test updated to use absolute. |
| 2 | Medium | NodeID concatenated into URI without encoding | **Fixed**: `url.PathEscape` in `workflow://nodes/<id>`. |
| 3 | Medium | rule.properties.precision is order-dependent (first-seen confidence) | **Fixed**: minimum confidence across findings — deterministic + conservative. |
| 4 | Low | No partialFingerprints for GitHub alert tracking | **Backlog**: optimisation, not correctness. |

**Regression tests added (3):**
- `TestSARIF_RelativeDocsURLOmitted`
- `TestSARIF_NodeIDIsURLEncoded`
- `TestSARIF_PrecisionIsMinimumAcrossFindings`

All suite green.
