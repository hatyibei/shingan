# testdata/dynamic_construction/

`dynamic_node_construction` ルールの動作確認用サンプルデータ。

## ファイル構成

| ファイル | 説明 | 期待 dynamic_node_construction Findings |
|---|---|---|
| `eval.json` | Tool ノードの `Config["body"]` が `lambda payload: eval(payload['code'])` を含む — 文字列レベルで `eval(` パターンに直接マッチ | 1件 (Critical, Confidence 0.95, ConfidenceReason exact_static_match) |
| `safe.json` | 同じ Tool 構造だが `body` は `HANDLERS[payload['op']](...)` という allow-list dispatch、`handler` は `${HANDLER_FACTORY_PATH}` という placeholder のみ — どちらも本ルールの検出パターンにマッチしない | 0件 |

## 検証コマンド

```bash
# eval: dynamic_node_construction Critical が 1件
shingan analyze --format json --input testdata/dynamic_construction/eval.json

# safe: dynamic_node_construction は 0件 (allow-list dispatch + placeholder)
shingan analyze --format json --input testdata/dynamic_construction/safe.json
```

`eval.json` には他に `error_handler_checker` Warning が含まれる可能性がある (Tool ノードの条件分岐欠落) が、本サンプルの注目点ではない。

## 設計メモ

- **eval.json** の Critical 発火点 = `body` フィールド内の `eval(` 部分文字列。検出パターン priority: eval/exec/Function (Critical 0.95) > compile/__import__ (Warning 0.85) > getattr/setattr (Info 0.6)。
- **safe.json** が安全な理由 = (1) `body` の `HANDLERS[...]` は静的 dict 経由の allow-list dispatch で本ルールの正規表現にマッチしない、(2) `handler` の `${HANDLER_FACTORY_PATH}` は placeholder のみのため strip-then-recheck で除外される。secret_exposure_scanner と同じ `placeholderPattern` を共有している。
- 静的解析の限界として、**`HANDLERS` 辞書の中身が後から書き換えられて任意 callable を仕込まれる** ような attack vector は検出できない (動的な属性アクセスとして `getattr/setattr` は Info で出るが)。
- Scan 対象 Config キーは `body` / `fn` / `handler` / `callback` / `code` / `factory` / `builder` のみ。`description` のような自由テキストフィールドは除外しているため、コメントとして "Wraps eval() calls safely" のような語が含まれていても発火しない。
