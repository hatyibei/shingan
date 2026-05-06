> 🌐 Language: **English** | [日本語](./dynamic-node-construction.ja.md)

# dynamic_node_construction — Design & Implementation Doc

> **Target version**: Phase 2 #3 (v0.6 series)
> **Implementation file**: `domain/rules/dynamic_node_construction.go`
> **Tests**: `domain/rules/dynamic_node_construction_test.go` (16 cases including subtests)
> **Tier (ADR-007)**: Local rule — `OnAny` listener + recursive Config scan

---

## 1. Background & Motivation

In node-based DSLs such as LangGraph or ADK-Go, the "body" of a node is sometimes **declared as a string**:

```python
# Typical anti-pattern in LangGraph
graph.add_node("dispatcher", lambda payload: eval(payload['code']))
```

A body like `lambda x: eval(x)` looks like an opaque string from the outside, but is executed at runtime, so:

- **Static analysis cannot peek inside** — what `eval` runs is not visible at the graph level
- **Combined with prompt injection / RAG poisoning, it becomes RCE** — if `payload['code']` is attacker-controlled, arbitrary code execution is possible
- **It bypasses rules like secret_exposure_scanner / pii_leak_scanner** — the attack surface only manifests dynamically

This rule recursively scans string values inside `Node.Config` and surfaces the **mere existence** of `eval(`/`exec(`/`Function(`/`compile(`/`__import__(`/`getattr(`/`setattr(` as findings, partitioned by Severity.

> **Positioning**: while sibling rule `eval_missing` looks at "structural reachability from an LLM node into a code-execution Tool", this rule looks at "string literals inside Config values". They are complementary — even if no structural sink exists, this rule catches a literal `eval(` written into Config; even if Config has nothing, eval_missing catches an LLM that flows into a code_execution Tool.

---

## 2. Detection Targets

### 2.1 Scanned Config Keys

Only the following curated set is scanned:

| Key | Example use |
|---|---|
| `body` | LangGraph `add_node(name, body)` body string |
| `fn` | function spec (FastAPI / RPC) |
| `handler` | handler function definition |
| `callback` | callback code |
| `code` | code spec, code-interpreter argument |
| `factory` | factory function string |
| `builder` | builder function string |

Free-text parameters such as `description` / `model` / `prompt_template` are **deliberately excluded**. Even if a description says "Wraps eval() calls safely", the rule does not fire (a design decision to avoid flagging documentation strings).

### 2.2 Detection Patterns

| Pattern | Regex | Severity | Confidence | ConfidenceReason |
|---|---|---|---|---|
| `eval(` | `\beval\s*\(` | **Critical** | 0.95 | `exact_static_match` |
| `exec(` | `\bexec\s*\(` | **Critical** | 0.95 | `exact_static_match` |
| `Function(` | `\bFunction\s*\(` | **Critical** | 0.95 | `exact_static_match` |
| `compile(` | `\bcompile\s*\(` | **Warning** | 0.85 | `exact_static_match` |
| `__import__(` | `__import__\s*\(` | **Warning** | 0.85 | `exact_static_match` |
| `getattr(` | `\bgetattr\s*\(` | **Info** | 0.6 | `heuristic_pattern` |
| `setattr(` | `\bsetattr\s*\(` | **Info** | 0.6 | `heuristic_pattern` |

`\s*` between the function name and `(` allows whitespace (`eval ( payload )` is also detected).

### 2.3 Severity collapsing

If a single Config value matches multiple patterns (e.g. `getattr(obj, 'cmd')(eval(payload))`), the rule **collapses to one finding at the highest Severity**. `getattr` Info + `eval` Critical → only one Critical finding. This communicates "compound danger" without inflating the finding count and clouding judgment.

### 2.4 Placeholder strip-then-recheck

Same shape as `secret_exposure_scanner.hasActualSecret`:

- **Placeholder only** (`${EVAL_FN}` / `{{handler}}` / `process.env.X` / `os.Getenv(...)`): skip (these are runtime-injected references, not real code)
- **Mixed** (`eval(${PAYLOAD})`): fires — even after stripping the placeholder, `eval(` remains, so the strip-then-recheck survives

This policy suppresses false positives while still catching attack surfaces **hidden behind placeholders** like `eval(${PAYLOAD})`.

---

## 3. Detection Algorithm

```
Step 1: Each Node fires the OnAny listener (1walk dispatcher)
Step 2: Walk Node.Config
        - Key not in dynamicScanKeys → skip
        - Otherwise → recursive scan (deep traversal of strings / maps / slices)
Step 3: Apply collectStringHits to each leaf string
        - Empty string → skip
        - placeholderPattern.MatchString && !hasActualDynamicPattern → skip
          (placeholder only, no real code)
        - Match each dynamicPatterns entry sequentially via regex; append hits
Step 4: If hits is non-empty, pick the highest-Severity hit and emit one Finding
        (at most 1 per key)
```

**Complexity**: O(V × cfg) — V = node count, cfg = number of Config values (recursion depth included). Same order as secret_exposure_scanner.

---

## 4. Implementation Design Decisions

### 4.1 Why we narrow scanned keys to a curated set

