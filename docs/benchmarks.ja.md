> 🌐 Language: [English](./benchmarks.md) | **日本語**

# Shingan Performance Benchmarks

> 計測日: 2026-04-15 (pii_leak_scanner v0.2最適化後に再計測)
> 環境: Linux x86_64 (WSL2), Go 1.25.0, Intel Core i7-13700F (24 logical cores)
> コマンド: `go test -bench=. -benchmem -run=^$ ./...`
> グラフ: `GenerateRandomGraph(n, seed=42)` — サイクル・PII経路・重複LLM等を意図的に含む

## 各ルール単独実行

| Rule | n=10 | n=100 | n=1000 |
|------|------|-------|--------|
| cycle_detection | 2,150 ns | 92,475 ns | 8,621,182 ns (8.6 ms) |
| loop_guard | 256 ns | 2,243 ns | 16,986 ns (0.017 ms) |
| unreachable_node | 1,553 ns | 78,921 ns | 8,772,122 ns (8.8 ms) |
| error_handler_checker | 3,026 ns | 74,953 ns | 10,698,628 ns (10.7 ms) |
| cost_estimation | 2,527 ns | 116,181 ns | 14,924,143 ns (14.9 ms) |
| redundant_llm_call | 1,082 ns | 3,234 ns | 77,462 ns (0.077 ms) |
| pii_leak_scanner | 3,918 ns | 208,848 ns | 26,369,182 ns (26.4 ms) ✅ v0.2最適化済み |

### ルール別考察

- **loop_guard / redundant_llm_call**: ノードをフラットにスキャンするだけで O(n)。1000ノードでも 0.02-0.08 ms と極めて高速。
- **cycle_detection / unreachable_node / error_handler_checker / cost_estimation**: DFS/BFS を伴う。`OutgoingEdges` が全エッジを線形スキャンするため O(n × e) ≈ O(n^2.2) となり、n=1000 で 8-15 ms に収まる。目標値 20 ms をクリア。
- **pii_leak_scanner** ✅ v0.2最適化済み: 逆方向隣接リスト事前構築 + Sink起点逆BFS により O(V+E) を実現。n=1000 で 267 ms → **26.4 ms**（**10倍改善**）。目標 50 ms を大幅クリア。

## Orchestrator (全7ルール)

| Variant | n=10 | n=100 | n=1000 |
|---------|------|-------|--------|
| 並行 (goroutine) | 36,371 ns (0.036 ms) | 397,440 ns (0.40 ms) | 27,743,086 ns (27.7 ms) |
| シーケンシャル | 18,985 ns (0.019 ms) | 675,329 ns (0.68 ms) | 74,051,336 ns (74.1 ms) |
| Speedup | 0.52x | 1.70x | **2.67x** |

### Orchestrator 考察

- **小規模 (n=10)**: goroutine 生成コストがオーバーヘッドになり、シーケンシャルの方が速い (0.52x)。
- **中規模 (n=100)**: goroutine の並列化効果が現れ始め、並行が 1.70x 高速。
- **大規模 (n=1000)**: pii_leak_scanner の v0.2 最適化 (267 ms → 26.4 ms) により、次のボトルネック (cost_estimation 14.9 ms) が律速となった。並行実行が **2.67x** に改善し、Orchestrator 全体も **27.7 ms** と目標 50 ms をクリア。

## Parser

| Parser | n=10 (小規模) | n=100 (中規模) | n=1000 (大規模) |
|--------|--------|--------|--------|
| ADKGo | 17,512 ns (0.018 ms) | 145,207 ns (0.15 ms) | 1,436,799 ns (1.44 ms) |
| JSON | 14,842 ns (0.015 ms) | 135,955 ns (0.14 ms) | 1,331,450 ns (1.33 ms) |

両パーサーとも n=1000 で 1.3-1.5 ms。実ユースケースの CI パイプラインでは Parse + Analyze (pii 除く) で **< 50 ms** 以内に完了する。

## 総合考察

1. **pii_leak_scanner v0.2最適化完了** — 旧実装は PII ソース数 × ノード数 × エッジ数の三重ループ相当 (O(sources·V·E))。v0.2 では逆方向隣接リスト (map[string][]Edge) を事前構築し、Sink起点の逆BFS を1回のみ実行することで O(V+E) を実現。n=1000 で 267 ms → **26.4 ms**（**10倍改善**）。

2. **goroutine 並行化の効果** — pii_leak_scanner の最適化により、7ルール並行実行の Orchestrator が 273.7 ms → **27.7 ms** に改善。Speedup も 1.14x → **2.67x** と大幅向上。次のボトルネックは cost_estimation (14.9 ms) であり、更なる改善余地あり。

3. **面接で使えるアピール**: 「n=1000ノード・7ルール並行実行のベンチマークを Go testing/benchmark で実装し、pii_leak_scanner の O(sources·V·E) ボトルネックを定量的に特定。逆方向隣接リスト + Sink起点逆BFS による O(V+E) 最適化を実装し、267ms → 26ms（10倍）改善を計測ベースで達成した。」

## 実行方法

```bash
# 全ベンチマーク
make bench

# ルール単独
make bench-rules

# パッケージ別
go test -bench=. -benchmem -run=^$ ./domain/rules/...
go test -bench=. -benchmem -run=^$ ./application/...
go test -bench=. -benchmem -run=^$ ./infrastructure/parser/...
```
