# Secret Detection — `secret_exposure_scanner`

> v0.3 feature (前倒し実装)

## 概要

`secret_exposure_scanner` はワークフローグラフの `Node.Config` フィールドに埋め込まれた
シークレット（APIキー・トークン・秘密鍵）を静的に検出するルールです。

LLMプロンプトや tool 引数にシークレットがハードコードされると、ログ・デバッグ出力・LLM のコンテキストを通じてシークレットが漏洩するリスクがあります。

---

## 検出パターン一覧

| パターン名 | 正規表現 | Severity |
|---|---|---|
| `aws_access_key` | `AKIA[0-9A-Z]{16}` | **Critical** |
| `private_key_pem` | `-----BEGIN (RSA )?PRIVATE KEY-----` | **Critical** |
| `anthropic_api_key` | `sk-ant-[A-Za-z0-9_-]{20,}` | **Critical** |
| `openai_api_key` | `sk-[A-Za-z0-9]{20,}` | **Critical** |
| `github_token` | `gh[pousr]_[A-Za-z0-9]{36,}` | Warning |
| `slack_token` | `xox[bpars]-[A-Za-z0-9-]{10,}` | Warning |
| `jwt` | `eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}` | Info |
| `generic_secret` | `(?i)(password\|secret\|api_key\|apikey\|token)\s*[:=]\s*['"]?[A-Za-z0-9_-]{20,}` | Info |

**注意:** `anthropic_api_key` は `openai_api_key` より先にチェックされます（`sk-ant-` は `sk-` にもマッチするため）。

---

## Severity 判定

| Severity | 対象 | 対応優先度 |
|---|---|---|
| **Critical** | AWS/GCP/OpenAI/Anthropic のキー・秘密鍵 | 即時対応必須 |
| **Warning** | GitHub Token / Slack Bot Token | 当日対応推奨 |
| **Info** | JWT / 汎用的な password=XXX パターン | 計画的に対応 |

---

## スキャン対象 Config フィールド

以下のフィールドを含む**すべての Config 値**を再帰的にスキャンします：

- `string` 値 — 直接パターンマッチ
- `map[string]any` 値 — キーを `parent.child` 形式でネスト展開してスキャン
- `[]any` 値 — インデックス付き `parent[0]` 形式でスキャン

代表的な検出対象：
- `Config["prompt"]` / `Config["prompt_template"]` / `Config["instruction"]`
- `Config["api_key"]` / `Config["headers"]` (例: `Authorization: Bearer sk-...`)
- 配列形式のプロンプトリスト

---

## 除外ロジック（誤検知防止）

以下のパターンが含まれる値は**安全な参照**として除外します：

| パターン | 例 |
|---|---|
| Shell 環境変数 | `$API_KEY`, `${OPENAI_KEY}` |
| テンプレートプレースホルダー | `{{secret}}`, `{{ env.TOKEN }}` |
| Node.js 環境変数参照 | `process.env.API_KEY` |
| Go 環境変数参照 | `os.Getenv("API_KEY")` |

**ただし**、プレースホルダーを含む文字列の残りの部分にシークレットが含まれる場合は検出します。
例: `"sk-abc123... ${SUFFIX}"` → プレースホルダー除去後も `sk-abc123...` が残るため検出。

---

## 実装例（安全なパターン）

```json
{
  "id": "llm_node",
  "config": {
    "api_key": "${OPENAI_API_KEY}",
    "prompt": "Authenticate using {{api_token}}",
    "headers": {
      "Authorization": "Bearer process.env.API_KEY"
    }
  }
}
```

---

## 実装例（危険なパターン）

```json
{
  "id": "llm_node",
  "config": {
    "api_key": "sk-abcdefghijklmnopqrstuvwxyz123456",
    "prompt": "Use AKIAIOSFODNN7EXAMPLE for AWS",
    "headers": {
      "Authorization": "Bearer sk-ant-api01-abcdefghijklmnopqrstuvwxyz"
    }
  }
}
```

---

## 検証コマンド

```bash
# ハードコードキーを含むグラフ → Critical 検出
shingan analyze --format json --input testdata/secrets/exposed.json --output markdown

# 環境変数参照のみ → 0件
shingan analyze --format json --input testdata/secrets/safe.json --output markdown

# shingan-gen で生成してパイプ検証
shingan-gen --pattern secret-exposure | shingan analyze --format json --input /dev/stdin --output markdown
```

---

## v0.4 予定: Shannon Entropy スキャナー

パターンマッチに加え、**Shannon エントロピー判定**により未知のシークレットパターンを検出する高精度スキャナーを予定しています。

- エントロピー閾値 > 3.5 のランダム文字列を候補としてフラグ
- ベースライン（ソルト不使用ハッシュ vs ランダムキー）の自動判定
- 誤検知率を下げるための文脈（キー名・近傍テキスト）スコアリング
