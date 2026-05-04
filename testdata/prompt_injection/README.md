# testdata/prompt_injection/

`prompt_injection_sink` ルールの動作確認用サンプルデータ。

## ファイル構成

| ファイル | 説明 | 期待 prompt_injection_sink Findings |
|---|---|---|
| `leak.json` | `user_query` (Config["source"]="user_input") → preprocess → LLM with `system_prompt` containing `{{user_query}}` substitution | 1件 (Critical, Confidence 0.9, ConfidenceReason heuristic_pattern) |
| `safe.json` | 同じ user_query を `validator` で型保証してから、LLM 側は静的 system に分離して `messages_template` 経由で `role: user` に格納 | 0件 |

## 検証コマンド

```bash
# leak: prompt_injection_sink Critical が 1件検出される
shingan analyze --format json --input testdata/prompt_injection/leak.json

# safe: prompt_injection_sink は 0件
shingan analyze --format json --input testdata/prompt_injection/safe.json
```

`leak.json` には他に `error_handler_checker` Warning が 2件含まれる (Tool ノードの条件分岐欠落) が、これは別ルールの動作であり本サンプルの注目点ではない。

## 設計メモ

- **leak.json** の Critical 発火点 = `system_prompt` フィールド内の `{{user_query}}` substitution。プレースホルダ `{{...}}` / `${...}` / `{...}` のいずれにマッチしても Critical (Confidence 0.9)。
- **safe.json** が安全な理由 = (1) 静的 system prompt にユーザ入力を **混ぜていない**、(2) ユーザ入力は `messages_template` という非標準キー経由で渡しており、 `role: user` 区分内に分離されている。シングンの sink classifier は `system_prompt` / `system` / `instruction(s)` / `prompt_template` / `user_message_template` / `user_template` / `prompt` のみを認識する。
- 静的解析の限界として `messages_template` のような OS/フレームワーク固有のキーは知らないので false negative の可能性がある。実運用ではフレームワーク固有 parser (LangGraph / ADK-Go) で構造化メッセージ配列を直接読むのが望ましい。
