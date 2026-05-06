> 🌐 Language: **English** | [日本語](./prompt-injection-sink.ja.md)

# prompt_injection_sink — Design & Implementation Doc

> **Target version**: Phase 2 #1 (v0.6 series)
> **Implementation file**: `domain/rules/prompt_injection_sink.go`
> **Tests**: `domain/rules/prompt_injection_sink_test.go` (12 cases including subtests)
> **Tier (ADR-007)**: Path rule — implements Sources / Sinks / Propagate

---

## 1. Background & Motivation

The most significant vulnerability category in LLM agent design is **prompt injection**:

- When an attacker-controlled string is concatenated directly into the LLM's system prompt, the LLM interprets those "instructions" as system commands and the original policy is overridden.
- Result: **credential leakage**, **access to other tenants' DBs**, **execution of forbidden operations (tool abuse)**, and **jailbreak**.

This rule detects "user-input node → LLM system-prompt field" reachability from the **static structure** of the workflow graph, surfacing the issue at design time **before relying on runtime sanitization**.

> **Limits of static analysis**: Cases where string-level sanitization (escape / validate / classify) is performed at runtime cannot be detected. This rule only **visualizes the structural attack surface**; it does not assert "actually exploitable". That is why we assign Confidence 0.9.

---

## 2. Detection Targets

### 2.1 Source (user-input nodes)

A node is treated as a source if it matches one of:

| Decision condition | Example |
|---|---|
| `Config["source"] == "user_input"` | Any NodeType. The Config flag is the most explicit |
| Name / ID matches the regex `(?i)^(user[_\-].*\|.*[_\-]input\|query\|request\|user_query\|user_request)$` | `user_query`, `chat_input`, `query`, `request` |

NodeType does not matter. In implementation, any of Tool / LLM / Output etc. that matches the pattern becomes a source.

### 2.2 Sink (LLM prompt-template nodes)

Among `NodeType.LLM` nodes, those whose any of the following Config keys is non-empty become sinks:

| Key | Tier |
|---|---|
| `system_prompt` | system tier |
| `system` | system tier |
| `instruction` | system tier |
| `instructions` | system tier |
| `prompt_template` | user tier |
| `user_message_template` | user tier |
| `user_template` | user tier |
| `prompt` | user tier |

**Substitution detection**: a regex decides whether the value contains any template substitution syntax, namely `{{var}}` / `${var}` / `{var}`.

### 2.3 Severity

Severity is determined **at sink classification time** and does not depend on path-analysis information:

| Sink tier | substitution | Severity | Confidence | ConfidenceReason |
|---|---|---|---|---|
| system tier | yes | **Critical** | 0.9 | `heuristic_pattern` |
| system tier | no | **Warning** | 0.7 | `heuristic_pattern` |
| user tier | yes | **Info** | 0.5 | `heuristic_pattern` |
| user tier | no | (not a sink) | — | — |

We do not treat "user tier × no substitution" as a sink because a prompt field without a template is almost always a static string that cannot concatenate user input.

---

## 3. Detection Algorithm

Same shape as the PII leak rule (reverse-BFS at the Path tier of ADR-007):

```
Step 1: Sources(g) extracts user-input nodes (O(V))
Step 2: Sinks(g) extracts LLM template nodes; classifySink decides each sink's severity (O(V))
Step 3: Reverse-BFS from each sink. Reverse adjacency is shared via ctx.Reverse (the PathRule contract).
        On hitting a source node, emit one finding (using the severity decided at sink classification).
Step 4: Unlike the PII rule there is no Human-gate boundary. Any structural reachability = finding.
```

**Complexity**: O(sinks × (V+E)). In typical workflows sinks << V, so effectively O(V+E).

**Dedup**: BFS uses a `visited` set to prevent re-visiting, so the same (sink, source) pair only fires once. On the other hand, when multiple sources can reach the same sink, each fires as a separate finding (so users can see "which entry point is the risk").

---

## 4. Implementation Design Decisions

### 4.1 Why we do not make the Human gate a boundary

PII leak operates in a GDPR / CCPA business context where "human approval permits external transmission". For prompt injection, **danger does not change with approval** (we even need to imagine attacks that pass through the approval screen to plant their payload). Not treating `NodeTypeHuman` as a boundary is the safer default.

There is room to add an option in the future for treating an explicit `sanitizer`-category node as a boundary (deferred under YAGNI).

### 4.2 Why both source and sink detection are heuristic

