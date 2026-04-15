# max_parallel_branches — 並列実行ノード数の上限チェック

## 概要

`max_parallel_branches` ルールは、ワークフローグラフ内で単一ノードが持つ outgoing edges 数 (fan-out degree) を検査し、
過剰な並列実行を検出する。

ParallelAgent / LoopAgent / SequentialAgent が多数のサブエージェントに同時並列分岐すると、
API レートリミット超過・リソース枯渇・コスト暴走のリスクがある。

## 検出ロジック

グラフの各ノードについて outgoing edges 数 (fan-out) をカウントし、以下の閾値で分類する。

| fan-out | Severity | Confidence | 説明 |
|---|---|---|---|
| >= 100 | **Critical** | 1.0 | ほぼ確実に API レートリミット超過・システム障害につながる |
| >= 20 | **Warning** | 0.9 | 多くのモデル API の並列上限を超える可能性が高い |
| >= 10 | **Info** | 0.7 | 注意が必要。本番環境では要確認 |
| < 10 | (検出なし) | — | 安全な範囲 |

## 例外 — max_concurrency 設定

ノードの `Config["max_concurrency"]` が設定されている場合、そのノードは明示的に並列数を制御しているとみなし、
閾値チェックをスキップする。

```json
{
  "id": "orchestrator",
  "name": "SafeOrchestrator",
  "type": "llm",
  "config": {
    "max_concurrency": 5
  }
}
```

上記のように設定すると、fan-out が 100 であっても検出されない。

## ParallelAgent / SequentialAgent との関連

ADK-Go の `ParallelAgent` は複数の `SubAgents` を並列実行する。
Shingan の JSON フォーマットでは、ParallelAgent の役割を持つノードは orchestrator ノードとして表現され、
各 SubAgent への outgoing edge が fan-out として計上される。

```
orchestrator → sub_agent_0
orchestrator → sub_agent_1
...
orchestrator → sub_agent_N   ← fan-out = N+1
```

`SequentialAgent` の場合、チェーン構造であれば各ノードの fan-out は 1 となるため問題にならない。

## 推奨対策

### 1. チャンク分割 (推奨)

100ワーカーを直接分岐する代わりに、10個ずつのチャンクに分けて中間コーディネーターを置く。

```
orchestrator → chunk_a (10 workers)
orchestrator → chunk_b (10 workers)
orchestrator → chunk_c (10 workers)
```

各 chunk の fan-out は 10 (Info) に留まり、orchestrator 自体は 3 (検出なし)。

### 2. max_concurrency 設定

既存のオーケストレーターがレートリミット制御を内部実装している場合は、
`Config["max_concurrency"]` を設定することで Shingan の検出を抑制できる。

```json
{
  "config": {
    "max_concurrency": 5
  }
}
```

## テストデータ

| ファイル | 内容 |
|---|---|
| `testdata/parallel/high_fanout.json` | fan-out=100 → Critical 発火 |
| `testdata/parallel/chunked.json` | チャンク分割構造 → 検出なし |
| `testdata/parallel/max_concurrency.json` | max_concurrency=5 → 検出なし |

## 動作確認コマンド

```bash
# Critical 発火を確認
./shingan analyze --format json --input testdata/parallel/high_fanout.json --output markdown

# shingan-gen でリアルタイム生成 + 解析
./shingan-gen --pattern high-fanout --size 100 --seed 42 | ./shingan analyze --format json --input /dev/stdin --output markdown

# Warning 閾値 (fan-out=20)
./shingan-gen --pattern high-fanout --size 20 --seed 42 | ./shingan analyze --format json --input /dev/stdin --output markdown
```

## 関連ルール

- `loop_guard` — LoopAgent の MaxIterations 未設定チェック (無限ループ防止)
- `cost_estimation` — ループ内の高額モデル使用コスト見積もり
