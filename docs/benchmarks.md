> 🌐 Language: **English** | [日本語](./benchmarks.ja.md)

# Shingan Performance Benchmarks

> Measured: 2026-04-15 (re-measured after pii_leak_scanner v0.2 optimization)
> Environment: Linux x86_64 (WSL2), Go 1.25.0, Intel Core i7-13700F (24 logical cores)
> Command: `go test -bench=. -benchmem -run=^$ ./...`
> Graph: `GenerateRandomGraph(n, seed=42)` — intentionally includes cycles, PII paths, redundant LLM calls, etc.

## Per-Rule Standalone Execution

| Rule | n=10 | n=100 | n=1000 |
|------|------|-------|--------|
| cycle_detection | 2,150 ns | 92,475 ns | 8,621,182 ns (8.6 ms) |
| loop_guard | 256 ns | 2,243 ns | 16,986 ns (0.017 ms) |
| unreachable_node | 1,553 ns | 78,921 ns | 8,772,122 ns (8.8 ms) |
| error_handler_checker | 3,026 ns | 74,953 ns | 10,698,628 ns (10.7 ms) |
| cost_estimation | 2,527 ns | 116,181 ns | 14,924,143 ns (14.9 ms) |
| redundant_llm_call | 1,082 ns | 3,234 ns | 77,462 ns (0.077 ms) |
| pii_leak_scanner | 3,918 ns | 208,848 ns | 26,369,182 ns (26.4 ms) ✅ v0.2 optimized |

### Per-Rule Discussion

- **loop_guard / redundant_llm_call**: Flat scan over nodes is O(n). Even at 1000 nodes, runs in 0.02–0.08 ms — extremely fast.
- **cycle_detection / unreachable_node / error_handler_checker / cost_estimation**: Involve DFS/BFS. Because `OutgoingEdges` linearly scans all edges, the complexity is O(n × e) ≈ O(n^2.2), landing in 8–15 ms at n=1000. Clears the 20 ms target.
- **pii_leak_scanner** ✅ v0.2 optimized: Pre-builds a reverse adjacency list and runs reverse BFS from sinks, achieving O(V+E). At n=1000, 267 ms → **26.4 ms** (**10x improvement**). Significantly clears the 50 ms target.

## Orchestrator (All 7 Rules)

| Variant | n=10 | n=100 | n=1000 |
|---------|------|-------|--------|
| Concurrent (goroutine) | 36,371 ns (0.036 ms) | 397,440 ns (0.40 ms) | 27,743,086 ns (27.7 ms) |
| Sequential | 18,985 ns (0.019 ms) | 675,329 ns (0.68 ms) | 74,051,336 ns (74.1 ms) |
| Speedup | 0.52x | 1.70x | **2.67x** |

### Orchestrator Discussion

- **Small (n=10)**: Goroutine creation cost dominates as overhead, so sequential is faster (0.52x).
- **Medium (n=100)**: Goroutine parallelism starts paying off — concurrent is 1.70x faster.
- **Large (n=1000)**: After the v0.2 optimization of pii_leak_scanner (267 ms → 26.4 ms), the next bottleneck became cost_estimation (14.9 ms). Concurrent execution improves to **2.67x**, and the Orchestrator overall lands at **27.7 ms**, clearing the 50 ms target.

## Parser

| Parser | n=10 (small) | n=100 (medium) | n=1000 (large) |
|--------|--------|--------|--------|
| ADKGo | 17,512 ns (0.018 ms) | 145,207 ns (0.15 ms) | 1,436,799 ns (1.44 ms) |
| JSON | 14,842 ns (0.015 ms) | 135,955 ns (0.14 ms) | 1,331,450 ns (1.33 ms) |

Both parsers run at 1.3–1.5 ms for n=1000. In real-world CI pipelines, Parse + Analyze (excluding pii) completes within **< 50 ms**.

## Overall Discussion

