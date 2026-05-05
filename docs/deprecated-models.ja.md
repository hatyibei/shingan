> 🌐 Language: [English](./deprecated-models.md) | **日本語**

# deprecated_model ルール — 非推奨・停止済みモデル検出

## 概要

`deprecated_model` ルールは、ワークフローグラフ内の LLM ノードが **停止済み (shutdown)** または **非推奨 (deprecated)** なモデルを使用しているかを検出します。

| ステータス | Severity | Confidence | 説明 |
|-----------|----------|------------|------|
| `shutdown` | Critical | 1.0 | 実行時に API エラーが発生する。即座に移行必須 |
| `deprecated` | Warning | 0.9 | まだ呼び出せるが、~6 ヶ月以内に shutdown 予定 |

---

## モデル分類テーブル

### OpenAI

| モデル | ステータス | Shutdown 日 | 推奨移行先 |
|--------|-----------|------------|-----------|
| `gpt-3.5-turbo-0301` | shutdown | 2024-06-13 | `gpt-4o-mini` |
| `gpt-3.5-turbo-0613` | shutdown | 2024-09-13 | `gpt-4o-mini` |
| `gpt-3.5-turbo-16k-0613` | shutdown | 2024-09-13 | `gpt-4o-mini` |
| `text-davinci-003` | shutdown | 2024-01-04 | `gpt-4o` |
| `text-davinci-002` | shutdown | 2024-01-04 | `gpt-4o` |
| `code-davinci-002` | shutdown | 2024-01-04 | `gpt-4o` |
| `gpt-4-0314` | shutdown | 2024-06-13 | `gpt-4o` |
| `gpt-4-32k` | **deprecated** | 2025-06-06 | `gpt-4o` |
| `gpt-4-0613` | **deprecated** | 2025-06-06 | `gpt-4o` |

### Anthropic

| モデル | ステータス | Shutdown 日 | 推奨移行先 |
|--------|-----------|------------|-----------|
| `claude-1` | shutdown | 2023-11-01 | `claude-3-5-sonnet` |
| `claude-1.3` | shutdown | 2023-11-01 | `claude-3-5-sonnet` |
| `claude-2` | shutdown | 2024-07-21 | `claude-3-5-sonnet` |
| `claude-2.0` | shutdown | 2024-07-21 | `claude-3-5-sonnet` |
| `claude-2.1` | shutdown | 2024-07-21 | `claude-3-5-sonnet` |
| `claude-instant-1` | shutdown | 2024-07-21 | `claude-3-haiku` |
| `claude-instant-1.2` | shutdown | 2024-07-21 | `claude-3-haiku` |
| `claude-3-opus` | **deprecated** | 2025-10-01 | `claude-3-5-sonnet or claude-opus-4` |

### Google

| モデル | ステータス | Shutdown 日 | 推奨移行先 |
|--------|-----------|------------|-----------|
| `gemini-pro` | shutdown | 2025-02-15 | `gemini-1.5-pro` |
| `text-bison-001` | shutdown | 2024-10-01 | `gemini-1.5-flash` |
| `chat-bison-001` | shutdown | 2024-10-01 | `gemini-1.5-flash` |

---

## マイグレーション推奨先

### 汎用タスク (コスト最適化優先)
- `gpt-4o-mini` — OpenAI の低コスト高品質モデル
- `claude-3-haiku` — Anthropic の高速・低コストモデル
- `gemini-1.5-flash` — Google の高速・低コストモデル

### 高精度タスク (品質優先)
- `gpt-4o` — OpenAI フラッグシップ
- `claude-3-5-sonnet` — Anthropic の主力モデル
- `gemini-1.5-pro` — Google の高性能モデル

---

## 更新頻度

各プロバイダの公式 deprecation policy を参照し、定期的にこのテーブルを更新してください:

- **OpenAI**: https://platform.openai.com/docs/deprecations
- **Anthropic**: https://docs.anthropic.com/en/api/deprecations
- **Google**: https://cloud.google.com/vertex-ai/generative-ai/docs/learn/model-versioning

---

## testdata サンプル

```bash
# Critical×3 (shutdown models)
./shingan analyze --format json --input testdata/deprecated/shutdown_models.json

# Warning×1 (deprecated model)
./shingan analyze --format markdown --input testdata/deprecated/deprecated_models.json

# 0 findings (active models)
./shingan analyze --format json --input testdata/deprecated/active_models.json
```

## shingan-gen による生成

```bash
./shingan-gen --pattern deprecated-model | ./shingan analyze --format json --input /dev/stdin
# 期待: Critical×1 (gpt-3.5-turbo-0613)
```
