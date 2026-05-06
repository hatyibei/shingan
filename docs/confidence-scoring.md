> 🌐 Language: **English** | [日本語](./confidence-scoring.ja.md)

# Confidence Scoring (v0.4)

Shingan's Confidence score is a 0.0–1.0 indicator of how likely each Finding is a true positive.

## Design Philosophy: Two Axes — Severity and Confidence

```
              Severity
              ↑
 Critical     │  loop_guard (1.0)   ← deterministic and severe
              │  error_handler (0.8)
 Warning      │  redundant (0.9)    ← high confidence but minor
              │  pii_leak RAG (0.6)
 Info         │  cost (0.7)         ← heuristic
              └──────────────────→ Confidence
                 0.0   0.5   1.0
```

- **Severity**: impact when the issue manifests (Info / Warning / Critical)
- **Confidence**: probability that this alert is an actual problem

Separating these two axes makes it possible to clearly distinguish "severe but uncertain warnings" from "minor but certain notifications".

## Confidence Rationale Per Rule

| Rule ID | Confidence | Rationale |
|---------|-----------|-----------|
| `cycle_detection` | **1.0** | DFS back-edge detection is mathematically deterministic. If a cycle exists, it will always be detected. |
| `loop_guard` | **1.0** | Checking for the presence of `Config["max_iterations"]` is deterministic. |
| `unreachable_node` | **1.0** | BFS reachability is deterministic. Nodes that cannot be traversed from the entry are always correctly detected. |
| `error_handler_checker` | **0.8** | Improved by 2-hop Condition node check + `reliable` flag, but patterns 3+ hops away are not yet detected. |
| `redundant_llm_call` | **0.9** | Exact match on `prompt_template` is strong evidence. Skipping nil/empty strings eliminates false positives. |
| `cost_estimation` | **0.7** | Model price tiers (High/Mid/Low) are roadmap-dependent. Subject to change due to model renames or provider discounts. |
| `pii_leak_scanner` | **0.6** (RAG/has_pii) | `category=rag` or `has_pii=true` are explicit flags. High accuracy. |
| `pii_leak_scanner` | **0.3** (name hint) | Including "user"/"pii"/"personal" in a node name alone is weak evidence. Depends on naming conventions. |
| `secret_exposure_scanner` | **0.95** (Critical/Warning) | AWS AKIA prefix, sk-ant-, OpenAI sk-, etc. are highly specific patterns. |
| `secret_exposure_scanner` | **0.5** (Info) | Generic patterns like `password=XXX` / JWT have a high false-positive rate. |

## CI Integration Example: Block Only Critical with Confidence >= 0.9

```yaml
# .github/workflows/shingan.yml
- name: Shingan Static Analysis
  run: |
    shingan analyze \
      --format json \
      --input workflow.json \
      --output sarif \
      --output-file results.sarif \
      --min-confidence 0.9
  # exit code 2 = Critical findings with confidence >= 0.9
  # exit code 1 = Warning only → allow merge
  # exit code 0 = clean

- name: Upload SARIF
  uses: github/codeql-action/upload-sarif@v3
  with:
    sarif_file: results.sarif
```

In this workflow:
- `cycle_detection` (1.0), `loop_guard` (1.0), `unreachable_node` (1.0), `redundant_llm_call` (0.9) → blocked
- `error_handler_checker` (0.8), `cost_estimation` (0.7), `pii_leak_scanner` (0.3–0.6) → allowed (review recommended)

## JSON Output Sample

```json
{
  "findings": [
    {
      "rule": "cycle_detection",
      "severity": "critical",
      "node_id": "loop_ctrl",
      "confidence": 1.0,
      "message": "Loop node \"loop_ctrl\" has a cycle but max_iterations is not set"
    },
    {
      "rule": "pii_leak_scanner",
      "severity": "warning",
      "node_id": "api_sink",
      "confidence": 0.3,
      "message": "potential PII leak: path from RAG/PII node..."
    }
  ],
  "summary": {
    "total": 6,
    "critical": 3,
    "warning": 2,
    "info": 1,
    "high_confidence_count": 3
  }
}
```

## SARIF Output (GitHub Code Scanning)

- `result.properties.confidence`: confidence of each detection (float)
- `rule.properties.precision`: rule precision label
  - `"high"`: Confidence >= 0.9
  - `"medium"`: 0.6 <= Confidence < 0.9
  - `"low"`: Confidence < 0.6

## v0.5 Plan: Dynamic Confidence Adjustment via Machine Learning

- Collect feedback (true positive / false positive) from real codebases
- Measure per-rule Precision-Recall and update Confidence dynamically
- For `cost_estimation`, integrate with LLM provider pricing APIs for real-time updates