1. **pii_leak_scanner v0.2 optimization complete** — The old implementation was effectively a triple loop over PII source count × node count × edge count (O(sources·V·E)). v0.2 pre-builds a reverse adjacency list (map[string][]Edge) and runs reverse BFS from sinks exactly once, achieving O(V+E). At n=1000: 267 ms → **26.4 ms** (**10x improvement**).

2. **Effect of goroutine concurrency** — Thanks to the pii_leak_scanner optimization, the 7-rule concurrent Orchestrator improved from 273.7 ms → **27.7 ms**. Speedup also rose from 1.14x → **2.67x**, a major gain. The next bottleneck is cost_estimation (14.9 ms), where further improvements remain possible.

3. **Interview talking point**: "I implemented benchmarks for 7-rule concurrent execution at n=1000 nodes using Go testing/benchmark, quantitatively identified the O(sources·V·E) bottleneck in pii_leak_scanner, and implemented an O(V+E) optimization using a reverse adjacency list + sink-rooted reverse BFS, achieving a measured 267 ms → 26 ms (10x) improvement."

## How to Run

```bash
# All benchmarks
make bench

# Standalone rules
make bench-rules

# Per package
go test -bench=. -benchmem -run=^$ ./domain/rules/...
go test -bench=. -benchmem -run=^$ ./application/...
go test -bench=. -benchmem -run=^$ ./infrastructure/parser/...
```

---

# Real-World Accuracy (Dogfood Sweep, v0.5.0 → v0.8.7)

Runtime benchmarks above measure *speed*. The far more relevant question
for a static-analysis tool is *signal quality*: when shingan flags a
node, is the developer's correct response "yes, fix that" or "ignore,
false positive"? We track this by sweeping real-world OSS that use
LangGraph / CrewAI / n8n / ADK-Go in production and recording how each
release lands on each repo.

> Methodology: zero hand-tuning. We run `shingan analyze --format=<fw>
> --input=<repo>` exactly as a CI user would, count every finding by
> rule, classify each as a real issue vs. false positive by reading the
> code, and ship a fix for every Critical FP before the next release.

## Cumulative track record (12+ OSS, v0.5.0 → v0.8.7)

