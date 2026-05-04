# Diff Mode & Progressive Adoption (Phase 2-E)

Shingan ships with three CLI flags that make it practical to roll out on an
existing codebase without flooding developers with pre-existing findings on
day 1:

| Flag | Purpose |
|---|---|
| `--since=<git-ref>` | Analyze only files changed since the given git ref. |
| `--save-baseline=<path>` | Write the current set of findings as a baseline JSON. |
| `--baseline=<path>` | Suppress findings already present in the baseline. |

The three flags compose. `--baseline` filter is applied *before*
`--save-baseline` writes, so `--baseline + --save-baseline` saves only the
newly-introduced findings — useful when you want to grow the baseline one
acknowledged finding at a time.

## Typical Rollout Flow

```
# 1. One-time, on main, generate the baseline of every pre-existing finding.
shingan analyze -i ./agents/ --format adk-go \
    --save-baseline shingan-baseline.json --output json

# 2. Commit the baseline file.
git add shingan-baseline.json
git commit -m "chore: snapshot shingan baseline"

# 3. Per PR in CI, run Shingan with the baseline. Only new findings surface.
shingan analyze -i ./agents/ --format adk-go \
    --baseline shingan-baseline.json --output sarif --output-file shingan.sarif

# 4. When the team fixes legacy findings, refresh the baseline on main.
shingan analyze -i ./agents/ --format adk-go \
    --save-baseline shingan-baseline.json --output json
git add shingan-baseline.json
git commit -m "chore: refresh shingan baseline"
```

## Diff Mode (`--since`)

`--since=<ref>` delegates to `git diff --name-only <ref>..HEAD` and restricts
the analyzer to the intersection of:

1. Files returned by that diff.
2. Files under the `--input` path (file or directory prefix).

If the intersection is empty, Shingan short-circuits to 0 findings and exits 0.
This plays well with PR-only CI where `ref = origin/main`.

Combining `--since` with `--baseline` is supported and recommended: `--since`
restricts *what is analyzed*, `--baseline` filters *what is reported*. The
former bounds analyzer cost, the latter bounds developer noise.

## Baseline JSON Format

Baselines are forward-compatible JSON documents:

```json
{
  "generated_at": "2026-04-15T12:00:00Z",
  "findings": [
    {
      "rule": "cycle_detection",
      "node_id": "loop_body",
      "message": "Loop node \"loop_body\" has a cycle but max_iterations is not set: risk of infinite loop"
    }
  ]
}
```

A fingerprint is `(rule, node_id, message)`. Severity and confidence are
deliberately **not** part of the fingerprint — re-classifying a rule's severity
should not invalidate the entire baseline.

## GitHub Action

The `shingan` composite action accepts two new inputs:

```yaml
- uses: hatyibei/shingan@v0.6
  with:
    input: ./agents/
    format: adk-go
    output: sarif
    baseline-file: shingan-baseline.json   # optional
    since: origin/main                     # optional
```

Both inputs are optional. When empty, the action behaves exactly as before
(fully-backwards compatible).

## Progressive Adoption Cookbook

- **Existing codebase with 2000 findings** → generate the baseline on main,
  commit it, enable Shingan in CI with `--baseline`. PRs only surface
  regressions; the team tackles legacy debt on a separate cadence.

- **Fast CI budget** → combine `--since=origin/${{ github.base_ref }}` with
  `--baseline`. Shingan only parses changed files *and* only reports new
  findings. Analysis runs sub-second on monorepos.

- **Gradual rule rollout** → when turning on a new rule, regenerate the
  baseline right after enabling it. Existing violations are grandfathered; new
  ones fail CI.
