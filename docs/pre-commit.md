> 🌐 Language: **English** (Japanese translation welcome — see [issue tracker](https://github.com/hatyibei/shingan/issues))

# pre-commit hook

Shingan ships a [pre-commit](https://pre-commit.com/) hook so agent workflows
are analyzed **before every commit**, catching infinite loops, cost
explosions, PII leak paths, and prompt-injection sinks at the earliest
possible moment — even before CI.

## Setup

Add to your `.pre-commit-config.yaml`:

```yaml
repos:
  - repo: https://github.com/hatyibei/shingan
    rev: v0.9.1 # pin to a release tag
    hooks:
      - id: shingan
```

Then install:

```bash
pre-commit install
```

The hook is built from source via `go install` on first run (pre-commit
downloads a Go toolchain automatically if none is present), then cached.

## Configuring format and input

The default runs `shingan analyze --format langgraph --input .` — analyzing
the whole repository as a LangGraph project. Override `args` for other
frameworks or a narrower input directory:

```yaml
hooks:
  - id: shingan
    args: ["analyze", "--format", "crewai", "--input", "./agents/"]
```

Supported formats: `json`, `adk-go`, `samurai`, `langgraph`, `n8n`, `crewai`,
`langgraph-js`, `pydantic-graph`, `llamaindex`, `autogen`, `mastra`,
`openai-agents`.

Useful additions:

- `--since main` — analyze only files changed since `main` (fast on large repos,
  see [diff-mode.md](./diff-mode.md))
- `--baseline .shingan-baseline.json` — suppress known findings during
  progressive adoption
- `--policy .shingan.yaml` — apply severity policy
  (see [severity-policy.md](./severity-policy.md))

```yaml
hooks:
  - id: shingan
    args:
      [
        "analyze",
        "--format", "langgraph",
        "--input", ".",
        "--since", "main",
      ]
```

## Exit behavior

`shingan analyze` exits `2` on Critical findings, `1` on Warning, `0` when
clean — pre-commit blocks the commit on any non-zero exit. To run
informationally while you triage existing findings, use a baseline file or
`--fail-on critical` so only Critical findings block.

## Notes

- The hook triggers when staged changes touch `.py` / `.go` / `.ts` / `.mts` /
  `.json` files; it then analyzes the configured `--input` (not just the staged
  files), because workflow graphs span multiple files.
- Python-shim formats (`langgraph`, `crewai`, `pydantic-graph`, `llamaindex`,
  `autogen`) require `python3` on PATH; see the per-framework docs
  ([langgraph.md](./langgraph.md), [crewai.md](./crewai.md)) for optional
  runtime-introspection dependencies. AST fallback works without them.
