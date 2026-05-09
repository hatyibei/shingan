> 🌐 Language: **English** (Japanese translation welcome — see [issue tracker](https://github.com/hatyibei/shingan/issues))

# `.shingan.yaml` — Severity Policy

Static analyzers earn organizational adoption when teams can express *"downgrade rule X to Info"* / *"off rule Y in legacy/"* without forking Shingan or modifying CI scripts. Shingan reads a YAML policy file in the ESLint / golangci-lint tradition.

## Quick start

Drop a `.shingan.yaml` at your repo root:

```yaml
# .shingan.yaml
rules:
  eval_missing:
    severity: critical          # already the default — explicit is fine
  unbounded_tool_arg:
    severity: info              # downgrade
  retry_storm:
    enabled: false              # disable globally

overrides:
  - paths:
      - "legacy/**"
      - "experiments/**"
    rules:
      cycle_detection:
        enabled: false
      error_handler_checker:
        severity: info
```

`shingan analyze` walks up from the current directory looking for `.shingan.yaml` (or `.shingan.yml`). Pass `--policy=<path>` to override the auto-discovery.

```bash
shingan analyze --format langgraph --input ./agents
shingan analyze --format crewai --input ./crew.py --policy=./.shingan.yaml
```

## Resolution order

Later wins. For each finding:

1. **Built-in rule defaults** (Severity, Confidence — produced by the rule)
2. **Top-level `rules:`** entries — replace severity / disable
3. **Matching `overrides[].rules`** — path-pattern based (`legacy/**`, `**/test_*.py`)

## Per-rule fields

| Field | Type | Effect |
|---|---|---|
| `severity` | `critical` / `warning` / `info` / `off` | Replaces the rule's default Severity. `off` is shorthand for `enabled: false`. |
| `enabled` | bool | `false` → drop every finding from this rule for the matched scope. |

## Path globs (`overrides[].paths`)

- `legacy/**` — anything inside `legacy/` (any depth)
- `**/test_*.py` — any `test_*.py` regardless of directory
- `experiments/agents.py` — exact filename match
- Multiple paths are OR'd together; multiple rules within an override block all apply.

Matching is performed against `Finding.SourceFile`, which is the repo-relative path returned by the parser pipeline.

## Common patterns

### Onboard a legacy tree without breaking CI

```yaml
overrides:
  - paths: ["legacy/**", "third_party/**"]
    rules:
      cycle_detection: { enabled: false }
      error_handler_checker: { enabled: false }
      unreachable_node: { enabled: false }
```

Pair this with `shingan analyze --baseline=baseline.json` for a more granular freeze (per-finding) on actively-developed code.

### Make `eval_missing` blocking everywhere except sandboxed test crews

```yaml
rules:
  eval_missing:
    severity: critical
overrides:
  - paths: ["tests/**"]
    rules:
      eval_missing:
        severity: info
```

## How it integrates with `# shingan: ignore`

Both mechanisms suppress findings, but they have different scopes:

| Mechanism | Scope | Best for |
|---|---|---|
| `# shingan: ignore eval_missing` (line / file comment) | Single line / file | Inline opt-out where the developer decides |
| `.shingan.yaml` policy | Per-repo / per-path | Organization-level decisions ("we don't enforce X here") |
| `--baseline.json` | Per-finding fingerprint | Freezing legacy debt while gating new findings |

Apply order at runtime (later steps see fewer findings):

1. Rules run, producing raw findings with built-in severities.
2. `# shingan: ignore` comments suppress per-line / per-file matches.
3. `.shingan.yaml` policy adjusts severity or drops disabled rules.
4. `--baseline.json` (if present) suppresses fingerprints already known.
5. `--min-confidence` filters by confidence threshold.

## Validating a policy

```bash
shingan analyze --policy=.shingan.yaml --input=. --format=langgraph --output=markdown
```

A malformed policy is logged to stderr but doesn't break the analysis — Shingan falls back to rule defaults so misconfigured CI still gives signal.

## Roadmap (Phase 0.5–0.6)

- **Per-PR overrides** via PR-bot `/shingan disable cycle_detection` comments (Phase 0.5 PR-bot)
- **Schema validation** for policy files (Phase 0.6) — catch typos at load time
- **Re-emit suppressed findings as `Info`** instead of dropping (Phase 1.0) — keeps the trail visible during audits
