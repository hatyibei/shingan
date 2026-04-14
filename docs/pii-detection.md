# PIIリーク検出ルール (`pii_leak_scanner`) — 設計・実装ドキュメント

> **対象バージョン**: v0.3 (preview)
> **実装ファイル**: `domain/rules/pii_leak.go`

---

## 1. 背景・動機

エンタープライズ向けAIエージェントシステムにおいて、**個人情報（PII: Personally Identifiable Information）の意図しない外部漏洩**は最大のコンプライアンスリスクである。

GDPR（欧州）、CCPA（カリフォルニア州）、日本の個人情報保護法のいずれも、PIIの第三者送信には**本人同意または業務上の正当根拠**が必要であり、ワークフロー設計の段階で漏洩経路を検出できれば、違反リスクを事前に排除できる。

**SamuraiAIのエンタープライズ顧客における最大懸念**: RAG（検索拡張生成）パイプラインが顧客DBから個人情報を取得し、そのまま外部APIやMCPサーバーへ送信してしまうパターン。

---

## 2. 検出対象

### 2.1 PII源ノード（Source）

| 判定条件 | Severity | 説明 |
|---------|---------|------|
| `NodeTypeTool` + `category == "rag"` | Warning | RAGは個人情報を含む可能性が高い |
| `NodeTypeTool` + `Config["has_pii"] == true` | Warning | 明示的なPIIフラグ |
| ノード名に `pii`/`user`/`personal`/`private` を含む | Info | ヒューリスティック（偽陽性あり） |

### 2.2 外部送信ノード（Sink）

`NodeTypeTool` かつ `category` が以下のいずれか:

- `api` — 外部REST/GraphQL API
- `mcp` — Model Context Protocol サーバー
- `browser` — ブラウザ自動操作（フォーム送信など）

### 2.3 Human Approval Gate（安全境界）

`NodeTypeHuman` ノードがPII源からSinkへの経路上にある場合、そのパスは安全とみなし検出しない。
Human ノードがプライバシー担当者や承認フローを表現することを前提とする。

---

## 3. 検出アルゴリズム

```
for each PII-source node S in graph:
    BFS from S:
        if current_node == NodeTypeHuman:
            stop this branch (Human gate = safe)
        if current_node is external sink (api/mcp/browser):
            emit Finding(severity, sink_node, message)
            stop this branch
        else:
            continue BFS to neighbors
```

**計算量**: O(V + E) per PII source node。通常のワークフローグラフ（< 100ノード）では実用上問題なし。

### 重複検出の防止

各PII源ノードごとに独立したBFSを実行し、`visited` マップを初期化する。
同一のSinkノードが複数のPII源から到達可能な場合、それぞれについて個別のFindingを発行する（各経路を独立した漏洩リスクとして扱う）。

---

## 4. Severity 表

| 条件 | Severity | 対応 |
|------|----------|------|
| RAG源 → 外部Sink（Human gate なし） | `Warning` | Human承認ノードを挿入、またはPIIサニタイズ |
| `has_pii=true`源 → 外部Sink（Human gate なし） | `Warning` | 同上 |
| 名前ヒューリスティック源 → 外部Sink | `Info` | 要確認（偽陽性の可能性あり） |

---

## 5. 実例ワークフローと検出結果

### 5.1 リスクあり（`testdata/pii/leak_risk.json`）

```
rag_search ──→ llm_summarize ──→ external_api
```

```json
{
  "rule": "pii_leak_scanner",
  "severity": "warning",
  "node_id": "external_api",
  "message": "potential PII leak: path from RAG/PII node \"rag_search\" (顧客情報RAG検索) to external tool \"external_api\" (category=\"api\") without Human approval gate",
  "suggestion": "ノード \"rag_search\" と \"external_api\" の間にHuman承認ノードを挿入するか、PIIフィールドをサニタイズしてください (GDPR/CCPA/個人情報保護法対応)"
}
```

### 5.2 安全（`testdata/pii/safe.json`）

```
rag_search → llm_summarize → human_approval → external_api
```

Finding: なし（Human ゲートが保護している）

---

## 6. AnalyzerFactory での登録

`pii_leak_scanner` は v0.3 で7番目のルールとして追加された。

```go
// infrastructure/factory/analyzer.go
case "pii_leak_scanner":
    return rules.NewPIILeakScanner(), nil
```

`CreateAll()` が返す7ルール:

1. `cycle_detection`
2. `unreachable_node`
3. `error_handler_checker`
4. `cost_estimation`
5. `redundant_llm_call`
6. `loop_guard`
7. `pii_leak_scanner` ← new

---

## 7. v0.3 での拡張予定

| 機能 | 優先度 | 概要 |
|------|--------|------|
| PIIコンテンツ検査 | High | ノードのConfig/Prompt内の正規表現パターン（メール、電話番号、マイナンバー等）でPIIを検出 |
| `encrypt_in_transit` フラグ | Medium | 送信時暗号化が設定されている場合はSeverityを下げる |
| SARIF出力連携 | Medium | GitHub Advanced SecurityのSARIF形式でPIIリスクを出力 |
| データフロータギング | Low | ノードごとにPIIタグを伝播させ、より精密なパス分析を実現 |
| 正規表現ベース検出 | Low | `\d{3}-\d{4}-\d{4}`（電話番号）等のパターンをPromptTemplate内で検索 |

---

## 8. 面接トークポイント

**SamuraiAI向けインパクト文 (50字以内)**:
> RAGパイプラインのPII漏洩を設計時に静的検出し、GDPR違反ゼロを保証

**技術的ハイライト**:
- Onion Architecture準拠: `domain/rules/` に外部依存なし（stdlib + domain パッケージのみ）
- BFS探索で全漏洩パスを網羅的に検出
- Human-in-the-loop パターンを「ゲート」として自動認識
- 明示フラグ（`has_pii`）+ カテゴリ推論 + 名前ヒューリスティックの3層判定で偽陰性を最小化

**競合差別化**:
従来の静的解析ツール（semgrep等）はコードレベルのパターンマッチングに留まるが、shinganは**ワークフロー意味論**（RAGがPIIを含むという業務知識）をルールに埋め込み、より高精度な検出を実現する。
