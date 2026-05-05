> 🌐 Language: **English** (Japanese translation welcome — see [issue tracker](https://github.com/hatyibei/shingan/issues))

# Shingan LSP Server (`shingan-lsp`)

`shingan-lsp` is a Language Server Protocol implementation for Shingan,
providing IDE-grade workflow-graph diagnostics in any LSP-capable editor —
VS Code, Cursor, Neovim, Helix, Zed, JetBrains, Sublime Text. The binary
is a thin wrapper around the same `AnalysisOrchestrator` the CLI uses, so
the rules you see in your editor are byte-identical to the rules that run
in `shingan analyze` or in CI via `actions/shingan`.

> Status: **Beta** — protocol shape is stable, hover/codeAction return
> useful content today, but full source-position support depends on
> per-parser progress (see "Source positions" below).

---

## Architecture in one breath

```
                    +-----------------+
   editor (LSP) --- |  shingan-lsp    | --- AnalysisOrchestrator
        ^           |                 |          |
        |           |  SHA256 LRU     |     [20 builtin rules]
        +---------- |  diff cache     |          |
                    |                 |     ParserFactory
                    |  PythonHealth   |
                    +-----------------+
```

Three defensive layers (see [ADR-009](../shingan-adr.md#adr-009)):

1. **Diff cache** — `(format, sha256(content))` keyed LRU; cache hit ≈ 10–30 ms,
   cache miss ≈ 80–250 ms.
2. **Long-lived workers** — for the future LangGraph Python parser (Track P);
   today every parser is Go-native, so this layer is a no-op pass-through.
3. **Degraded mode** — if `python3` is missing or the langgraph package
   fails to import, the server publishes a single Info-severity diagnostic
   `shingan_degraded_mode: limited analysis — …` so users immediately
   understand why their feedback shrank.

---

## Editor setup

### VS Code / Cursor

The recommended path is the official extension:

```jsonc
// settings.json
{
  "shingan.lspPath": "/usr/local/bin/shingan-lsp"
}
```

The bundled `extensions/vscode-shingan/` already spawns `shingan-lsp` on
`onLanguage:go` and `onLanguage:json`; install it with `npx vsce package`
followed by VS Code → Extensions → "Install from VSIX…".

### Neovim (built-in LSP)

```lua
-- ~/.config/nvim/lua/shingan.lua
local lspconfig = require("lspconfig")

if not lspconfig.shingan then
  require("lspconfig.configs").shingan = {
    default_config = {
      cmd = { "shingan-lsp" },
      filetypes = { "json", "go" },
      root_dir = lspconfig.util.root_pattern(".git", "go.mod"),
    },
  }
end
lspconfig.shingan.setup({})
```

### Helix

```toml
# ~/.config/helix/languages.toml
[[language]]
name = "json"
language-servers = ["shingan-lsp"]

[language-server.shingan-lsp]
command = "shingan-lsp"
```

### Zed

```jsonc
// ~/.config/zed/settings.json
{
  "language_servers": {
    "shingan-lsp": {
      "binary": { "path": "shingan-lsp" }
    }
  },
  "languages": {
    "JSON":  { "language_servers": ["shingan-lsp"] },
    "Go":    { "language_servers": ["gopls", "shingan-lsp"] }
  }
}
```

---

## File-format mapping

| URI extension | LanguageID | Parser used   |
|---------------|------------|---------------|
| `.json`       | `json`     | `JSONParser`  |
| `.go`         | `go`       | `ADKGoParser` |
| (any)         | `samurai`  | `SamuraiParser` (opt-in, set explicitly in editor config) |
| (other)       | (other)    | empty publish — diagnostics cleared, no analysis |

If your project uses a non-`.json` extension for SamuraiAI workflows
(e.g. `.workflow`), associate the file pattern with `languageId: samurai`
in your editor.

> **Heads-up:** every `.go` file is currently routed to the ADK-Go parser.
> If your repository mixes ADK agent code with regular Go, opening a non-
> ADK file will surface a single `shingan_parse_error` Warning until
> heuristic detection (matching on `google.golang.org/adk` imports) lands
> in a follow-up. To silence the noise, scope the LSP to specific paths
> via your editor's `root_pattern` / workspace folder configuration.

---

## Diagnostic shape

Each `shingan` Finding becomes one LSP `Diagnostic`:

| LSP field       | Source                                       |
|-----------------|----------------------------------------------|
| `range`         | `Node.Pos` (1-based) → LSP (0-based); fallback `(0,0)-(0,1)` |
| `severity`      | `Critical → Error`, `Warning → Warning`, `Info → Information` |
| `code`          | `Finding.RuleName`                           |
| `source`        | `"shingan"`                                  |
| `message`       | `[node_id] Message — Suggestion`             |
| `data.node_id`  | `Finding.NodeID` (preserved for codeAction)  |
| `data.confidence` | `Finding.Confidence`                       |

The `Source = "shingan"` label lets users filter our diagnostics from
those of `gopls`, `eslint-lsp`, etc. when both servers are attached.

### Source positions

A diagnostic's `range` is precise when:

- The parser populates `Node.Pos` (today: ADK-Go always; JSON only when
  the user-authored payload includes a `pos` field).
- The Finding references that node by `NodeID`.

When the position is unknown, the diagnostic still appears in the editor's
Problems panel with a `(0,0)-(0,1)` fallback range — the only consequence
is that hovering over column 0 of line 1 shows the issue, rather than at
the offending node. JSON parsers will gain content-aware position
reporting in a follow-up.

---

## Hover and CodeAction

Hover over any diagnostic to see:

- Rule name and severity
- Originating node ID
- Full message + Suggestion text
- Confidence percentage (when < 100 %)
- Co-located findings (when multiple rules fire at the same range)

CodeAction returns one entry per finding with a non-empty Suggestion,
shown as a Quick Fix in the editor. Today the action is informational
(no buffer mutation); once Track R lands the visitor refactor and ADR-008
adds an `AutoFix` field on Finding, the same CodeAction handler will
populate `WorkspaceEdit` automatically.

---

## Cache TTL and performance

| Phase              | Latency       | Notes                                            |
|--------------------|---------------|--------------------------------------------------|
| Cache hit          | 10–30 ms      | SHA-256 → in-memory lookup, no parse             |
| Cache miss (small) | 80–150 ms     | parse + 20 rules concurrently                    |
| Cache miss (large) | 200–500 ms    | adk-go directory walk over 100+ files            |
| Cold start         | 50–80 ms      | go binary boot + first Initialize                |

Cache parameters:

- LRU capacity: **512 entries** (`infrastructure/cache.DefaultSize`)
- TTL: **1 hour** (`infrastructure/cache.DefaultTTL`)
- Key: SHA-256 of file contents, scoped by parser format

Hit rate in typical IDE usage exceeds 80 % because identical text (paste,
formatter round-trip, undo/redo) returns to the same hash.

---

## Degraded mode

When `python3` is unreachable at startup, every analysis includes an
extra Info-severity diagnostic:

```
[shingan_degraded_mode] shingan: limited analysis — python health not yet probed
```

This is purely a heads-up signal today — none of Shingan's 20 built-in
rules require Python. The diagnostic exists because Track P (LangGraph
parser) will introduce Python-dependent rules; users who see the notice
ahead of time can install `python3` once and avoid surprises later.

To silence the notice in environments where Python will never be
available (pinned containerised editors, locked-down CI workstations),
set `shingan.suppressDegradedMode = true` in your VS Code settings —
this knob is reserved for the extension; the LSP itself always emits the
diagnostic so external clients can render their own UX.

---

## Troubleshooting

**The server starts but no diagnostics ever appear.**
Check stderr; the LSP framing reserves stdout for protocol traffic, so
errors land on stderr only. Most editors expose this through an "Output"
panel filtered by language server.

**Diagnostics are stale after a save.**
Verify the editor is sending `didChange` — Shingan only re-analyses on
content change events, not file-system save notifications.

**Hover returns nothing on a clearly-flagged line.**
Findings without `Node.Pos` map to `(0,0)-(0,1)`. Hovering anywhere on
line 1 shows them; for nuanced ranges, parse-time positions need to land
on the offending parser (track via the SourcePos coverage matrix in
`docs/source-pos.md`).

---

## Related ADRs

- [ADR-009: LSP diff execution + degraded mode](../shingan-adr.md#adr-009) — design rationale
- [ADR-006: ESLint visitor pattern](../shingan-adr.md#adr-006) — future codeAction integration point
- [ADR-008: ConfidenceReason](../shingan-adr.md#adr-008) — richer hover content roadmap