A full scan would pick up references to `eval()` inside free text such as `description` / `system_prompt` / `instruction`. For example, a documentation string like "This tool wraps the python `eval()` function safely." technically matches the `eval(` regex but is not an attack surface. We **deliberately narrow** the scope to favor FP suppression.

When more keys are needed, a one-line patch adds entries to the `dynamicScanKeys` map.

### 4.2 Why Severity collapsing reduces to 1 finding per key

For an attack string like `getattr(obj, 'cmd')(eval(payload))(__import__('os'))`, emitting 3 findings (Critical + Warning + Info) tends to be misread as "multiple alerts = separate problems". Folding to **one value = one finding (highest Severity)** aligns with the perception "there is one nasty construct here".

When `eval` appears in different Config keys, separate findings still fire for `body` and `handler` (validated by Case 14).

### 4.3 Why we share placeholder handling with secret_exposure

`hasActualDynamicPattern` reuses `secret_exposure_scanner.placeholderPattern`. This guarantees consistent skipping of safe references such as `${VAR}` / `{{var}}` / `process.env.X` / `os.Getenv(` across both rules. Changing one affects the other, so we judged that **maintainability outweighs coupling concerns** for this incidental consistency.

### 4.4 Why we do not narrow NodeType (`OnAny`)

Tool nodes are the main target, but lambda bodies can hide inside an LLM node's `prompt_template` (legacy LangChain API), in post-processing scripts of an Output node, or in a Human node's review function. Each example uses a different type. **OnAny walks all kinds and the scanned keys filter from there** — this gives better portability.

---

## 5. Recommended Mitigation Patterns

```python
# NG: lambda x: eval(x)  ← this rule fires Critical
def dispatch_via_eval(payload):
    return eval(payload['code'])

# OK: explicit dispatch table
HANDLERS = {
    "sum":  lambda args: args['a'] + args['b'],
    "diff": lambda args: args['a'] - args['b'],
}
def dispatch_via_table(payload):
    op = payload['op']
    if op not in HANDLERS:
        raise ValueError(f"unknown op {op}")
    return HANDLERS[op](payload['args'])

# OK alternative: schema-validated function calling (OpenAI / Anthropic Tool Use)
# The LLM may call only the named functions enumerated in the tools array. eval is not involved.

# OK last resort: sandboxed evaluator
# - PyPy sandbox / WASI / Vercel Sandbox / Firecracker microVM
# - allow-list imports + memory cap + CPU cap
# Static analysis still sees the sink so Critical fires; on the operations side, introduce a custom
# category (e.g. "sandboxed_code") that explicitly marks "sandboxed" and allow-list it.
```

Library-specific mitigations:

| Library | Recommended API |
|---|---|
| LangChain | Pass an **already-imported Python callable** to `RunnableLambda`. Do not use string eval. |
| LangGraph | `add_node(name, callable)` where `callable` is a statically-imported function. We recommend a CI rule that prohibits `lambda x: eval(...)` as the argument to `add_node` itself. |
| OpenAI / Anthropic | Tool Use / function calling — schema-typed arguments. Allow-list dispatch via `tools=[...]`. |
| ADK-Go | Pass `agent.RunnerFn` as a Go function value directly (no string assignment needed). |

---

## 6. Known False Positives / False Negatives

### False Positive

- **Comments mentioning eval**: `description` is excluded so it never fires, but if a comment such as `// example: eval(x) is dangerous` is mixed into a scanned key like `body`, the current regex inspects it and fires. It comes out at Confidence 0.95, so it cannot be suppressed via `--min-confidence 1.0` (reserved for absolutes in the future). There is room to add comment-stripping (deferred under YAGNI).
- **Regex literals**: A string like Go's `regexp.MustCompile("eval\\(...\\)")` written into `body` etc. fires. As a workflow convention, it is best to use a separate key like `regex_pattern`.

### False Negative

- **Indirect eval calls**: Patterns like `func = "eval"; getattr(builtins, func)(...)` that **pass the name as a string** do not match the literal `eval(` and cannot be detected. `getattr` fires at Info, so a human still has to follow the chain.
- **Encoded eval**: Eval hidden via `bytes.fromhex('6576616c28...')` (hex / base64) is undetectable. There is room to add an entropy heuristic, similar to secret_exposure_scanner.
- **Via C extensions**: Cython or native eval-equivalents (`ctypes.cdll.eval(...)`) are undetectable.
- **Outside the scanned keys**: Eval written directly into free-text keys like `prompt_template` / `model` / `description` is intentionally not detected (see Section 4.1).

---

## 7. Related Rules / ADRs

- **ADR-006**: ESLint visitor pattern — this rule is a typical Local-tier example using `OnAny`.
- **ADR-007**: Local / Path / Global three-tier separation — this rule sits at the Local tier because the decision is at the Config-value level.
- **ADR-008**: ConfidenceReason — Critical / Warning use `ReasonExactStaticMatch` (regex full match), Info uses `ReasonHeuristicPattern` (dynamic-attribute access is an indirect signal).
- `eval_missing` (`docs/rules/eval-missing.md`) — sibling Path rule covering structural attack surfaces (LLM → code_execution Tool reachability). This rule covers the string-level surface, eval_missing covers the structural surface — complementary.
- `secret_exposure_scanner` (`docs/secret-detection.md`) — same Local + recursive-scan + placeholder strip-then-recheck shape. Shares `placeholderPattern`.
