> 🌐 Language: **English** | [日本語](./missing-eval-dataset.ja.md)

# missing_eval_dataset — Design & Implementation Doc

> **Target version**: Phase 2 #6 (v0.6 series)
> **Implementation file**: `domain/rules/missing_eval_dataset.go`
> **Tests**: `domain/rules/missing_eval_dataset_test.go` (19 cases)
> **Tier (ADR-007)**: Local rule — `OnGraph` aggregation, decided per graph

---

## 1. Background & Motivation

When an AI agent workflow is deployed to production / staging without an **eval dataset / regression test set**, model updates can silently change behavior:

- LLM providers silently roll over a model version (e.g. minor `gpt-4o` updates)
- Tweaks to prompts skew classification results
- Tool output formats change and downstream parsers fail

These can be **caught at PR time by running the eval suite**, but if the workflow definition has no eval-dataset reference, the regression typically only surfaces as post-deployment support tickets / customer complaints.

This rule emits a **Warning when there is a deploy signal but no eval reference anywhere in the graph**, encouraging users to wire eval into CI.

---

## 2. Detection Targets

### 2.1 Detecting Deploy Signals

We walk `g.Nodes`. If any node has one of these, there is a deploy signal:

| Config key | Value |
|---|---|
| `deployment` | `bool true` |
| `deploy` | `bool true` |
| `env` | `string` matching `prod`/`production`/`staging`/`stg` (case-insensitive, trimmed) |
| `environment` | same as above |

The set of `env` values is hard-coded in the `deployEnvValues` map of `domain/rules/missing_eval_dataset.go`. To add aliases (e.g. `pre-prod`), update that map.

### 2.2 Detecting Eval Signals

Likewise, walk `g.Nodes`. If any node has one of these, there is an eval signal:

| Config key | Accepted value type |
|---|---|
| `eval_dataset` | string (non-empty) / map / array |
| `test_set` | same |
| `benchmark` | same |
| `eval` | same |
| `evals` | same |
| `test_dataset` | same |
| `regression_set` | same |

For strings, after `strings.TrimSpace` the value must be non-empty (whitespace-only is treated as missing — ADR-008's "do not silently pass typos" principle).

### 2.3 Decision Logic

```
deploy signal:  ANY node satisfies the conditions above? → has_deploy
eval signal:    ANY node satisfies the conditions above? → has_eval

if has_deploy && !has_eval:
    Warning (Confidence 0.7, heuristic_pattern)
else:
    silent
```

- pre-prod (anything other than `env=dev` / `env=staging`) → silent
- deploy present + eval present → silent (the ideal case)
- **At most 1 finding per graph**. With multiple deploy flags, still 1 finding.
- The NodeID points at **the first node carrying a deploy signal** (map traversal order is non-deterministic, but the `len(findings) == 1` guarantee is preserved)

---

## 3. Implementation Design Decisions

### 3.1 Why we registered this at the Local tier

`OnGraph` decides once and per-node visits are unnecessary. The complexity does not warrant the Path / Global tier. This is a simplified version of the "OnNode aggregate → OnGraph emit" pattern used by `redundant_llm_call` (`redundant.go`).

### 3.2 The "ANY node has deploy / eval" design

Workflow authors place metadata in different ways:

- **pattern A**: aggregate `deployment=true` on the orchestrator node
- **pattern B**: scatter `env=prod` across leaf or per-step nodes
- **pattern C**: keep `eval_dataset` on a separate `evaluator` node

ANY-node existence checks work across all of these. NodeID merely points at the deploy-side node to support detection.

### 3.3 deploy=false / absent is silent

We stay silent even when `deployment=false` is explicitly written (an environment toggle). The reason is FP suppression — there are many flows that keep `deployment=false` as a development-time mock.

### 3.4 Rationale for Confidence 0.7

Both deploy detection (env string match) and eval detection (key-name match) are **naming heuristics**, not schema-bound contracts:

- Non-canonical values like `env=prod-eu` produce false negatives (they require extending `deployEnvValues`)
- Graphs that store `eval_dataset` under a custom key like `test_data` are false negatives
- The premise itself — "production deploy = eval is mandatory" — depends on engineering culture

This falls in ADR-008's 0.5–0.7 band, "stronger heuristic".

---

## 4. False Positive / False Negative

### False Positive

- **Eval managed in a separate repository**: A practice where the workflow JSON has no eval reference but a CI step runs eval externally counts as a false positive. Workaround: place a token string like `Config["eval_dataset"] = "ci/eval-suite"` in the workflow JSON, or exclude this rule via `--rules ...`.
- **Canary deploy**: A/B splits deploying to a limited cohort essentially deploy before fully running eval. Severity stays at Warning, so it does not raise the exit code (CI stays green).

### False Negative

- **`env=production-eu`**: Does not exact-match `deployEnvValues` and is skipped. Whether to extend aliases or split into a separate rule (`production_env_pattern_check`) is a Phase 3 consideration.
- **Dynamic deploy flags**: Code like `if env == "prod": config["deployment"] = True` decided at runtime. The `true` value never appears in Config, so detection fails. Framework-specific parsers (LangGraph / ADK-Go) need AST-level tracking.
- **Non-canonical eval keys**: Custom names like `Config["test_data"]` / `Config["benchmark_path"]` are out of scope. PRs to extend `evalDatasetKeys` are welcome.

---

## 5. Related ADRs

- **ADR-006**: ESLint visitor pattern — uses the OnGraph dispatcher of Local rules.
- **ADR-007**: Local / Path / Global three-tier separation — this rule is Local tier (graph-wide aggregation via OnGraph).
- **ADR-008**: ConfidenceReason — uses `ReasonHeuristicPattern`.
- **ADR-010**: Plugin SDK is internal-only (until v1.0) — this rule is also registered via `init()` calling `registerBuiltin()`.
