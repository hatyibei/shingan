> 🌐 Language: **English** | [日本語](./retry-storm.ja.md)

# retry_storm — Design & Implementation Doc

> **Target version**: Phase 2 (v0.6 series, reliability batch)
> **Implementation file**: `domain/rules/retry_storm.go`
> **Tests**: `domain/rules/retry_storm_test.go` (14 cases)
> **Tier (ADR-007)**: Path rule — implements Sources / Sinks / Propagate (Sinks is nil)

---

## 1. Background & Motivation

Naive use of retry creates **exponential load amplification** the moment an external API has a transient failure:

- A Tool node with `retries=5` issues 1 request → on failure, hits the upstream 5 more times (retry coefficient 5)
- If **20 concurrent invocations** funnel into that node, it spikes to a momentary `100 requests`
- That breaches upstream API rate limits, causing IP block / token-budget exhaustion / cascading failure

`retry_storm` **detects this statically from the graph in advance**. It binds the product of "retry coefficient" and "parallelism" — **blast radius** — to Severity once it crosses thresholds.

> **Limits of static analysis**: Mitigation effects of backoff_factor / circuit-breaker / shared rate limiter etc. rarely show up on the graph. That is why we keep Confidence in the heuristic band (Warning / Info at 0.7 / 0.5). Critical (blast >= 100) gets exact_static_match because the numeric computation is deterministic.

---

## 2. Detection Targets

### 2.1 Source (retry-storm candidate nodes)

Among `NodeType.Tool` nodes, any node where one of the following Config keys is **>= 3**:

| Config key | Purpose |
|---|---|
| `retries` | Generic retry count (LangChain / LlamaIndex) |
| `max_retries` | Idiomatic for requests / boto3 family |
| `retry_count` | tenacity / Polly family |

Anything below 3 is treated as "ordinary transient-failure handling" and does not become a source (FP suppression).

When multiple keys are present simultaneously, **the first matching (>=3) value** is used (priority follows array order).

### 2.2 Parallelism Estimation (max of 3 signals)

Parallelism is **the max of these three independent upper bounds**:

1. **fan-in count** — number of incoming edges (concurrent upstream calls flowing in)
2. **the source's own `max_concurrency`** — the parallelism cap declared on the Tool itself
3. **upstream 1-hop signals** — for the `From` node of each incoming edge:
   - `NodeTypeLoop` carrying `max_iterations` → that value
   - carrying `max_concurrency` → that value

The minimum is 1 (an isolated source is still evaluated as `retries × 1`).

> **Design choice**: We "max the three" to see the **worst-case storm**. Real runtime parallelism is often below the max, but retry_storm is a rule that visualizes "**the worst-case scenario that could occur**", not one that targets measured values.

### 2.3 Severity / Confidence Matrix

Compute `blast = retries × parallelism` and fire on these thresholds:

| blast radius | Severity | Confidence | ConfidenceReason | Note |
|---|---|---|---|---|
| >= 100 | **Critical** | 0.9 | `exact_static_match` | Determined by numeric product |
| >= 30 | **Warning** | 0.7 | `heuristic_pattern` | parallelism estimate is conservative |
| >= 10 | **Info** | 0.5 | `heuristic_pattern` | Reference level, suppressible via `--min-confidence 0.7` |
| < 10 | (does not fire) | — | — | retry × parallelism is small-scale |

---

## 3. Detection Algorithm

```
Step 1: Sources(g) — walk Tool nodes and collect those with retryCount() >= 3 (O(V))
Step 2: Sinks(g) is nil. retry_storm is a per-source evaluation that does not need path analysis.
Step 3: Propagate(ctx) — for each source, compute estimateParallelism(graph, src):
        - count fan-in
        - read src's own max_concurrency
        - look at each incoming edge's From node 1 hop, reading Loop.max_iterations / max_concurrency
        - take the max of the 3 signals (minimum 1)
Step 4: Compute blast = retries × parallelism and pick Severity from the threshold matrix.
Step 5: Emit Finding.
```

**Complexity**: O(sources × E). In typical workflows sources << V, so effectively O(V+E).

---

## 4. Implementation Design Decisions

### 4.1 Why this lives at the Path tier

A Local rule (one node + Config only) cannot touch incoming edges. retry_storm reads upstream 1-hop info, which is impossible at Local and hard to express via the ESLint listener API. Building a whole-graph DFS at Global is overkill.

