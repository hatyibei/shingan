# SamuraiAI アダプター設計書

## 概要

`infrastructure/parser/samurai.go` は SamuraiAI ワークフロー JSON を
Shingan の内部表現 (`domain.WorkflowGraph`) に変換するアダプターである。

このアダプターは Onion Architecture の価値を具体的なコードで実証する:
**`domain/` 層・`application/` 層を一切変更せずに、新フレームワーク対応が完了した。**

---

## 想定スキーマの例

```json
{
  "version": "1.0",
  "workflow_id": "wf_abc123",
  "entry_node": "node_1",
  "nodes": [
    {
      "id": "node_1",
      "type": "llm",
      "name": "分類器",
      "config": {
        "model": "gemini-1.5-flash",
        "prompt": "ユーザー入力を分類してください"
      }
    },
    {
      "id": "node_2",
      "type": "browser",
      "name": "ブラウザ操作",
      "config": { "action": "click", "selector": "#submit" }
    },
    {
      "id": "node_3",
      "type": "loop",
      "name": "リトライループ",
      "config": { "max_iterations": 5 }
    }
  ],
  "edges": [
    { "from": "node_1", "to": "node_2" },
    { "from": "node_2", "to": "node_3" },
    { "from": "node_3", "to": "node_1", "condition": "retry" }
  ]
}
```

> ⚠️ このスキーマは ADR Appendix B に基づく**想定版**。
> 実際の SamuraiAI 社内スキーマは非公開のため、入社後に差し替えを行う。

---

## ノード型マッピング (ADR Appendix B)

| SamuraiAI type | Shingan NodeType | config.category | 根拠 |
|---|---|---|---|
| `llm` | NodeTypeLLM | — | LLM による推論 |
| `auto_judge` | NodeTypeLLM | — | Intent 分類 (LLM) |
| `param_extract` | NodeTypeLLM | — | 構造化データ抽出 (LLM) |
| `agent` | NodeTypeLLM | — | 自律エージェント (LLM) |
| `browser` | NodeTypeTool | `"browser"` | 外部 GUI 操作 |
| `connector` | NodeTypeTool | `"api"` | 外部 API 呼出 |
| `api` | NodeTypeTool | `"api"` | 外部 API 呼出 |
| `mcp_tool` | NodeTypeTool | `"mcp"` | MCP 経由ツール呼出 |
| `code` | NodeTypeTool | `"code"` | カスタムコード実行 |
| `knowledge_search` | NodeTypeTool | `"rag"` | RAG 検索 |
| `loop` | NodeTypeControl | — | 反復制御 |
| `condition` | NodeTypeControl | — | 条件分岐 |
| `approval` | NodeTypeHuman | — | Human-in-the-loop |
| `review` | NodeTypeHuman | — | Human-in-the-loop |
| `output` | NodeTypeOutput | — | 最終出力 |
| `answer` | NodeTypeOutput | — | 最終出力 |
| `memo` | (スキップ) | — | 実行時無視 |

---

## アーキテクチャ上の位置づけ

```
┌─────────────────────────────────────────────────────┐
│  cmd/shingan/analyze.go  (CLI / 起動層)              │
│    --format samurai → ParserFactory.Create("samurai") │
└───────────────┬─────────────────────────────────────┘
                │
┌───────────────▼─────────────────────────────────────┐
│  infrastructure/factory/parser.go  (Factory)         │
│    case "samurai": return NewSamuraiParser()         │ ← ここに1行追加するだけ
└───────────────┬─────────────────────────────────────┘
                │
┌───────────────▼─────────────────────────────────────┐
│  infrastructure/parser/samurai.go  (Adapter)         │
│    SamuraiWorkflow → domain.WorkflowGraph            │ ← 新規追加ファイル
└───────────────┬─────────────────────────────────────┘
                │
┌───────────────▼─────────────────────────────────────┐
│  domain/ (変更なし)                                   │
│    WorkflowGraph, Node, Edge, NodeType               │ ← 一切触っていない
└─────────────────────────────────────────────────────┘
```

**変更ファイル数: 2**（`samurai.go` 新規 + `factory/parser.go` に1行追加）
**変更ファイル数 (domain + application 層): 0**

---

## 実スキーマ差し替え時の手順

入社後、SamuraiAI 公式スキーマを確認したときに行う作業:

**手順 1 — 構造体を差し替える** (`infrastructure/parser/samurai.go`):
```go
// SamuraiWorkflow, SamuraiNode, SamuraiEdge のフィールドを実スキーマに合わせる
type SamuraiWorkflow struct {
    // 公式スキーマのフィールドに変更
}
```

**手順 2 — ノード型マッピングを更新する** (同ファイル `mapSamuraiNodeType`):
```go
// 実ノード名 ("LLM_NODE" など) に合わせて case を修正
case "LLM_NODE":
    return domain.NodeTypeLLM, "", nil
```

**手順 3 — テストデータを更新する** (`testdata/samurai/`):
```bash
# 実 JSON サンプルを配置してテストを通す
go test -race ./infrastructure/parser/... -v
```

`domain/` 層・`application/` 層・`cmd/` 層の変更は不要。

---

## 面接でのストーリー

> 「入社後にどう統合しますか?」への回答

Shingan は Onion Architecture を採用しており、フレームワーク固有の知識は
`infrastructure/parser/` 層に閉じ込めている。

SamuraiAI への対応は既に実装済みのスケルトンとして `samurai.go` に存在する。
入社後に公式スキーマを確認し、構造体とマッピング表を差し替えるだけで統合が完了する。
`domain/` 層・`application/` 層は一切変更しないため、既存の解析ルール
(CycleDetector, ErrorHandlerChecker など) がそのまま SamuraiAI ワークフローに適用される。

このアーキテクチャ判断は ADR-003 に記録済みであり、コードで実証している。
