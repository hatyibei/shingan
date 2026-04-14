# Shingan Performance Benchmarks

> 計測日: 2026-04-15
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
| pii_leak_scanner | 1,138 ns | 240,008 ns | 267,219,322 ns (267 ms) ⚠️ |

### ルール別考察

- **loop_guard / redundant_llm_call**: ノードをフラットにスキャンするだけで O(n)。1000ノードでも 0.02-0.08 ms と極めて高速。
- **cycle_detection / unreachable_node / error_handler_checker / cost_estimation**: DFS/BFS を伴う。`OutgoingEdges` が全エッジを線形スキャンするため O(n × e) ≈ O(n^2.2) となり、n=1000 で 8-15 ms に収まる。目標値 20 ms をクリア。
- **pii_leak_scanner** ⚠️: PII ソースノード「ごと」に BFS を実行し、各ステップで `OutgoingEdges` を線形スキャンするため、実質 O(pii_sources × n × e)。n=1000 で 267 ms と目標 20 ms を大幅超過。ボトルネック。

## Orchestrator (全7ルール)

| Variant | n=10 | n=100 | n=1000 |
|---------|------|-------|--------|
| 並行 (goroutine) | 31,310 ns (0.031 ms) | 405,745 ns (0.41 ms) | 273,663,049 ns (273.7 ms) |
| シーケンシャル | 13,781 ns (0.014 ms) | 640,400 ns (0.64 ms) | 311,796,418 ns (311.8 ms) |
| Speedup | 0.44x | 1.58x | **1.14x** |

### Orchestrator 考察

- **小規模 (n=10)**: goroutine 生成コストがオーバーヘッドになり、シーケンシャルの方が速い (0.44x)。
- **中規模 (n=100)**: goroutine の並列化効果が現れ始め、並行が 1.58x 高速。
- **大規模 (n=1000)**: pii_leak_scanner の 267 ms がボトルネックとなり、全体が最遅ルールの時間に収束。スピードアップは 1.14x にとどまる。pii_leak_scanner を最適化すれば、残る 6 ルールは合計で 44 ms 以下であり、Orchestrator 全体を **< 50 ms** に抑えることが可能。

## Parser

| Parser | n=10 (小規模) | n=100 (中規模) | n=1000 (大規模) |
|--------|--------|--------|--------|
| ADKGo | 17,512 ns (0.018 ms) | 145,207 ns (0.15 ms) | 1,436,799 ns (1.44 ms) |
| JSON | 14,842 ns (0.015 ms) | 135,955 ns (0.14 ms) | 1,331,450 ns (1.33 ms) |

両パーサーとも n=1000 で 1.3-1.5 ms。実ユースケースの CI パイプラインでは Parse + Analyze (pii 除く) で **< 50 ms** 以内に完了する。

## 総合考察

1. **ボトルネック: pii_leak_scanner** — 現実装は PII ソース数 × ノード数 × エッジ数の三重ループ相当。隣接リスト (map[string][]Edge) を事前構築することで `OutgoingEdges` の O(e) → O(1) 化が可能。これにより n=1000 でも数 ms に改善できる見込み。

2. **goroutine 並行化の効果** — ルール間の独立性を活かした並行実行は理論上ボトルネックルールの時間に収束する。pii_leak_scanner を最適化すれば次のボトルネック (cost_estimation 14.9 ms) が律速となり、Orchestrator 全体で **< 20 ms** が現実的。

3. **面接で使えるアピール**: 「n=1000ノード・7ルール並行実行のベンチマークを Go testing/benchmark で実装し、pii_leak_scanner の O(n²) ボトルネックを定量的に特定。隣接リスト最適化により Orchestrator 全体を 273 ms → < 20 ms に改善できることを計測ベースで提言した。」

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
