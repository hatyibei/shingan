# testdata/retry_storm/

`retry_storm` ルールの動作確認用サンプルデータ。

## ファイル構成

| ファイル | 説明 | 期待 retry_storm Findings |
|---|---|---|
| `storm.json` | Tool ノード `storm_api` が `retries=5` × `max_concurrency=20` (blast radius 100) | 1件 (Critical, Confidence 0.9, ConfidenceReason exact_static_match) |
| `safe.json` | Tool ノード `api_caller` が `retries=2` × `max_concurrency=4` (retries が閾値 3 未満なので source にならない) | 0件 |

## 検証コマンド

```bash
# storm: retry_storm Critical が 1件検出される
shingan analyze --format json --input testdata/retry_storm/storm.json

# safe: retry_storm は 0件
shingan analyze --format json --input testdata/retry_storm/safe.json
```

`storm.json` には他に `error_handler_checker` Warning が含まれる場合がある (Tool ノードの条件分岐欠落) が、これは別ルールの動作であり本サンプルの注目点ではない。

## 設計メモ

- **storm.json** の Critical 発火条件:
  - `retries` が >= 3 (storm のソース判定閾値)
  - parallelism = max(fan-in, max_concurrency, upstream Loop max_iterations) — ここでは `max_concurrency=20` が支配的
  - blast radius = `retries × parallelism` = 5 × 20 = 100 → `>= 100` で Critical
- **safe.json** が安全な理由:
  - `retries=2` なのでそもそも storm のソースにならない (閾値 3 未満)
  - 仮に `retries=3` でも `max_concurrency=4` なので blast = 12 → Info 程度に留まる
  - `backoff_factor=2.0` のような mitigation の存在は静的解析では「読み取れる shape の一例」に過ぎないが、 retry 数が小さいことで自動的に safe 判定になる
- 静的解析の限界: backoff の実装が library / runtime に閉じている場合や、 retry が外側 LangGraph グラフ・circuit breaker proxy 等で吸収される場合は false negative の可能性がある。実運用ではフレームワーク側の retry middleware を確認のこと。