Both rely on **name / Config-key pattern matching** with no taint propagation or other semantic analysis. To reflect that, we use `ConfidenceReason = ReasonHeuristicPattern` and keep Confidence in the 0.5–0.9 band.

This follows ADR-008's principle "every Finding carries a Reason". Users can suppress False Positives via `--min-confidence 0.8` and decide tuning direction (parser improvement / DSL extension / type annotation) by reading the Reason.

### 4.3 Why `system_prompt` / `system` / `instruction(s)` are unified as "system tier"

We covered the representative key names that signify the system role across LangChain / LlamaIndex / ADK-Go / Vercel AI SDK / Anthropic Messages API. `prompt_template` alone is ambiguous with user-message templates, so we put it in a separate tier.

### 4.4 The substitution regex

```
(\{\{[^}]+\}\}|\$\{[^}]+\}|\{[A-Za-z_][A-Za-z0-9_\.]*\})
```

- `{{var}}`: Mustache / Handlebars / LangChain family
- `${var}`: JS template literals / Vercel AI SDK family
- `{var}`: Python `str.format` / f-string flag (identifier only, conservative so as not to swallow JSON / math `{...}`)

Mixed templates of all three (`hi {{a}}, ${b}, {c}`) are also detected.

---

## 5. Recommended Mitigation Patterns

Use the LLM SDK's API that **separates `messages` into role:user / role:system**, so system stays a static string and the user portion lives in a different field. This enables anti-injection designs:

```python
# OK: user content and system instruction are different layers
client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[
        {"role": "system", "content": "You are ACME's support assistant."},
        {"role": "user",   "content": user_input},  # ← separate field
    ],
)

# NG: mixing user input into system via str.format
client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[
        {"role": "system", "content": f"You are ACME's support assistant. User said: {user_input}"},
    ],
)
```

Library-specific support examples:

| Library | Recommended API |
|---|---|
| Anthropic SDK | `messages=[{"role":"system",...},{"role":"user",...}]` (Messages API) |
| OpenAI SDK | same (chat.completions, with system / user messages separated) |
| LangChain | `ChatPromptTemplate.from_messages([("system","..."),("user","{user_query}")])` keeping input variables only on the user side |
| Vercel AI SDK | `streamText({ messages, system })` dedicates a `system` argument |
| ADK-Go | Hold `LlmAgent.Instructions` (system) and `Input` (user) in separate fields |

In addition, as defensive layers:

1. **Input length cap** — reject abnormally long input first (prompts of 10K–50K characters or more breed injection)
2. **Schema validation** — enforce type/shape via JSON schema or regex; reject anything other than free text early
3. **Allow-list output classification** — also filter dangerous outputs (tool-call arguments, SQL queries) at output time

---

## 6. Known False Positives / False Negatives

### False Positive

- **`prompt_template` has substitution but the LLM call separates roles in the template**: Static analysis cannot see the LLM SDK behavior, so it is reported at Info. That is why Confidence is 0.5. Suppress with `--min-confidence 0.8`.
- **Spurious name-pattern matches**: For instance `customer_request` ends in `request` and is intentionally classified as user-input. There can also be cases where it represents an "internally-generated RPC request" by business logic — rename it or set the source key explicitly to avoid the FP.

### False Negative

- **Dynamically generated nodes**: Patterns that splice in nodes via `conditional_edges` / runtime in LangGraph syntax are absent from the static graph and undetectable.
- **Multi-stage prompt-template concatenation**: When a string concatenation like `prompt = base + user_query` is fed directly into `system_prompt`, only the static string is visible in Config and detection fails (in this case other rules such as `secret_exposure_scanner` may indirectly catch it).
- **Non-standard key names**: `messages_template` / `instr` / custom schema keys are out of scope. The royal road is to read structured message arrays directly via a framework-specific parser (LangGraph / ADK-Go).

---

## 7. Related Rules / ADRs

- **ADR-006**: ESLint visitor pattern — contrast with Local rules. This rule is at the Path tier.
- **ADR-007**: Local / Path / Global three-tier separation — basis for this rule sitting at the Path tier.
- **ADR-008**: ConfidenceReason — uses `ReasonHeuristicPattern`.
- `pii_leak_scanner` (`docs/pii-detection.md`) — earlier implementation of the same Path rule, the reverse-BFS template.
- `secret_exposure_scanner` (`docs/secret-detection.md`) — Local rule, regex matching at the Config-value level.
