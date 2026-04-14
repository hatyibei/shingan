# testdata/pii — PIIリークルール検証シナリオ

このディレクトリには `pii_leak_scanner` ルール（v0.3）の検証用ワークフローグラフが含まれています。

## ファイル一覧

### `leak_risk.json` — リスクあり（Warning 期待）

**シナリオ**: 顧客情報RAG検索 → 要約LLM → 外部マーケティングAPI

```
rag_search ──→ llm_summarize ──→ external_api
(RAG/PII源)    (LLMは通過点)    (外部送信/Sink)
                                ↑ Human gate なし → Warning
```

期待 Finding:
- rule: `pii_leak_scanner`
- severity: `warning`
- node: `external_api`
- message: PIIノードから外部Toolへの直接パスが存在する

### `safe.json` — 安全（Finding なし）

**シナリオ**: 顧客情報RAG検索 → 要約LLM → **プライバシー担当者承認(Human)** → 外部API

```
rag_search → llm_summarize → human_approval → external_api
                              ↑ Human gate あり → Safe
```

期待 Finding: なし（Human ノードがゲートとして機能する）

## 検出ロジック概要

1. **PII源ノード識別**: `category=="rag"` または `has_pii==true` の Tool ノード
2. **外部Sink識別**: `category` が `api`/`mcp`/`browser` の Tool ノード
3. **BFS探索**: PII源から外部Sinkへのパスを幅優先探索
4. **Human gate判定**: パス上に `NodeTypeHuman` が存在する場合は安全とみなし探索停止

## 手動実行確認

```bash
# リスクあり → Warning が発火
./shingan analyze --format json --input testdata/pii/leak_risk.json

# 安全 → Finding なし
./shingan analyze --format json --input testdata/pii/safe.json
```
