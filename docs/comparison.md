> 🌐 Language: **English** | [日本語](./comparison.ja.md)

# Shingan vs competing tools

Shingan's competitive category is "static analysis of workflows / code". We compare it within the AI-agent-specific space.

---

## Overall comparison

| Tool | Target | Category | Analysis timing | AI agent support | Maturity |
|---|---|---|---|---|---|
| **Shingan** | **AI Agent Workflow** (ADK-Go / JSON / SamuraiAI assumed) | Static analysis | **Design time** | **Full support** | v0.5.0 (2026-04) |
| FlowLint | n8n | Static analysis | Design time | × (traditional automation only) | v0.3.7 |
| AI-BOM | n8n | Security audit | Design time | × (API key leakage only) | Early |
| LangSmith | LangGraph | Runtime observability | **Runtime** | ○ | Production |
| Systems Inspector | LangGraph | Runtime testing | **Runtime** | ○ | Prototype |
| Semgrep | **General code** | Static analysis | Design time | × (no AI-agent structural awareness) | Production |
| Snyk Code | General code | Security | Design time | × | Production |
| ESLint | JavaScript | Static analysis | Design time | × | Production |

**Conclusion**: As of April 2026, Shingan is effectively the only player in the category of **static analysis** specific to AI agent workflows.

---

## Feature comparison matrix

### Issues that can be detected

| Issue category | Shingan | FlowLint | LangSmith | Semgrep |
|---|---|---|---|---|
| Infinite loop (design time) | ✓ loop_guard | ✓ (retry-loop) | runtime only | × |
| Unreachable node | ✓ unreachable_node | ✓ dead-end | × | × |
| Missing error handling | ✓ error_handler_checker | × | × | × |
| **LLM cost explosion** | ✓ cost_estimation | × | × (after-the-fact detection) | × |
| **Redundant LLM calls** | ✓ redundant_llm_call | × | × | × |
| **PII leakage paths** | ✓ pii_leak_scanner | × | × | limited |
| **Embedded secrets** | ✓ secret_exposure_scanner | × (n8n credentials) | × | ✓ |
| **Excessive parallelism** | ✓ max_parallel_branches | × | × | × |
| **Deprecated models** | ✓ deprecated_model | × | × | × |
| Non-control cycles | ✓ cycle_detection | × | × | × |

**Shingan-only rules**: 7 (covering AI-agent-specific domains)

---

## Design philosophy comparison

### FlowLint
- **Target**: n8n workflow definitions (JSON)
- **Philosophy**: Detect code-quality issues in existing "workflow automation (Zapier-style)"
- **Gap**: Does not cover AI-agent-specific issues (LLM cost, inference loops, prompt design)

### LangSmith
- **Target**: Traces from LangChain/LangGraph runtime
- **Philosophy**: Accumulate and visualize execution logs for debugging and optimization
- **Gap**: Detects only **after** execution. Does not act as a design-time guard.
- **Complementary relationship**: Shingan (design time) + LangSmith (runtime) covers both wheels

### Semgrep
- **Target**: General code in Go / Python / JS, etc.
- **Philosophy**: Bug detection via AST pattern matching
- **Gap**: Does not understand the "workflow graph structure" of AI agents. Dependencies between LlmAgents and fan-out are not in scope.

### Shingan
- **Target**: AI agent workflows (normalized into a WorkflowGraph)
- **Philosophy**: A dedicated analyzer that understands workflow-specific structure
- **Differentiator**: With Onion Architecture, the domain (rules) is framework-independent → adding adapters alone enables multi-framework support

---

## Detailed comparison with FlowLint

Both can analyze n8n-style structures, but their target areas differ.

| Item | Shingan | FlowLint |
|---|---|---|
| Primary target | AI agents (LlmAgent, Tool, LoopAgent) | n8n (Trigger, HTTP, Set, IF) |
| Generality | ADK-Go / JSON / SamuraiAI; extend by adding adapters | n8n only |
| Number of analysis rules | 10 | ~5 (retry-loop, dead-end, secrets) |
| LLM cost judgment | ✓ cost_estimation (model price tiers) | × |
| Confidence score | ✓ (0-1.0, --min-confidence) | × |
| Output formats | JSON / Markdown / **SARIF** | JSON |
| GitHub Code Scanning | ✓ integrated via SARIF | △ |
| Core data model | Language-neutral WorkflowGraph | n8n-schema-specific |

**Shingan can be positioned as the "AI-agent-specialized + multi-framework version of FlowLint".**

---

## Complementary relationship with LangSmith

Shingan (design time) and LangSmith (runtime) are not competitors — they **complement** each other.

```
┌─────────────────────────────────┐
│  Development: Shingan static    │
│  analysis                       │
│  - 10 rules, FP rate managed    │
│  - Pre-block in CI PR           │
└──────────────┬──────────────────┘
               │ before deploy
               ▼
┌─────────────────────────────────┐
│  Runtime: LangSmith Trace       │
│  - Runtime observability, A/B   │
│  - Debugging, real cost         │
└─────────────────────────────────┘
```

**Why both wheels are needed**:
- Shingan covers "structural bugs" (cycle, unreachable)
- LangSmith covers "non-deterministic behavior" (LLM hallucinations, prompt quality)

---

## Goal toward v1.0: Multi-framework static analyzer

Coverage at v0.5: ADK-Go (native), JSON (native), SamuraiAI assumed (Alpha)

v0.6: n8n parser (Issue #4) → subsumes FlowLint's features
v0.7: LangGraph (via Python AST)
v0.8: Dify
v1.0: CrewAI, AutoGen support; multi-framework stable release

At that point, Shingan becomes the "**one and only** cross-workflow-engine static analysis platform".

---

## Conclusion

**Shingan's positioning** (2026-04):
- An **early player** in the static analysis category for AI agent workflows
- Broader than FlowLint (n8n-only), earlier than LangSmith (runtime)
- 10 proprietary rules, Onion Architecture, confidence scoring built in
- CI integration (SARIF), middleware injection, and Vertex AI integration already implemented

Just as ESLint launched in 2013 and became the standard for JavaScript static analysis, Shingan launches in 2026 aiming to become the standard for AI agent static analysis.