| Project | Framework | Stars | Findings @ first run | Critical FP @ latest | Notes |
| --- | --- | --- | --- | --- | --- |
| `assafelovic/gpt-researcher` | LangGraph | 24K | 1 cycle_detection | **0** | Real bug → [Issue #1766](https://github.com/assafelovic/gpt-researcher/issues/1766) |
| `langchain-ai/open_deep_research` | LangGraph | 7K | 9 → 1 (after v0.8 router fix) | **0** | Real bug → [Issue #269](https://github.com/langchain-ai/open_deep_research/issues/269), maintainer-style community engagement |
| `langchain-ai/executive-ai-assistant` | LangGraph | 1K | 14 → 3 (after v0.8 sentinel/router fix) | **0** | Archived, but parser improvements landed in v0.8.6 |
| `langchain-ai/company-researcher` | LangGraph | 600 | 1 Critical FP → 0 (v0.8.6 router-Literal fix) | **0** | Triggered the `tools_condition` builtin handling |
| `starpig1129/AI-Data-Analysis-MultiAgent` (DATAGEN) | LangGraph | 1.7K | 2 unreachable FP → 0 (v0.8.7 for-loop unrolling) | **0** | Triggered v0.8.7 FP-1 fix |
| `theyashwanthsai/Devyan` | CrewAI | 289 | 3 unreachable FP → 0 (v0.8.7 agents-only fallback) | **0** | Triggered v0.8.7 FP-2 fix |
| `langtalks/swe-agent` | LangGraph | 630 | 4 cycle_detection | **0** | Real bug → [Issue #6](https://github.com/langtalks/swe-agent/issues/6) |
| `ArcInstitute/SRAgent` | LangGraph | — | **0 findings** | **0** | Clean repo, no FP either |
| `CopilotKit/open-multi-agent-canvas` | LangGraph | — | **0 findings** | **0** | Clean repo |
| `letta-ai/letta` (formerly MemGPT) | LangGraph | 13K | swept in v0.8.4 dogfood | **0** | — |
| `langchain-ai/langgraph-supervisor-py` | LangGraph | 800 | swept in v0.8.4 | **0** | — |
| `langchain-ai/data-enrichment` | LangGraph | 200 | canonical tool-calling pattern | **0** | Skipped Issue submission — impact too weak |

**Zero Critical false positives across every sweep at the latest release**. Each non-zero number in the "Findings" column was either (a) a real bug we filed upstream, or (b) a parser-shim gap we closed in the next release. The fix commits are linked from each release's CHANGELOG entry under "dogfood-driven".

## Dogfood-driven shim improvements (v0.5.0 → v0.8.7)

The fixes below were all triggered by real OSS — not by synthetic test cases. They are why the FP count keeps trending down.

| Release | Fix | Source repo | Pattern closed |
| --- | --- | --- | --- |
| v0.8.7 | LangGraph for-loop edge unrolling | DATAGEN | `for x in [<lit>, …]: g.add_edge(x, "T")` |
| v0.8.7 | CrewAI agents-only empty-graph | Devyan | `agents.py` factory-style module |
| v0.8.6 | LangGraph `Command(goto=…)` typed-return | open_deep_research | `Command[Literal["a","b"]]` |
| v0.8.6 | LangGraph bare-return router (Source 3) | chatbot-simulation-eval | untyped router with `>=2` distinct literal returns |
| v0.8.6 | LangGraph BoolOp + functools.partial | simulation_utils | `should_continue or functools.partial(_should_continue, …)` |
| v0.8.6 | LangGraph fan-in list-src `add_edge` | langgraph/bench/wide_state | `add_edge(["a","b"], "c")` |
| v0.8.6 | LangGraph END-sentinel exit recognition | many | `add_edge(node, END)` → `HasExitBranch=true` |
| v0.8.6 | LangGraph 2-pass visit ordering | simulation_utils | router defined after `make_graph()` |
| v0.8.6 | LangGraph router-Literal annotation lookup | executive-ai-assistant | `add_conditional_edges("src", route_fn)` with no `path_map` |
| v0.8.6 | CrewAI generic-Exception fallback | langchain-academy | import-time `OpenAIError` / `RuntimeError` |
| v0.8.6 | n8n sticky-note skip | n8n workflows | `n8n-nodes-base.stickyNote` widgets |
| v0.8.6 | Onion: typed `Node.HasExitBranch` field | — | replaced cross-layer `Config["has_end_branch"]` |

## Reproducing the accuracy benchmark

The full corpus is one Make target:

```bash
make dogfood
# → clones every repo in the corpus to /tmp/shingan-dogfood,
#   runs `shingan analyze` against each, writes one Markdown
#   report per repo plus a summary INDEX.md.
#
# Env overrides (any combination):
#   OUT_DIR=/path/to/dir   different working directory
#   SHINGAN=./bin/shingan  use a locally built binary
#   MIN_CONF=0.5           lower the confidence threshold
```

Or run one repo at a time:

```bash
git clone --depth=1 https://github.com/starpig1129/AI-Data-Analysis-MultiAgent /tmp/datagen
shingan analyze --format=langgraph --input=/tmp/datagen \
                --output=markdown --min-confidence=0.7

# Expected with shingan-lint@0.8.7+: 4 bounded-cycle warnings,
# zero unreachable_node FPs. Earlier versions emitted 2 FPs on
# QualityReview / NoteTaker before the for-loop unrolling fix.
```

The corpus tracked by `make dogfood` is the source of truth for the
[track-record table](#cumulative-track-record-12-oss-v050--v087) above.
When a new release lands, re-run `make dogfood` and update the table
from the resulting `INDEX.md` so the published numbers stay in sync
with what you can reproduce locally.

## Zero-FP guarantee policy

For every Critical false positive surfaced in dogfood (or reported via
GitHub Discussions), the fix lands in the *next* release together with
a regression fixture pinned in `testdata/`. We treat FP fixes as
load-bearing CHANGELOG entries (not housekeeping notes), and the
"dogfood-driven" tag on each entry tells maintainers exactly which
real-world pattern motivated the change.
