# testdata/secrets/

`secret_exposure_scanner` ルールの動作確認用サンプルデータ。

## ファイル構成

| ファイル | 説明 | 期待 Findings |
|---|---|---|
| `exposed.json` | AWS/OpenAI/Anthropic キーがハードコードされたグラフ | 3件 (Critical×3) |
| `safe.json` | すべてが環境変数参照・プレースホルダーのグラフ | 0件 |

## 検証コマンド

```bash
# exposed: Critical が 3件検出される
shingan analyze --format json --input testdata/secrets/exposed.json --output markdown

# safe: 0件 (クリーン)
shingan analyze --format json --input testdata/secrets/safe.json --output markdown
```
