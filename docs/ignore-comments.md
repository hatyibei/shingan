> 🌐 Language: **English** (Japanese translation welcome — see [issue tracker](https://github.com/hatyibei/shingan/issues))

# `shingan: ignore` Comments

Static analyzers earn developer trust by letting users opt out of specific findings without modifying CI policy. Shingan supports three marker scopes, in the ESLint / golangci-lint tradition:

```text
# shingan: ignore <rule_name>            # current line
# shingan: ignore-next-line <rule_name>  # next line
# shingan: ignore-file <rule_name>       # entire file
```

Multiple rule names may be comma-separated. Omitting the rule name disables **all** rules for that scope. Both `#` (Python / YAML) and `//` (Go / TS) comment prefixes are recognised so the same syntax works across every framework.

## Examples

### Suppress one rule on one line

```python
# Python (LangGraph / CrewAI)
agent = Agent(
    role="multi_tool_assistant",
    tools=[PythonReplTool()],  # shingan: ignore eval_missing
)
```

### Suppress one rule for the whole file

```python
# shingan: ignore-file eval_missing, prompt_injection_sink
"""This module has a sandboxed code-execution path; both rules are
expected and tracked separately in our threat model."""
```

### Suppress all rules on the next line

```python
# shingan: ignore-next-line
agent = Agent(role="experimental_agent")  # all rules off here
```

### Disable everything for an entire file (rare)

```python
# shingan: ignore-file
"""Generated code — analysed in the original source instead."""
```

### Go (ADK-Go) syntax

```go
// shingan: ignore-file cycle_detection, retry_storm
package main

import "google.golang.org/adk/agent/llmagent"
```

## Resolution semantics

- **File-level scope** wins: an `ignore-file` marker suppresses the rule everywhere in the file regardless of line number.
- **Line-level** markers attach to the line they appear on. Use `ignore-next-line` when the marker can't fit on the same line as the offending code (long expressions, multi-line calls).
- **Comma-separated rule lists** (`ignore-file eval_missing, retry_storm`) all disable on the same scope.
- **No rule list** (`ignore-file` alone) disables all rules — use sparingly; prefer naming the rules so the intent is obvious in code review.
- The ignore filter operates **after** rules run, on the orchestrator's combined finding list. Confidence and severity are unchanged; the finding is simply suppressed from output.

## n8n JSON workflows (no comments allowed)

JSON has no comment syntax, so n8n exports use a `_shingan_ignore` array on the node instead:

```json
{
  "name": "RiskyCodeNode",
  "type": "n8n-nodes-base.code",
  "parameters": { "jsCode": "..." },
  "_shingan_ignore": ["eval_missing", "unbounded_tool_arg"]
}
```

The semantics are identical to a per-line comment: rules in the array are suppressed for that specific node. Use `["*"]` to suppress every rule.

The field name is prefixed with `_` so it can't collide with future n8n schema additions, and Shingan strips it from the canonical Config view (so other rules don't see it as a regular n8n parameter).

## Related

- [`.shingan.yaml`](./severity-policy.md) — organisation-level severity overrides + per-path disable. Use this when a whole team / directory should treat a rule differently.
- [`--baseline.json`](./diff-mode.md) — fingerprint-based suppression for legacy debt. Use this when you want to gate **new** findings without fixing existing ones.

## How it works

The orchestrator reads each Finding's `SourceFile` (and its NodeID's source position when available), opens the file lazily, and parses every line for the three marker patterns. Markers are cached per file per analysis run so a directory walk doesn't re-read the same file. See `application/ignore.go`.

## Why this matters for adoption

Per the [v2 market analysis](../README.md#where-shingan-stands-today), Shingan's biggest adoption gap vs Snyk / Semgrep / ESLint isn't rule count — it's operational ergonomics. `ignore` comments are the single feature that turns "shingan blocks our CI on a finding we accept" from a deal-breaker into a one-line annotation.

Pair this with `--baseline.json` for legacy debt and `--severity-policy.yaml` (Phase 0.5) for organization-level rule overrides.
