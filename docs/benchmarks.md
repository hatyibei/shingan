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
