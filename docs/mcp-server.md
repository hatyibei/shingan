> 🌐 Language: **English** | [日本語](./mcp-server.ja.md)

# Shingan MCP Server (`shingan-mcp`)

`shingan-mcp` is a Model Context Protocol (MCP) server that exposes Shingan's
workflow-graph static analyzer over stdio. Any MCP-capable client — Claude
Desktop, Cursor, Claude Code, Zed, LangGraph Studio — can call the four
tools below from a chat or completion flow, with no extra wrapper code.

The binary is a thin layer on top of `application/orchestrator.go` and the
existing `infrastructure/factory` factories; it reuses the same rules and
parsers as the CLI (`shingan analyze`).

---

## Tools

### `shingan_analyze_graph(graph_json: string) → FindingList`

Run all 20 built-in rules against a `WorkflowGraph` JSON payload.

Input:

```json
{ "graph_json": "{\"nodes\":[...],\"edges\":[...],\"entry_node_id\":\"start\"}" }
```

Output (`FindingList`):

```json
{
  "findings": [
    {
      "rule_name": "loop_guard",
      "severity":  "critical",
      "node_id":   "loop",
      "message":   "Loop node \"loop\" has no max_iterations configured",
      "suggestion":"Set Config[\"max_iterations\"] to a positive integer",
      "confidence": 1.0
    }
  ],
  "count": 1
}
```

Sort order: `severity` descending → `confidence` descending → `rule_name` ascending.

### `shingan_analyze_file(path: string, framework: string) → FindingList`

Read a workflow from disk and analyse it. `framework` is one of:

| framework | accepts              |
|-----------|----------------------|
| `json`    | single `.json` file  |
| `adk-go`  | single `.go` file OR a directory walked recursively |
| `samurai` | single SamuraiAI JSON file |

Same `FindingList` output shape as `shingan_analyze_graph`.

### `shingan_explain_rule(rule_name: string) → RuleExplanation`

Returns a human-readable description (what the rule detects, why it matters,
severity, confidence rationale, and one concrete example) for any of the 10
built-in rule names.

Output:

```json
{
  "rule_name":   "loop_guard",
  "explanation": "loop_guard — flags LoopAgent / control nodes that lack Config[\"max_iterations\"]. ..."
}
```

Unknown rule names return an MCP error with the list of known names.

### `shingan_suggest_model(node_description: string, input_token_estimate: int) → ModelRecommendation`

Heuristic model picker. Recognises three buckets:

| signal                                                        | recommendation     |
|---------------------------------------------------------------|--------------------|
| description contains `reasoning` / `推論` / `complex` / …     | `claude-3-5-sonnet`|
| description contains `classification` / `extract` / … OR tokens < 1000 | `gpt-4o-mini`|
| everything else                                               | `gpt-4o`           |

Output:

```json
{
  "model":                       "gpt-4o-mini",
  "rationale":                   "Short / classification-style workload. ...",
  "estimated_cost_per_call_usd": 0.00006
}
```

Cost assumes output ≈ 10% of input, with a 50-token floor.

---

## Client configuration

### Claude Desktop (`claude_desktop_config.json`)

```json
{
  "mcpServers": {
    "shingan": {
      "command": "/usr/local/bin/shingan-mcp"
    }
  }
}
```

### Cursor (`~/.cursor/mcp.json`)

```json
{
  "mcpServers": {
    "shingan": {
      "command": "shingan-mcp"
    }
  }
}
```

### LangGraph Studio

Studio is an Electron app with built-in MCP support. In the Settings pane,
add a new MCP server with:

- Name: `shingan`
- Command: `shingan-mcp`
- Transport: stdio

Once connected, the four tools appear in the tool picker next to the Play
button and can be invoked on the currently open graph.

### Claude Code (`~/.claude.json`)

```json
{
  "mcpServers": {
    "shingan": {
      "command": "shingan-mcp",
      "args": []
    }
  }
}
```

---

## Building and installing

```bash
go install github.com/hatyibei/shingan/cmd/shingan-mcp@latest
```

Or from a local checkout:

```bash
go build -o shingan-mcp ./cmd/shingan-mcp
```

Smoke test the stdio handshake:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.0.1"}}}' \
  | shingan-mcp | head -5
```

Expected response begins with:

```json
{"jsonrpc":"2.0","id":1,"result":{"capabilities":{...},"protocolVersion":"2024-11-05","serverInfo":{"name":"shingan","version":"dev"}}}
```

---

## Architecture notes

- MCP SDK: [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk) (official, v1.5.x).
- Transport: stdio only. HTTP / SSE transports are not enabled — the entire
  surface is meant to be local-only so graph payloads never leave the host.
- The server is stateless between tool calls; each call reconstructs the
  analyzer rule set via `AnalyzerFactory.CreateAll()`.
- Onion boundary is preserved: `cmd/shingan-mcp` only depends on
  `application` (interfaces) and `infrastructure/factory` (wiring), never
  on infrastructure packages directly.
