# unbounded_tool_arg — 設計・実装ドキュメント

> **対象バージョン**: Phase 2 #4 (v0.6 系列)
> **実装ファイル**: `domain/rules/unbounded_tool_arg.go`
> **テスト**: `domain/rules/unbounded_tool_arg_test.go` (17 ケース)
> **層 (ADR-007)**: Local rule — `OnNode[NodeTypeTool]` で 1 node 完結判定

---

## 1. 背景・動機

LLM agent が呼び出す **Tool** の引数定義に **上限が無い** とき、攻撃者・あるいは LLM 自身がモンスターな payload を生成して Tool に流し込めてしまう:

- string 引数に 100MB のテキストを送り込んで token 数 / 月額課金を爆発させる
- array 引数に 10000 要素をぶら下げて downstream の DB / API を過負荷にする
- number 引数に `2^53` を送って overflow / OOM を起こす

これらは **runtime sanitization 以前に、Tool 定義の段階** で `maxLength` / `maxItems` / `maximum` を入れていれば防げる。本ルールはワークフローの Tool ノード定義を JSON schema として扱い、上限の欠落を静的に検出する。

---

## 2. 検出対象

### 2.1 走査対象 (Tool ノードに限定)

`NodeType.Tool` のみが対象。`Config` 直下の以下のキーが `map[string]any` の場合に JSON schema として走査:

| キー | 由来 |
|---|---|
| `args_schema` | LangChain `BaseTool.args_schema`、ADK-Go `Tool.ArgsSchema` |
| `parameters` | OpenAI function calling `tool.parameters`、Anthropic `tool.input_schema` の別名 |
| `input_schema` | Anthropic Messages API `tool.input_schema` |

これ以外の Config キー (`name`, `description`, `category` 等) は **走査しない** ことで、汎用 Config 値を見る `secret_exposure_scanner` との重複を回避する。

### 2.2 Schema 内の再帰

- `properties` (object schema) → 各フィールドを `classifyField` で個別判定
- `items` (array schema) → 内部要素 schema へ再帰

JSON schema の `oneOf` / `anyOf` / `$ref` までは現状追わない (false positive 抑制と保守性のトレードオフ)。

### 2.3 Severity マトリクス

| 状況 | Severity | Confidence | ConfidenceReason |
|---|---|---|---|
| `string` × `maxLength` 未指定 | **Warning** | 0.7 | `heuristic_pattern` |
| `string` × `maxLength > 100_000` | **Info** | 0.5 | `heuristic_pattern` |
| `array` × `maxItems` 未指定 | **Warning** | 0.7 | `heuristic_pattern` |
| `number` / `integer` × `maximum` 未指定 | **Info** | 0.4 | `heuristic_pattern` |

`maxLength = "unlimited"` (string 型の typo) は **数値ではない** ため `hasNumericKey` は false を返し、未指定と同等扱いとなる。これは ADR-008 の「typo を見逃して silent pass しない」原則。

### 2.4 Finding cap

1 Tool ノードで **5 finding まで** に cap (`maxFindingsPerNode = 5`)。50 フィールドの大規模 schema で report が埋もれるのを防ぐ妥協。

---

## 3. 既存ルールとの棲み分け

| ルール | 範囲 | 重複 |
|---|---|---|
| `secret_exposure_scanner` | 全 Config 値の string を再帰スキャン | 本ルールは Tool ノードの schema フィールドのみ。Config[`api_key`] のような値スキャンは secret rule の責務 |
| `temperature_misuse` | LLM ノードの `temperature` / `task` / `structured_output` | NodeType が違うので衝突なし |
| `model_card_mismatch` | LLM ノードの `model` × `base_url` / `provider` | 同上 |

---

## 4. Suggestion パターン

各 finding には `Suggestion` フィールドで具体的な修正方針が入る:

```
Add `maxLength` to schema field "args_schema.query" (e.g. 4000) so attacker-
controlled or LLM-generated payloads cannot trigger token / API failures.
```

ユーザは IDE / LSP のホバー or QuickFix で Suggestion を確認しながら schema を修正できる (将来 ADR-008 の AutoFix フィールド導入時には schema の `maxLength: 4000` を直接挿入する CodeAction を提供する想定)。

---

## 5. False Positive / False Negative

### False Positive

- **意図的に bound を外している schema**: 例えば「LLM の出力をそのまま受け取る」内部 Tool で `maxLength` を入れてしまうと quality regression するケース。`--min-confidence 0.8` で抑制可能 (本ルールは最大 0.7、すべて 0.7 以下に降格される)。
- **`oneOf` / `anyOf` 配下の field**: 現状 `properties` / `items` のみ追うので、`oneOf` ブランチ内の string field は **検出されない**。これは false negative 側のトレードオフ (false positive は出ない)。

### False Negative

- **schema を `Config["args_schema_yaml"]` のような non-canonical キーで保持している**: 検出されない。フレームワーク固有 parser (LangGraph / ADK-Go) で構造化 schema を直接読むのが王道。
- **動的 schema 構築**: code 側で `args_schema = build_schema(env)` のように runtime 生成しているケースでは Config に static な map が現れず検出できない。

---

## 6. 関連 ADR

- **ADR-006**: ESLint visitor pattern — Local rule の OnNode dispatcher を使用。
- **ADR-007**: Local / Path / Global の3層分離 — 本ルールは Local tier。
- **ADR-008**: ConfidenceReason — `ReasonHeuristicPattern` を採用。
- **ADR-010**: Plugin SDK internal-only (v1.0 まで) — 本ルールも `init()` で `registerBuiltin()` 経由の登録。
