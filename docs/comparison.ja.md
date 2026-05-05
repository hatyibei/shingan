# Shingan vs 競合ツール

Shingan の競合カテゴリは「ワークフロー/コードの静的解析」。AIエージェント固有領域で比較する。

---

## 全体比較

| ツール | 対象 | カテゴリ | 解析タイミング | AIエージェント対応 | 成熟度 |
|---|---|---|---|---|---|
| **Shingan** | **AI Agent Workflow** (ADK-Go / JSON / SamuraiAI想定) | 静的解析 | **設計時** | **完全対応** | v0.5.0 (2026-04) |
| FlowLint | n8n | 静的解析 | 設計時 | × (従来型自動化のみ) | v0.3.7 |
| AI-BOM | n8n | セキュリティ監査 | 設計時 | × (APIキー漏洩のみ) | Early |
| LangSmith | LangGraph | ランタイム観測 | **実行時** | ○ | Production |
| Systems Inspector | LangGraph | ランタイムテスト | **実行時** | ○ | Prototype |
| Semgrep | **コード一般** | 静的解析 | 設計時 | ×（AIエージェント構造認識なし） | Production |
| Snyk Code | コード一般 | セキュリティ | 設計時 | × | Production |
| ESLint | JavaScript | 静的解析 | 設計時 | × | Production |

**結論**: AIエージェントワークフロー固有の**静的解析**カテゴリは Shingan が2026年4月時点で事実上唯一。

---

## 機能比較マトリクス

### 検出できる問題

| 問題カテゴリ | Shingan | FlowLint | LangSmith | Semgrep |
|---|---|---|---|---|
| 無限ループ (設計時) | ✓ loop_guard | ✓ (retry-loop) | ランタイムでのみ | × |
| 到達不能ノード | ✓ unreachable_node | ✓ dead-end | × | × |
| エラーハンドリング欠落 | ✓ error_handler_checker | × | × | × |
| **LLMコスト爆発** | ✓ cost_estimation | × | × (事後検知) | × |
| **冗長LLM呼出** | ✓ redundant_llm_call | × | × | × |
| **PII漏洩経路** | ✓ pii_leak_scanner | × | × | 限定的 |
| **シークレット埋め込み** | ✓ secret_exposure_scanner | × (n8n credentials) | × | ✓ |
| **並列数過多** | ✓ max_parallel_branches | × | × | × |
| **非推奨モデル** | ✓ deprecated_model | × | × | × |
| 非Controlサイクル | ✓ cycle_detection | × | × | × |

**Shingan独自ルール**: 7個 (AIエージェント固有領域をカバー)

---

## Design philosophy 比較

### FlowLint
- **対象**: n8nのワークフロー定義 (JSON)
- **思想**: 既存の「ワークフロー自動化 (Zapier的)」のコード品質問題を検出
- **ギャップ**: AIエージェント固有の問題 (LLMコスト, 推論ループ, プロンプト設計) はカバーしない

### LangSmith
- **対象**: LangChain/LangGraph ランタイムのTrace
- **思想**: 実行ログを蓄積・可視化してデバッグ・最適化
- **ギャップ**: **実行**してから検知する。設計時ガードにはならない
- **補完関係**: Shingan (設計時) + LangSmith (実行時) で両輪

### Semgrep
- **対象**: Go/Python/JS等の一般コード
- **思想**: AST パターンマッチでのバグ検出
- **ギャップ**: AIエージェントの「ワークフローグラフ構造」は理解しない。LlmAgent間の依存関係や Fan-out は検出対象外

### Shingan
- **対象**: AIエージェントワークフロー (WorkflowGraphに正規化)
- **思想**: ワークフロー固有の構造を理解する専用解析器
- **独自性**: Onion Architectureで domain (ルール) が フレームワーク非依存 → アダプター追加だけで多フレーム対応

---

## FlowLintとの詳細比較

両者ともn8n的な構造を解析できるが、ターゲット領域が違う。

| 項目 | Shingan | FlowLint |
|---|---|---|
| 主対象 | AIエージェント (LlmAgent, Tool, LoopAgent) | n8n (Trigger, HTTP, Set, IF) |
| 汎用性 | ADK-Go/JSON/SamuraiAI、アダプター追加で拡張 | n8n専用 |
| 解析ルール数 | 10 | ~5 (retry-loop, dead-end, secrets) |
| LLMコスト判定 | ✓ cost_estimation (モデル価格階層) | × |
| 信頼度スコア | ✓ (0-1.0, --min-confidence) | × |
| 出力形式 | JSON / Markdown / **SARIF** | JSON |
| GitHub Code Scanning | ✓ SARIFで統合 | △ |
| 中核データモデル | Language-neutral WorkflowGraph | n8n schema固有 |

**Shingan は FlowLint の"AIエージェント特化版+多フレーム対応"版** と位置付けられる。

---

## LangSmith との補完関係

Shingan (設計時) と LangSmith (ランタイム) は競合ではなく**補完**。

```
┌─────────────────────────────────┐
│  開発時: Shingan 静的解析       │
│  - 10ルール、誤検知率管理       │
│  - CI PRで事前ブロック         │
└──────────────┬──────────────────┘
               │ デプロイ前
               ▼
┌─────────────────────────────────┐
│  実行時: LangSmith Trace        │
│  - ランタイム観測、A/Bテスト    │
│  - デバッグ、コスト実測         │
└─────────────────────────────────┘
```

**両輪の必要性**:
- Shingan は「構造的バグ」をカバー (cycle, unreachable)
- LangSmith は「非決定的挙動」をカバー (LLMハルシネーション、プロンプトの質)

---

## v1.0 への目標: Multi-framework static analyzer

v0.5 時点の対応: ADK-Go (native), JSON (native), SamuraiAI想定 (Alpha)

v0.6: n8n parser (Issue #4) → FlowLint の機能包含
v0.7: LangGraph (Python AST経由)
v0.8: Dify
v1.0: CrewAI, AutoGen対応、Multi-framework安定版

この時点でShinganは「ワークフローエンジン横断の **唯一の**静的解析プラットフォーム」になる。

---

## 結論

**Shingan の立ち位置** (2026-04):
- AIエージェントワークフローの静的解析カテゴリの**初期プレイヤー**
- FlowLint (n8n専用) より広く、LangSmith (ランタイム) より早く
- 10独自ルール、Onion Architecture、信頼度スコア搭載
- CI統合 (SARIF)、middleware injection、Vertex AI連携まで実装済

ESLintが2013年に立ち上がりJavaScript静的解析の標準になったように、Shinganは2026年に立ち上がりAIエージェント静的解析の標準を目指す。
