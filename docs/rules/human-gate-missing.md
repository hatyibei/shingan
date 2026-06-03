# human_gate_missing — 設計・実装ドキュメント

> **対象バージョン**: Phase 2 系列
> **実装ファイル**: `domain/rules/human_gate_missing.go`
> **テスト**: `domain/rules/human_gate_missing_test.go`
> **層 (ADR-007)**: Local rule — グラフ全体集約 (`OnGraph`) で 1 finding

---

## 1. 背景・動機

本番デプロイされた agent workflow が、外部副作用 (API 書き込み・コード実行・送金・データ
egress・ブラウザ自動操作) を **人間の承認なし** に実行できる構成は、ガバナンス上のリスク
である。`pii_leak_scanner` が特定の source→sink パスを追跡するのに対し、本ルールは
**グラフ全体の posture** を検査する: 「外の世界に触れる本番 agent には、どこかに human-in-
the-loop が必要」という原則を強制する。

---

## 2. 検出条件

以下が **すべて** 成立したとき Warning を 1 件出す:

1. **Deploy signal**: いずれかのノードが本番/ステージング相当のシグナルを持つ
   (`Config["env"] ∈ {prod, production, staging}`、`Config["deployment"] == true`、
   `Config["deploy"] == true`、`Config["environment"] ∈ {prod,…}`)。
2. **Sensitive-action signal**: いずれかの `Tool` ノードが機微カテゴリ
   (`ToolCategory ∈ {code_execution, api, mcp, browser}`) を持つ、または名前が
   `send` / `delete` / `transfer` / `payment` / `email` / `webhook` /
   `execute` / `deploy` / `publish` / `fire` などのパターンにマッチ。
3. **Human signal の不在**: `Type == NodeTypeHuman` のノードが 1 つも存在しない。

(2) の機微アクション gate により、「本番だが外部副作用のない純計算グラフ」では誤発火しない。

> **注**: カテゴリ判定は `Node.GetToolCategory()` 経由 (typed field 優先、`Config["category"]`
> フォールバック; ADR-003 Onion 強化)。

---

## 3. Severity / Confidence

| 項目 | 値 |
|---|---|
| Severity | **Warning** |
| Confidence | **0.6** |
| ConfidenceReason | `heuristic_pattern` |

deploy signal が命名ヒューリスティック (`Config["env"]` 等; 一部のチームは `Config["stage"]`
等の独自キーを使う) であるため、Confidence は 0.6 に留める。

---

## 4. Suggestion

機微アクションの前に `Human` 承認ノード (human-in-the-loop) を挿入することを推奨する。

---

## 5. 関連ルール

- `pii_leak_scanner` — 特定の PII source→sink パスを追跡 (本ルールはグラフ全体 posture)。
- `missing_eval_dataset` — 同じ deploy signal を共有する本番ガバナンス系ルール。
