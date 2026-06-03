# tool_description_missing — 設計・実装ドキュメント

> **対象バージョン**: Phase 2 系列
> **実装ファイル**: `domain/rules/tool_description_missing.go`
> **テスト**: `domain/rules/tool_description_missing_test.go`
> **層 (ADR-007)**: Local rule — `Tool` ノード単位で完結判定

---

## 1. 背景・動機

LLM agent は **description のテキストだけ** を見て呼び出すツールを選ぶ。description が無い、
または 1 単語しかないと、モデルは推測でツールを選択することになり、誤ツール選択・
引数のハルシネーション・誤った API 呼び出し (高コスト) を招く。本ルールはこの
「説明不足の Tool」を検出する。

---

## 2. 検出条件

以下が **すべて** 成立したとき Info を 1 件出す:

1. `Node.Type == NodeTypeTool`。
2. `Config` の `description` / `doc` / `summary` / `help` のいずれにも、trim 後
   **10 文字以上** の非空文字列が無い。
3. Tool の `Name` (または `Config["tool_name"]`) 自体が description を兼ねていない
   (3 語以上の自然文ならパス — 例: `"Send email to recipient"` は description フィールドが
   無くても許容)。

### 除外

- `ToolCategory == "trigger"` の Tool (webhook / scheduler は LLM-facing でないため
  description 不要)。判定は `Node.GetToolCategory()` 経由 (typed field 優先;
  ADR-003 Onion 強化)。
- `Config["_shingan_ignore"]` に本ルール名を含むノード。

---

## 3. Severity / Confidence

| 項目 | 値 |
|---|---|
| Severity | **Info** |
| Confidence | **0.6** |
| ConfidenceReason | `heuristic_pattern` |

「十分な description」は本質的に曖昧 (保守的に ≥10 文字を閾値とするが、`"search"` のような
良い 1 単語名も発火しうる) ため、Confidence は 0.6。

---

## 4. Suggestion

各 Tool に、LLM がいつ・どう使うかを判断できる 1 文以上の description を付与することを推奨する。

---

## 5. 関連ルール

- `unbounded_tool_arg` — Tool 引数スキーマの境界欠落 (本ルールは description 欠落)。
