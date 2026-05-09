> 🌐 Language: **English** (Japanese translation welcome — see [issue tracker](https://github.com/hatyibei/shingan/issues))

# Shingan Case Studies

Real-world dogfood reports on running Shingan against public AI-agent OSS. Each case study documents:

- the repository analysed,
- the parser used and how it was invoked,
- findings produced (with severity / confidence / rule),
- a brief take on whether each finding is a real bug or an expected baseline warning,
- gaps Shingan exposed in itself — bugs we shipped fixes for during the dogfood run.

## Index

| Repo | Framework | Findings | Notable | Doc |
|---|---|---|---|---|
| [crewAIInc/crewAI-examples](./crewAI-examples.md) | CrewAI | 7 (1 Critical) on `multi_tool.py`; 3 Warnings on `game-builder-crew/crew.py` | `eval_missing` Critical on Agent → `python_repl` path (LLM output → code execution sink) | [→](./crewAI-examples.md) |
| [Zie619/n8n-workflows community corpus](./n8n-community-workflows.md) | n8n | 15 findings across 3 sample workflows; multi-trigger + UUID-keyed connection bugs surfaced | Drove the v0.8.2 fix that lets Shingan read modern n8n exports correctly | [→](./n8n-community-workflows.md) |
| [assafelovic/gpt-researcher](./gpt-researcher.md) | LangGraph | parser improvements but 0 user-visible findings (factory built inside instance method, v0.9 AST track) | Surfaced the package-aware `sys.path` and dotted-name import gaps fixed in v0.8.1+ | [→](./gpt-researcher.md) |

## Methodology

Every fixture is run on the **public, npm-distributed `shingan-lint@<version>`** (not a dev checkout) so the report mirrors what an external user would see:

```bash
npm install -g shingan-lint@latest
shingan analyze --format <framework> --input <file-or-dir> --output markdown
```

Findings are reported verbatim — no curation. When Shingan hits a real issue inside the parser (rather than the user code), the case study links to the resulting fix commit so the timeline is auditable.

## Why publish dogfood reports?

1. **Honesty about coverage.** Most real OSS produces 0 findings on first run because the agent graph is built inside an instance method or factory function our v0.8 parsers don't traverse yet (v0.9 AST fallback in flight). We say so up front instead of cherry-picking.
2. **Concrete proof of value.** When Shingan does fire (n8n community workflows, CrewAI multi-tool patterns), the findings are immediately actionable.
3. **Fastest path to fixing Shingan itself.** Each case study double-serves as a regression suite — the fix commits link back here.
