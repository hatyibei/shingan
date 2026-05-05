# shingan-lint

> AI Agent Workflow Static Analyzer — `npx`-installable wrapper for [Shingan](https://github.com/hatyibei/shingan).

```bash
# zero-install one-shot
npx shingan-lint analyze --format adk-go --input ./agents

# project-local
pnpm add -D shingan-lint
pnpm exec shingan analyze --format json --input ./testdata/buggy.json

# global
npm install -g shingan-lint
shingan analyze --since main
```

## What it does

Shingan detects 20 classes of design-time bugs in AI agent workflows **before they ship**:

- **Infinite loops** (`loop_guard`, `cycle_detection`)
- **Cost explosions** (`cost_estimation`, `retry_storm`, `max_parallel_branches`)
- **PII / prompt injection** (`pii_leak_scanner`, `prompt_injection_sink`, `secret_in_prompt_template`)
- **Code-execution risk** (`eval_missing`, `dynamic_node_construction`)
- **Misconfiguration** (`deprecated_model`, `model_card_mismatch`, `temperature_misuse`)
- **Architectural smells** (`circular_dep_agents`, `unbounded_tool_arg`, `error_handler_checker`, `redundant_llm_call`, `unreachable_node`, `missing_eval_dataset`)

Supported frameworks: **LangGraph** (Python), **ADK-Go** (Google), **n8n** / **SamuraiAI** / generic JSON.

## How this package works

`shingan-lint` is a thin Node wrapper. On `npm install` it downloads the platform-specific Go binary from the matching GitHub Release, verifies its SHA-256 against `checksums.txt`, and installs it under `~/.cache/shingan-lint/v<version>/`. Subsequent `shingan` invocations spawn the cached binary directly — no Node overhead at runtime.

| Platform | Supported |
|---|---|
| Linux x64 / arm64 | ✅ |
| macOS Intel / Apple Silicon | ✅ |
| Windows x64 / arm64 | ✅ |

If your platform isn't listed, install via Go directly:
```bash
go install github.com/hatyibei/shingan/cmd/shingan@v0.6.0
```

## Environment variables

| var | purpose |
|---|---|
| `SHINGAN_SKIP_POSTINSTALL=1` | skip the download step (air-gapped CI; you provide the binary yourself) |
| `SHINGAN_CACHE_DIR=/some/dir` | override `~/.cache` for the binary cache |
| `SHINGAN_DOWNLOAD_BASE=https://mirror/...` | mirror base URL (corporate proxies) |

## Documentation

Full rule list, ADRs, LSP setup, and contributing guide live in the main repository:
- [README](https://github.com/hatyibei/shingan)
- [Rule authoring guide](https://github.com/hatyibei/shingan/blob/main/docs/rule-authoring.md)
- [LSP integration](https://github.com/hatyibei/shingan/blob/main/docs/lsp.md)
- [ADRs](https://github.com/hatyibei/shingan/blob/main/shingan-adr.md)

## License

MIT — see [LICENSE](https://github.com/hatyibei/shingan/blob/main/LICENSE).
