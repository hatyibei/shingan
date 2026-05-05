# testdata/prompt_secret

`secret_in_prompt_template` ルールの動作確認用サンプルデータ。

| ファイル | 期待 `secret_in_prompt_template` Findings |
|---|---|
| `leak.json` | 1件 (Critical, Confidence 0.95, ConfidenceReason `exact_static_match`)。`system_prompt` 内に `sk-...` がハードコードされている |
| `safe.json` | 0件 — `system_prompt` の API key は `${OPENAI_API_KEY}` 形式の env-var placeholder |

## 検証コマンド

```bash
shingan analyze --format json --input testdata/prompt_secret/leak.json
shingan analyze --format markdown --input testdata/prompt_secret/safe.json
```

## 設計メモ

- `leak.json` では同時に **`secret_exposure_scanner` も Critical 1 件** 発火する
  (OnAny 再帰スキャンが `system_prompt` の値も走査するため)。これは設計上の重複で、
  `secret_in_prompt_template` 側は **prompt-specific な Suggestion** (env-var 置換 +
  rotation 指示) を出すことに価値がある。`secret_exposure_scanner` は generic な
  `prompt`/`api_key`/`headers` 等のキーをカバーしている。
- `safe.json` は `${ENV_VAR}` 形式のため両ルールとも 0 件。`{{ env.X }}` /
  `process.env.X` / `os.Getenv(...)` も同様に exempt される。
- 検出パターンは AWS access key / OpenAI / Anthropic / GitHub / PEM
  block (Critical) と JWT (Warning) の 6 種。
