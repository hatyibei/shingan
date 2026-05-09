> 🌐 Language: **English** (Japanese translation welcome — see [issue tracker](https://github.com/hatyibei/shingan/issues))

# Shingan PR bot

A sticky GitHub PR comment summarising Shingan's findings. Designed for incremental adoption: keeps your CI green (`fail-on: never`) while making findings visible to reviewers.

## What it looks like

When the workflow finds issues:

> ## 🔍 Shingan static analysis
>
> **Summary**: 7 findings — 1 Critical · 5 Warning · 1 Info
>
> | Severity | Rule | Node | Confidence | File:Line |
> |---|---|---|---|---|
> | critical | `eval_missing` | `crew::tool::python_repl` | 90% | `src/crew.py:42` |
> | warning  | `error_handler_checker` | `crew::task::Answer-0` | 80% | `src/crew.py:30` |
> | …
>
> <sub>Posted by [shingan](https://github.com/hatyibei/shingan) · use `# shingan: ignore <rule>` to suppress an individual finding · `.shingan.yaml` for repo-level overrides</sub>

A subsequent PR push **updates the same comment in place** (sticky marker `<!-- shingan-pr-comment-marker -->`) so the timeline doesn't fill with duplicates.

When there are no findings:

> ## 🔍 Shingan static analysis
>
> ✅ **No findings.**

## Setup

```yaml
# .github/workflows/shingan.yml
name: shingan
on: pull_request
permissions:
  contents: read
  pull-requests: write          # required for the sticky comment
jobs:
  analyze:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }   # for --since to compute the diff
      - uses: hatyibei/shingan@v1
        with:
          format: langgraph        # or crewai / n8n / adk-go / json / samurai
          input: ./agents
          pr-comment: true         # ← enables this feature
          pr-comment-mode: on-findings   # always | on-findings | on-failure
          fail-on: never           # informational — let the comment do the talking
          # Recommended companions:
          baseline-file: .shingan/baseline.json
          policy-file: .shingan.yaml
          since: ${{ github.event.pull_request.base.ref }}
```

## Modes

| `pr-comment-mode` | When the comment is posted |
|---|---|
| `always` | Every PR run, including no-findings PRs (✅ "No findings" sticky) |
| `on-findings` (default) | Only when total > 0 |
| `on-failure` | Only when the analyzer's exit code trips the configured `fail-on` threshold |

## Permissions

The bot uses the workflow's `${{ github.token }}` — no extra secret needed. Make sure the workflow declares `pull-requests: write` (the line above), otherwise the comment-posting step fails with a clear `Resource not accessible by integration` error in the workflow log.

For PRs from forks: `pull_request` events from forks **don't** have write permission. Switch the trigger to `pull_request_target` (and read about the [security implications](https://securitylab.github.com/research/github-actions-preventing-pwn-requests/)) — usually you want `pull_request_target` only when the analysis itself doesn't execute untrusted code, which is exactly Shingan's situation.

## Combining with other features

| Companion | Effect |
|---|---|
| [`# shingan: ignore`](./ignore-comments.md) | Authors can suppress individual findings inline; the bot honours these so the comment only shows what's actionable |
| [`.shingan.yaml`](./severity-policy.md) | Org-level severity overrides are applied before the comment is built |
| [`--baseline.json`](./diff-mode.md) | Pre-existing findings are suppressed; the bot only highlights new debt |
| [`--since=<base>`](./diff-mode.md) | Limits analysis to changed files; tighter signal for big repos |

## Customising the comment body

The current comment is the markdown table above. The roadmap (Phase 0.5/3 → Phase 0.6) covers:

- **Inline review comments** at the exact diff line (when `Finding.Pos.Line` is populated by the parser)
- **`/shingan disable <rule>` PR comment** — author replies with this to add a one-shot suppression scoped to the PR
- **Per-team Slack notifications** when severity ≥ critical
