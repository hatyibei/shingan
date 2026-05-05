> 🌐 Language: **English** | [日本語](./pii-detection.ja.md)

# PII Leak Detection Rule (`pii_leak_scanner`) — Design and Implementation Doc

> **Target version**: v0.3 (v0.2 optimization complete)
> **Implementation file**: `domain/rules/pii_leak.go`

---

## 1. Background and Motivation

For enterprise AI agent systems, **unintended external leakage of PII (Personally Identifiable Information)** is the single largest compliance risk.

GDPR (EU), CCPA (California), and Japan's Act on the Protection of Personal Information all require **subject consent or a legitimate business basis** before transmitting PII to a third party. If leak paths can be detected at workflow design time, violation risk can be eliminated upfront.

**The top concern for SamuraiAI's enterprise customers**: a RAG (retrieval-augmented generation) pipeline that fetches PII from a customer database and forwards it directly to an external API or MCP server.

---

## 2. Detection Targets

### 2.1 PII Source Nodes

| Condition | Severity | Description |
|-----------|----------|-------------|
| `NodeTypeTool` + `category == "rag"` | Warning | RAG is highly likely to contain PII |
| `NodeTypeTool` + `Config["has_pii"] == true` | Warning | Explicit PII flag |
| Node name contains `pii` / `user` / `personal` / `private` | Info | Heuristic (false positives possible) |

### 2.2 External Sink Nodes

`NodeTypeTool` whose `category` is one of:

- `api` — External REST / GraphQL API
- `mcp` — Model Context Protocol server
- `browser` — Browser automation (form submission, etc.)

### 2.3 Human Approval Gate (Safety Boundary)

If a `NodeTypeHuman` node sits on the path from a PII source to a sink, that path is treated as safe and is not reported.
The rule assumes the Human node represents a privacy officer or approval workflow.

---

## 3. Detection Algorithm (v0.2 — Sink-rooted reverse BFS)

v0.2 optimization: replaced the legacy implementation that ran a forward BFS from each PII source (O(sources x V x E)) with **reverse BFS rooted at each Sink** (O(V+E)). At n=1000, runtime improved from 267 ms to 26 ms (10x speed-up).

```
Step 1: Build the reverse adjacency list in O(E)
    reverse[to] = list of edges pointing into "to"

Step 2: Classify nodes in O(V)
    humanSet    = { n | n.Type == NodeTypeHuman }
    ragSources  = { n | isRAGSource(n) or isPIIHintSource(n) }
    sinks       = { n | isExternalSink(n) }

Step 3: Reverse BFS from each Sink
    for each sink in sinks:
        BFS using reverse adjacency list:
            if predecessor == NodeTypeHuman:
                stop this branch (Human gate = safe)
            if predecessor in ragSources:
                emit Finding(severity=Warning/Info, node_id=sink, source=predecessor)
            else:
                continue BFS backwards
```

**Complexity**: O(V+E) overall. Reverse adjacency construction is O(E), node classification is O(V), and reverse BFS over all sinks totals O(sinks x (V+E)). In typical workflows sinks << V, so the practical bound is O(V+E).

### Finding correspondence

Each Finding corresponds to a `(sink_id, source_id)` pair. When a single sink is reverse-reachable from multiple PII sources, an individual Finding is emitted per source (each path is treated as an independent leak risk).

---

## 4. Severity Table

| Condition | Severity | Action |
|-----------|----------|--------|
| RAG source -> external Sink (no Human gate) | `Warning` | Insert Human approval node, or sanitize PII |
| `has_pii=true` source -> external Sink (no Human gate) | `Warning` | Same as above |
| Name-heuristic source -> external Sink | `Info` | Review required (false positive possible) |

---

## 5. Sample Workflows and Detection Results

### 5.1 Risk present (`testdata/pii/leak_risk.json`)

```
rag_search ──→ llm_summarize ──→ external_api
```

```json
{
  "rule": "pii_leak_scanner",
  "severity": "warning",
  "node_id": "external_api",
  "message": "potential PII leak: path from RAG/PII node \"rag_search\" (顧客情報RAG検索) to external tool \"external_api\" (category=\"api\") without Human approval gate",
  "suggestion": "ノード \"rag_search\" と \"external_api\" の間にHuman承認ノードを挿入するか、PIIフィールドをサニタイズしてください (GDPR/CCPA/個人情報保護法対応)"
}
```

### 5.2 Safe (`testdata/pii/safe.json`)

```
rag_search → llm_summarize → human_approval → external_api
```

Findings: none (the Human gate protects the path).

---

## 6. Registration in AnalyzerFactory

`pii_leak_scanner` was added in v0.3 as the seventh rule.

```go
// infrastructure/factory/analyzer.go
case "pii_leak_scanner":
    return rules.NewPIILeakScanner(), nil
```

The seven rules returned by `CreateAll()`:

1. `cycle_detection`
2. `unreachable_node`
3. `error_handler_checker`
4. `cost_estimation`
5. `redundant_llm_call`
6. `loop_guard`
7. `pii_leak_scanner` ← new

---

## 7. Planned Extensions for v0.3

| Feature | Priority | Summary |
|---------|----------|---------|
| PII content inspection | High | Detect PII via regex patterns (email, phone number, My Number, etc.) inside node Config / Prompt |
| `encrypt_in_transit` flag | Medium | Lower the severity when transport encryption is configured |
| SARIF output integration | Medium | Emit PII risks in GitHub Advanced Security's SARIF format |
| Data-flow tagging | Low | Propagate per-node PII tags for more precise path analysis |
| Regex-based detection | Low | Search for patterns like `\d{3}-\d{4}-\d{4}` (phone number) inside `PromptTemplate` |

---

## 8. Interview Talking Points

**SamuraiAI impact statement (≤ 50 chars JA)**:
> RAGパイプラインのPII漏洩を設計時に静的検出し、GDPR違反ゼロを保証

**Technical highlights**:
- Onion Architecture compliant: `domain/rules/` has no external dependencies (stdlib + domain packages only)
- **v0.2 optimization**: sink-rooted reverse BFS plus pre-built reverse adjacency list achieves O(V+E). At n=1000, 275 ms -> 26 ms (10x improvement)
- Human-in-the-loop pattern is auto-recognized as a "gate"
- Three-layer detection — explicit flag (`has_pii`) + category inference + name heuristic — minimizes false negatives

**Competitive differentiation**:
Traditional static-analysis tools (semgrep etc.) stop at code-level pattern matching, but Shingan embeds **workflow semantics** (the domain knowledge that RAG implies PII) into rules, achieving higher-precision detection.
