> 🌐 Language: **English** | [日本語](./eval-missing.ja.md)

# eval_missing — Design & Implementation Doc

> **Target version**: Phase 2 #2 (v0.6 series)
> **Implementation file**: `domain/rules/eval_missing.go`
> **Tests**: `domain/rules/eval_missing_test.go` (15 cases including subtests)
> **Tier (ADR-007)**: Path rule — implements Sources / Sinks / Propagate

---

## 1. Background & Motivation

The pattern of having an LLM-based agent "**execute as code** instructions written in natural language" is highly productive but maps directly onto one of the most dangerous vulnerability categories:

- LLM output is **dynamically generated**, so if an attacker can fabricate a code string via prompt injection / RAG poisoning / Tool return value, it leads to **arbitrary code execution (RCE)** through `eval()` / `exec()` / `Function()` / `code_interpreter`.
- The result: **credential leakage**, **access to internal system files**, **outbound exfiltration**, and **lateral movement to other tenants**.

This rule detects "reachability from LLM nodes to code-execution Tool nodes" from the **static structure** of the workflow graph, surfacing the issue at design time **before relying on a runtime sandbox**.

> **Limits of static analysis**: runtime schema validation / output classifiers / sandboxed evaluators do not appear in the graph and cannot be detected. This rule only **visualizes the structural attack surface**; it does not assert "actually exploitable". That is why we assign Confidence 0.9.

---

## 2. Detection Targets

### 2.1 Source (LLM nodes)

Any node with `NodeType.LLM` is treated as a source. Severity itself does not depend on Source-side characteristics — it depends on the gates along the path — so we do not narrow the Source set (a deliberate design choice that accepts over-detection).

### 2.2 Sink (code-execution Tool nodes)

Among `NodeType.Tool` nodes, those matching any of the following are treated as sinks:

| Decision path | Decision condition |
|---|---|
| Config["category"] | `code_execution` or `code_eval` |
| Config["tool"] | `eval` / `exec` / `code_interpreter` / `python_runner` / `shell` (case-insensitive) |
| Name / ID regex | partial match against `(?i)(eval\|exec\|code[_]?runner\|python[_]?runner\|shell\|bash)` |

This matches both snake_case (`code_runner` / `python_runner`) and PascalCase (`CodeRunner` / `PythonRunner`).

### 2.3 Gates Along the Path

The kind of node sandwiched on the path determines Severity:

| Strongest gate on path | Severity | Confidence | ConfidenceReason |
|---|---|---|---|
| Nothing in between (LLM → Tool direct, or only LLM/Tool between) | **Critical** | 0.9 | `heuristic_pattern` |
| `NodeType.Condition` node present | **Warning** | 0.6 | `heuristic_pattern` |
| `NodeType.Human` node present | **(skip)** | — | — |

"Condition only" is a weak signal that **the operator is aware of the need for explicit validation**, but automated code checks cannot perfectly reject every attack string (e.g. `eval("__import__('os').system(...)")` slips past a naive syntax check). We **lower Severity by one notch instead of fully invalidating** as the rule's design choice.

"Human gate present" means **the approver can visually inspect the code**, so the rule does not let the path materialize at all (same shape as the Human-gate rule of the PII leak rule).

---

## 3. Detection Algorithm

Unlike the PII leak rule (reverse-BFS), this rule uses **forward BFS**, for these reasons:

1. Severity **depends on the kind of gate along the path**, so it is more natural to keep a `viaCondition` flag in the frontier and propagate it forward (reverse-BFS would require reconstructing "did we pass a Condition?" from sink to source).
2. It matches the human reading "does this LLM reach eval?", making code reading easier.

```
Step 1: Sources(g) extracts LLM nodes (O(V))
Step 2: Sinks(g) extracts code_execution Tool nodes (O(V))
Step 3: Forward BFS from each source. frontier = {node, viaCondition bool}.
        - Next node is Human → stop expansion (drop path)
        - Next node is Condition → expand with viaCondition = true
        - Next node is Sink → emit Finding (Severity = viaCondition ? Warning : Critical)
Step 4: visited dedupes on the (node, viaCondition) pair.
        Re-reaching a node with viaCondition=true that was already visited at
        viaCondition=false does not downgrade the severity (the stronger path dominates).
```

**Complexity**: O(sources × (V+E)). In typical workflows sources << V, so effectively O(V+E).

---

## 4. Implementation Design Decisions

### 4.1 Why "Human gate is a boundary"

Same judgment as the PII leak rule: if a human visually approves the code on an approval screen, that is a stronger defensive line than automated eval. With Condition alone the validation logic itself cannot be statically checked, so we keep "insufficient but explicit" as Warning by lowering Severity by one notch.

### 4.2 Why Severity is decided by path-side gates instead of sink classification

