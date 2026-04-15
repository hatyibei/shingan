# testdata/parallel — max_parallel_branches テストデータ

このディレクトリには `max_parallel_branches` ルール (Issue #1) の動作検証用サンプルを格納する。

## ファイル一覧

| ファイル | 説明 | 期待される検出 |
|---|---|---|
| `high_fanout.json` | オーケストレーターが100ワーカーに並列分岐 | Critical (fan-out=100, Confidence=1.0) |
| `chunked.json` | 3チャンクコーディネーターが各9ワーカーに分岐 | 検出なし (各コーディネーターのfan-out=9 < 10) |
| `max_concurrency.json` | fan-out=25だがmax_concurrency=5設定あり | 検出なし (max_concurrency設定で抑制) |

## 実行例

```bash
# Critical発火を確認
./shingan analyze --format json --input testdata/parallel/high_fanout.json --output markdown

# chunked構造は安全 (0件)
./shingan analyze --format json --input testdata/parallel/chunked.json --output markdown

# max_concurrency=5で抑制 (0件)
./shingan analyze --format json --input testdata/parallel/max_concurrency.json --output markdown
```

## 閾値

| fan-out | Severity | Confidence |
|---|---|---|
| >= 100 | Critical | 1.0 |
| >= 20 | Warning | 0.9 |
| >= 10 | Info | 0.7 |
| < 10 | (検出なし) | — |

`Config["max_concurrency"]` が設定されているノードは閾値チェックをスキップする。
