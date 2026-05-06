> 🌐 Language: **English** | [日本語](./source-pos.ja.md)

# SourcePos — Node Position Information (Phase 2 Foundation)

> The Phase 2 LSP server, VS Code extension, and CodeAction features all
> read position information attached to `domain.Node.Pos`. This document
> summarizes that contract, the design intent, and future extension points.

## Design Intent

Through Phase 1, `domain.Finding` only carried a `NodeID` for location
information, which made it impossible to highlight the relevant line in an
IDE or perform a "Go to Finding" equivalent. To realize the following UX
in Phase 2, the parser must attach source positions to each `Node`:

- `range` for LSP `textDocument/publishDiagnostics`
- Edit-target ranges for LSP `textDocument/codeAction`
- Injection sites for VS Code Code Lens (`▶ Analyze` / `⚡ Quick Fix all`)
- `locations[].physicalLocation` for SARIF reports

None of these can be produced without knowing which line and column the
`Node` was written on. Position information is attached to the **`Node`
side**, not to the `Finding` — when multiple findings stem from the same
node, the position only needs to be recorded once.

## Type Definition

`domain/graph.go`:

```go
type SourcePos struct {
    File string `json:"file,omitempty"` // Parser-defined. Empty allowed for embedded input.
    Line int    `json:"line,omitempty"` // 1-based
    Col  int    `json:"col,omitempty"`  // 1-based
}

func (p SourcePos) IsZero() bool {
    return p.File == "" && p.Line == 0 && p.Col == 0
}

type Node struct {
    ID     string         `json:"id"`
    Name   string         `json:"name"`
    Type   NodeType       `json:"type"`
    Config map[string]any `json:"config,omitempty"`
    Pos    SourcePos      `json:"pos,omitempty"` // NEW in 0.6.0
}
```

### `IsZero()` Convention

- **Principle**: When the parser cannot determine a position, leave Pos at its zero value
- **Consumer's responsibility**: Use `SourcePos.IsZero()` to determine whether
  position information is present; if zero, omit range information or fall
  back to whole-file targeting
- The zero check is "all 3 fields empty". Treated as "unset" only when
  `File=""` and `Line=0` and `Col=0`

## How Each Parser Populates Pos

### ADK-Go Parser (`infrastructure/parser/adkgo.go`)

AST-based. It already holds a `token.FileSet`, so `cl.Pos()` and
`ident.Pos()` are converted into `SourcePos` via the `b.sourcePos(pos)`
helper:

```go
func (b *adkgoBuilder) sourcePos(pos token.Pos) domain.SourcePos {
    if !pos.IsValid() {
        return domain.SourcePos{}
    }
    p := b.fset.Position(pos)
    return domain.SourcePos{File: p.Filename, Line: p.Line, Col: p.Column}
}
```

Population sites:

- `processAgentLit` — bare struct literals (`&LlmAgent{...}` etc.) use `cl.Pos()`
- `processRealAPIConfig` — real SDK calls (`loopagent.New(loopagent.Config{...})`)
  use `cfg.Pos()` to point at the Config composite literal (falling back to `callExpr.Pos()`)
- `processToolElement` — `Pos()` of tool node identifiers / expressions
- `extractRealSubAgents`, `processSubAgent` — placeholder nodes from
  unresolved idents use `ident.Pos()`

In `Parse([]byte)`, the file name is hardcoded as `"input.go"`, so tests
verify against that value. `ParseFile(path)` only sets the actual file
path in Filename when the types pass succeeds.

### JSON Parser (`infrastructure/parser/json.go`)

No code change. Because `domain.Node`'s `Pos` field carries the
`json:"pos,omitempty"` tag:

- If the input JSON contains `"pos": {"file": ..., "line": ..., "col": ...}`, it is auto-decoded
- If there is no `"pos"` field, a `SourcePos{}` (IsZero) is produced
- Existing pre-v0.5.0 testdata works unchanged (backward compatible)

> ⚠️ Behavior of `omitempty`: Go's `encoding/json` does not consider a
> struct value "empty". Therefore, even when `Pos` is the zero value, the
> output of `json.Marshal` on a `WorkflowGraph` will always include
> `"pos": {}`. This does not affect input parsing (an empty object decodes
> to the zero value), and existing consumers do not reference the `Pos`
> field, so compatibility is preserved. If output-size minimization later
> becomes a requirement, an option is to change `Pos` to `*SourcePos`, but
> for Phase 2 we keep it as a value type (prioritizing the elimination of
> nil checks).

### SamuraiAI Parser (`infrastructure/parser/samurai.go`)

Since the schema is still in the assumed-spec stage, do not assume that
input JSON carries position information. Add an optional
`Pos *domain.SourcePos \`json:"pos,omitempty"\`` field to `SamuraiNode`
and copy it into `domain.Node.Pos` when present in the input. After the
real SamuraiAI schema is finalized, map and overwrite accordingly.

## Future Extensions

- Add `Range` to `domain.Finding` (`StartPos`, `EndPos`) — the current
  `Pos` is single-point, but LSP wants a range, requiring start and end
  positions of the node declaration. Slated for the full CodeAction
  implementation in Phase 2-F
- Python (LangGraph) Parser — works as-is via a contract that the JSON
  returned from `python -m shingan_export <file>` includes a `pos` field
- LSP server — when `SourcePos.IsZero()`, fall back to "file beginning,
  column 0" for the `publishDiagnostics` range. Even when the value is
  zero, still emit the diagnostic ("fired but position unknown" is still
  useful information)

## Backward Compatibility

- Existing JSON testdata does not have a `pos` field — all tests stay
  green (gated by `TestJSONParser_NoPosField_BackwardCompat`)
- Existing consumers (Reporter / Orchestrator) do not read `Pos`, so
  behavior is unchanged
- However, where `WorkflowGraph` is serialized to JSON and passed
  outside (currently nowhere), note that `"pos": {}` will appear in the
  output even for zero values — harmless unless the receiver runs in
  strict mode