→ The Path tier is optimal for "look at adjacency around a source". Sinks are unused, but the `PathRule` interface allows Sinks to be nil — same shape as CostAnalyzer.

### 4.2 Why `retries=2` is not a source

In production workflows, `retries=1` ~ `retries=2` is heavily used to "absorb a single transient failure". That is a safe usage and excluding it from retry_storm warnings reduces FPs directly. Threshold 3 is the realistic floor given the LangChain / OpenAI Python SDK default of 3.

### 4.3 Why we max the parallelism signals

The 3 signals are "independent upper bounds". Real runtime works near the **min** of each. But what static analysis wants to expose is "**the worst case**" (worst-case blast), so we take the max:
- It suppresses false negatives (no signal is missed)
- ConfidenceReason / Severity explains FPs (`heuristic_pattern` Warning / Info)

### 4.4 Why Critical alone is `exact_static_match` and Warning/Info are `heuristic_pattern`

A blast >= 100 is large enough to **manifest as a storm under any mitigation**. By contrast, the 30 ~ 99 band is plausibly absorbed by backoff / circuit breaker, so we treat it as heuristic. The Confidence ladder (0.9 / 0.7 / 0.5) aligns with ADR-008's recommended values.

---

## 5. Recommended Mitigation Patterns

```python
# NG: naive retry + high parallelism
@retry(stop=stop_after_attempt(5))  # retries=5
def call_external_api(arg):
    ...
# Called from upstream ParallelAgent (max_concurrency=20) → blast 100

# OK: exponential backoff
@retry(stop=stop_after_attempt(5), wait=wait_exponential(multiplier=2))
def call_external_api(arg):
    ...

# OK: circuit breaker
breaker = CircuitBreaker(fail_max=3, reset_timeout=60)
@breaker
@retry(stop=stop_after_attempt(5))
def call_external_api(arg):
    ...

# OK: shared rate limiter (semaphore)
sem = asyncio.Semaphore(5)  # cap concurrency
async def call_external_api(arg):
    async with sem:
        ...
```

Library-specific support examples:

| Library | Recommended API |
|---|---|
| Python `tenacity` | `wait_exponential` / `wait_random_exponential` |
| Python `httpx` | `AsyncHTTPTransport(retries=...)` + custom transport for backoff |
| Go `cenkalti/backoff` | `ExponentialBackOff` |
| Go `sony/gobreaker` | `CircuitBreaker` |
| LangChain | `ChatModel(... max_retries=N)` + manual `wait_exponential` |
| OpenAI Python SDK | `OpenAI(max_retries=N)` (internal exponential backoff) |

---

## 6. Known False Positives / False Negatives

### False Positive

- **fan-in is 20 but real concurrency is ≤ 5**: Static analysis cannot see runtime scheduler behavior. Cut Warning and below via `--min-confidence 0.8` to mitigate.
- **Idempotent retries with no real impact**: Cases like GET-only cache fetches where storm causes no upstream damage. Severity stays at Warning / Info, so it does not block CI directly (Critical is exit code 2).

### False Negative

- **Retry delegated to outer middleware**: Framework-internal retry middleware (LangGraph's `RetryPolicy`, Vercel AI SDK's `streamText({ maxRetries: ... })`) may not appear in Config. Framework-specific parsers (planned for the second half of Phase 2) will broaden coverage.
- **Code that decides retry count dynamically**: Expressions like `retries=int(os.getenv("RETRY"))` may not have their numeric value extracted depending on the parser, and are treated as Config-absent (false negative).

---

## 7. Related Rules / ADRs

- **ADR-006**: ESLint visitor pattern — contrast with Local rules. This rule is at the Path tier.
- **ADR-007**: Local / Path / Global three-tier separation — basis for this rule sitting at the Path tier.
- **ADR-008**: ConfidenceReason — Critical = `exact_static_match`, Warning/Info = `heuristic_pattern`.
- `cost_estimation` (analogous to `docs/rules/cost-estimation.md`) — same shape as a Path rule with Sinks=nil.
- `max_parallel_branches` (Global) — fan-out detection alone. Combined with retry_storm it is complementary.
- `error_handler_checker` (Path) — missing error path on Tool nodes. Tools flagged by retry_storm are often also targets of error_handler_checker.