In `prompt_injection_sink`, Severity is determined **at sink classification** (system_prompt + substitution → Critical). In eval_missing the sink itself is roughly constant in danger (RCE), and what really matters is which gates appear on the path. We therefore chose a design that holds path state in the frontier.

### 4.3 Why Source is taken broadly as "all LLMs"

There is no explicit flag like `Config["allow_code_exec"]`, and it is hard to claim that an LLM is intrinsically safe as a source (external input mixing in via RAG is normal). We **suppress FPs by narrowing on the Sink side** instead.

### 4.4 Why visited is keyed by `(node, viaCondition)`

A normal BFS dedupes by `node`, but here the frontier carries `viaCondition`, so **reaching the same node in a different state must be treated as a different path**. We add a dominance rule (when a Critical-eligible state already exists, ignore the downgrade route) so that results stay stable.

### 4.5 Building forward adjacency in Propagate

The PathContext's `Reverse` is reverse adjacency pre-built by path_walker. This rule is forward-flow, so it builds a `forward` map once on its own. The cost is O(E), the same as the reverse-construction cost in the PII leak rule. There is room for path_walker to grow a `Forward` field in the future (deferred under YAGNI).

---

## 5. Recommended Mitigation Patterns

Avoid architectures that pipe LLM output straight into eval / exec / Function. Replace with one of the following:

```python
# OK: structured tool-call schema (function calling) — arguments are type-checked structs
client.chat.completions.create(
    model="gpt-4o",
    messages=[...],
    tools=[
        {
            "type": "function",
            "function": {
                "name": "run_query",
                "parameters": {
                    "type": "object",
                    "properties": {"sql": {"type": "string"}},
                    "required": ["sql"],
                },
            },
        },
    ],
)
# Always run the returned sql through a parser before execution (allow-list / parameterized query).

# NG: feeding the LLM's free-text response straight into eval
result = eval(llm_response)  # ← this rule fires Critical
```

Concrete mitigations:

1. **Structured tool-call schema** — function calling / Tool Use API to structure arguments and forbid free strings
2. **Sandboxed evaluator** — isolate execution via `seccomp` / Docker / Firecracker / [Vercel Sandbox](https://vercel.com/docs/runtime/sandbox)
3. **Allow-list dispatch** — explicit table like `commands = {"sum": handler_sum, "diff": handler_diff}` mapping arguments; do not use `eval`
4. **Static AST validation** — `ast.parse` + AST visitor that allows only whitelisted node kinds
5. **Human-in-the-loop approval** — insert a human approval into the path for high-risk operations (DB drops, outbound calls)

---

## 6. Known False Positives / False Negatives

### False Positive

- **Eval through a runtime sandbox**: Even when LangGraph etc. invoke "Tool internal seccomp via fork-exec", the graph only shows a `code_execution`-categorized Tool, so Critical fires. Suppress with `--min-confidence 0.95`, or fall through by changing the Tool node's `category` to a custom value such as `sandboxed_code`.
- **Overestimating the Condition downgrade gate**: Even if the Condition body is "a stub that always returns true", Warning still fires. Conversely a "near-perfect validator in practice" still reads as the same Warning. Validity of the body is outside static analysis.

### False Negative

- **Eval without a Tool node**: A LangChain-style pattern that calls `exec(llm_out)` directly on the LLM output, written outside Tool nodes (e.g. straight in code), produces no sink node in the graph and is undetectable. Sibling rule `dynamic_node_construction` complements this at the string-value level for `Config["body"]` etc.
- **Delayed injection via Loops**: Patterns that insert LLM output into the next iteration's input inside a Loop, then have a separate Tool eval it. Forward BFS does propagate across the Loop's back edge, but the source must be visible in the subgraph for detection to succeed.
- **Custom Tool category names**: When `Config["category"]` carries unregistered keys like `runtime_eval` / `dynamic_exec`, sink detection misses them. Either share a naming convention (PR additions) or use names easy for the name regex to catch.

---

## 7. Related Rules / ADRs

- **ADR-006**: ESLint visitor pattern — contrast with Local rules. This rule is at the Path tier.
- **ADR-007**: Local / Path / Global three-tier separation — basis for this rule belonging to the Path tier.
- **ADR-008**: ConfidenceReason — uses `ReasonHeuristicPattern`.
- `prompt_injection_sink` (`docs/rules/prompt-injection-sink.md`) — sibling Path rule covering the structural sink user_input → LLM.
- `pii_leak_scanner` (`docs/pii-detection.md`) — the reverse-BFS template, source of the Human-gate rule.
- `dynamic_node_construction` (`docs/rules/dynamic-node-construction.md`) — Local rule that directly detects `eval(`/`exec(`/`Function(` at the Config-value level. This rule covers the structural surface, dynamic_node_construction covers the string-level surface — complementary.
