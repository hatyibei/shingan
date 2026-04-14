---
marp: true
theme: shingan
paginate: true
header: "Shingan — AI Agent Workflow Static Analyzer"
footer: "hatyibei / 2026-04-17"
---

<!-- _class: title -->
<!-- _paginate: false -->
<!-- _header: "" -->
<!-- _footer: "" -->

# Shingan（心眼）
## AI Agent Workflow Static Analyzer

**hatyibei**
Kiva社 最終面接 | 2026-04-17

---

## なぜ作ったか ── 30秒の答え

- **「100%精度」を12名で実現する** には品質保証を人の注意力に頼れない
- AIエージェントの「設計時バグ」を検出するカテゴリが **市場空白**
- コンパイラがランタイムエラー前にコンパイルエラーを出すのと同じ発想
- ESLintの立ち上がり期と同じ、**カテゴリ形成フェーズ**

---

## 事業課題からの逆算

**ADR-001: なぜこのプロダクトか**

| 制約 | 含意 |
|---|---|
| 100%精度 | エラー1つが顧客信頼を損なう |
| 12名チーム | 手動レビューのスケールに限界 |
| エンタープライズ展開 | コメ兵・関通の本番ワークフロー |

→ **設計時に構造的バグを機械検出する仕組みが必要**

---

## 市場空白

| ツール | 対象 | 解析性質 |
|---|---|---|
| FlowLint | n8n専用 | 静的（従来型） |
| LangSmith | LangGraph | **ランタイム**観測 |
| LangFuse | 各種フレームワーク | **ランタイム**観測 |
| **(該当なし)** | AI Agent 一般 | **静的 ← Shingan** |

> 壊れてから検知するか、壊れる前に防ぐか。

---

## アーキテクチャ

**Onion Architecture** ── ドメインを中心に外側が依存する設計

```
┌──────────────────────────────────────────────┐
│  cmd/          DI配線・エントリポイント           │
│  ┌──────────────────────────────────────┐    │
│  │  infrastructure/  具象実装            │    │
│  │  ┌──────────────────────────────┐   │    │
│  │  │  application/  ユースケース   │   │    │
│  │  │  ┌──────────────────────┐   │   │    │
│  │  │  │  domain/  外部依存ゼロ │   │   │    │
│  │  │  └──────────────────────┘   │   │    │
│  │  └──────────────────────────────┘   │    │
│  └──────────────────────────────────────┘    │
└──────────────────────────────────────────────┘
```

**Factory Pattern 3箇所**: Analyzer / Parser / Reporter

---

## 4つのエントリポイント

| コマンド | 用途 |
|---|---|
| `shingan` | CLI ── ファイル解析、CI組込 |
| `shingan-api` | goa Design-first HTTP API + 自動OpenAPI |
| `shingan-runner` | Vertex AI Gemini 実行 + safe-guard |
| `shingan-web` | ADK Web UI + middleware ← SamuraiAI想定 |

**goa** ── DSLからOpenAPIを自動生成。実装がAPI定義を裏切れない構造。

---

## 7つの解析ルール

| Rule | 検出 | Severity |
|---|---|---|
| `cycle_detection` | LoopAgent管理下/外のサイクル | Warning / Critical |
| `loop_guard` | MaxIterations未設定 | **Critical** |
| `error_handler_checker` | 外部I/O後のエラーハンドリング欠落 | **Critical** |
| `unreachable_node` | エントリから到達不能なノード | Warning |
| `cost_estimation` | ループ内高額モデル使用 | Warning |
| `redundant_llm_call` | 同一prompt×modelの重複呼出 | Warning |
| `pii_leak_scanner` | RAG→外部API PII漏洩経路 | Warning |

---

## デモ ── 4ステップ (90秒)

```bash
bash scripts/demo.sh
```

**Step 1** 静的解析でCritical警告 ── MaxIterations未設定を即検出

**Step 2** Runner safe-guardで実行拒否 ── Critical findingがあると実行ブロック

**Step 3** 安全版（MaxIter=3）はクリーン判定で実行成功

**Step 4** Vertex AI Gemini で実際のAgent応答「こんにちは！」

> コスト: 1デモ実行あたり **&lt;0.1円**（gemini-2.0-flash-001）

---

## ADK Web UI統合

SamuraiAIのようなGUIワークフローエディタへの統合イメージ

```bash
bash scripts/web-demo.sh
# → http://localhost:8080
```

- **middleware注入**: `router.Use(shinganGuardMiddleware(...))` → RunAPI前にShinganが割り込む
- **Critical ワークフロー** → 実行ブロック + Web UIにエラー表示
- **クリーン ワークフロー** → 通常通りVertex AI Gemini実行

---

## SamuraiAIへの適用

ADR Appendix B: **14ノード → Shingan NodeType マッピング済み**

```
infrastructure/parser/samurai.go ← スキーマ差し替えのみ
```

Onion Architectureなので、ドメイン層（7ルール）は **変更なし**。

**差し替え手順 (3ステップ)**
1. `SamuraiWorkflow` 構造体を実スキーマに合わせる
2. `mapSamuraiNodeType` のcaseを実ノード名に更新
3. `testdata/samurai/` の実JSONでテストを通す

---

## エンタープライズ機能

- **SARIF出力** → GitHub Code Scanning統合
  - PRの "Files changed" に警告インライン表示
  - Branch Protectionで「Criticalゼロでないとマージ不可」を設定可能
- **GitHub Action**: `uses: hatyibei/shingan@v0.1.0`
- **Docker image**: distroless、multi-arch対応
- **OpenAPI spec**: クライアントSDK自動生成対応

---

## ロードマップ

| フェーズ | 時期 | 内容 |
|---|---|---|
| **v0.1** | 2026-04 ✓ | 7ルール、ADK-Go対応、Web UI middleware |
| **v0.2** | Q2-Q3 2026 | n8nパーサー、`go/types`型情報連動、キャッシュ |
| **v0.3** | Q4 2026 | 信頼度スコア、PII拡張、CI plugin |
| **v1.0** | Q1 2027 | LangGraph / Dify対応、運用観測統合 |

---

## 開発プロセス

- **期間**: ADR 1日 + 実装 1.5日 = 計 **2.5日**
- **品質**: 141テスト関数、`-race` green、CI構成（lint/test/build）
- **AI活用**: Claude Code 並列オーケストレーション
  - 各フェーズで3-4エージェントを同時ディスパッチ
  - アーキテクチャ判断・設計は自分。実装スピードをAIで補完
- **判断の記録**: ADR 5件 + docs/各種設計文書

---

## なぜKivaでやりたいか

- **事業課題と技術手段の垂直統合**
  - 「100%精度」要件から逆算した設計
- **実ワークフローでベンチマーク**
  - コメ兵・関通の本番データで誤検知率を検証したい
- **OSSとして育てつつ競争優位を守る**
  - 汎用ルール → OSS公開
  - SamuraiAI固有のルールセット → 競争優位として非公開

---

<!-- _paginate: false -->
<!-- _header: "" -->
<!-- _footer: "" -->

## ありがとうございました

**github.com/hatyibei/shingan**

技術質問、事業質問、何でもどうぞ。

---

*参考: 終了コード規約*

| Code | 意味 |
|---|---|
| `0` | Info以下のみ（クリーン） |
| `1` | Warning検出 |
| `2` | **Critical検出 → CI失敗** |
