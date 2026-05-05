# testdata/unbounded

Sample WorkflowGraph JSON files for the `unbounded_tool_arg` rule.

| File | Expected `unbounded_tool_arg` findings |
|------|----------------------------------------|
| `tool.json` | Warning×2 (string `query` without maxLength, array `tags` without maxItems) + Info×1 (integer `page_size` without maximum) |
| `safe.json` | 0 findings — every schema field has a bound (`maxLength`, `maxItems`, `maximum`) |

> 両ファイルとも独立して `error_handler_checker` Warning が 1 件発火する
> (Tool ノードに conditional outgoing edge がないため)。これは別ルールの動作で、
> `unbounded_tool_arg` の検証には影響しない。

## Usage

```bash
./shingan analyze --format json --input testdata/unbounded/tool.json
./shingan analyze --format markdown --input testdata/unbounded/safe.json
```

## 設計メモ

- ルールは Tool ノードの `args_schema` / `parameters` / `input_schema` のみを走査
  (汎用 Config 値は `secret_exposure_scanner` がカバーする)。
- nested object / array (items) の schema も再帰的に走査される。`tool.json`
  では trial としてあえて `tags.items.maxLength` を bounded にしてあるので、
  外側 `tags` array (maxItems 不在) のみが Warning を発火する。
- 1 ノードあたり最大 5 finding に cap される (50 フィールドで埋もれない設計)。
