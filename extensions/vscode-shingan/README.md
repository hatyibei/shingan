# Shingan VS Code Extension

Real-time AI agent workflow static analysis for VS Code.

Detects infinite loops, cost explosions, PII leaks, and other anti-patterns in
AI agent workflows (ADK-Go, LangGraph JSON, etc.) before execution.

## Install

1. Install `shingan-lsp` binary:
   ```bash
   go install github.com/hatyibei/shingan/cmd/shingan-lsp@latest
   ```
2. Install this extension from the VS Code Marketplace (or via `.vsix`).

## Settings

| Setting | Default | Description |
| --- | --- | --- |
| `shingan.lspPath` | `shingan-lsp` | Path to the LSP binary |
| `shingan.enabledRules` | `[]` | If empty, all rules enabled. Otherwise only these rule IDs. |
| `shingan.analyzeOnSave` | `true` | Analyze each file on save |
| `shingan.severityThreshold` | `info` | Minimum severity to report (`info` \| `warning` \| `critical`) |

## Commands

- **Shingan: Analyze Current File**
- **Shingan: Analyze Workspace**
- **Shingan: Show Rules Documentation**

## License

MIT
