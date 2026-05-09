# Shingan（心眼）

## AI Agent Workflow Static Analyzer — Architecture Decision Records

```
作成者:    hatyibei
リポジトリ: github.com/hatyibei/shingan
ステータス: v0.8.3 Released (6 frameworks + AST-based fallback parser、ADR-014 確定。実 OSS hit 率 3% → 37.5%)
```

> **注 (2026-05-05)**: ADR-001 と ADR-002 は v0.1 (2026-04) 当時の判断記録です。当初は特定エンタープライズ製品 (GUI ワークフローエディタ) を念頭に置いた narrative で書かれていますが、Shingan は v0.6 で **汎用 AI agent linter** として再ポジショニングされ、LangGraph (Phase 1 主戦場、ADR-011) / ADK-Go / Generic JSON workflow / 任意 GUI ワークフローエディタを横並びでサポートします。当時の specific 文言は context として残しますが、最新方針は **ADR-006〜012** を参照してください。

---

# 目次

1. [ADR-001: プロダクト選定](#adr-001)
2. [ADR-002: 解析対象フレームワークの選定](#adr-002)
3. [ADR-003: アーキテクチャ設計](#adr-003)
4. [ADR-004: インフラストラクチャ設計](#adr-004)
5. [ADR-005: 実装スコープとスケジュール](#adr-005)
6. [ADR-006: ESLint方式 visitor + selector + listener 採用](#adr-006)
7. [ADR-007: Local / Path / Global の3層ルール分離](#adr-007)
8. [ADR-008: Confidence × ConfidenceReason 二次元品質管理](#adr-008)
9. [ADR-009: LSP 差分実行 + degraded mode](#adr-009)
10. [ADR-010: Plugin SDK internal-first 戦略](#adr-010)
11. [ADR-011: 主戦場 LangGraph シフト (ADR-002 補正)](#adr-011)
12. [ADR-012: multi-file directory analysis — per-file independent graph](#adr-012)
13. [Appendix A: 用語集](#appendix-a)
14. [Appendix B: SamuraiAI ↔ ADK-Go ノードマッピング](#appendix-b)
15. [Appendix C: 解析ルール詳細仕様](#appendix-c)

---

<a id="adr-001"></a>
# ADR-001: プロダクト選定 — なぜ「AIエージェントワークフローの静的解析」か

## ステータス
Accepted

## コンテキスト

### 事業背景

株式会社Kivaは、ワークフロー型GUI操作AIエージェント「SamuraiAI」を開発・提供している。以下の事業環境にある。

- 時価総額1000億円を1-2年で目指す成長フェーズ
- 開発チームは12名
- プロダクトの目標は「100%の精度」での業務自動化
- エンタープライズ顧客（コメ兵、関通等）への展開を急速拡大中
- 月額29,800円（ビジネスプラン）+ エンタープライズプラン（応相談）の収益構造

### プロダクト構造

SamuraiAIのワークフローは14種のノードで構成される。

| ノード種別 | 役割 | LLM依存 |
|---|---|---|
| LLM | Gemini / OpenAI / Anthropic によるテキスト処理 | Yes |
| ブラウザ操作 | 専用ブラウザによるGUI自動操作 | Yes（判断） |
| 外部連携コネクタ | MCP / 外部API呼出 | No |
| ループ | 繰り返し処理 | No |
| 条件分岐 | If/else | No |
| 自動判定 | Intent分類 | Yes |
| 承認/レビュー | Human-in-the-loop | No |
| 回答を出力 | 出力ノード | No |
| パラメータ抽出 | 構造化データ抽出 | Yes |
| MCP Tool | MCPツール呼出 | No |
| エージェント | 自律エージェント | Yes |
| ナレッジベース検索 | RAG | Yes |
| コード実行 | カスタムロジック | No |
| メモ | 設計用付箋（実行時無視） | No |

ワークフロー作成者は非エンジニアであり、自然言語で指示してフローを組む。テンプレートは22個提供されている。

### 構造的課題

100%精度を阻むボトルネックは「実行時」ではなく「設計時」に存在する。

ワークフローに以下の構造的バグが混入しうる:

1. **無限ループ**: ループノード + 条件分岐の組合せで脱出条件が不在または到達不能
2. **到達不能ノード**: 条件分岐の片方が論理的に死んでいる
3. **エラーハンドリング欠落**: ブラウザ操作ノードの直後に失敗時フローがない
4. **コスト非効率**: LLMノードがGPT-4oを使用しているが、タスク内容的にGPT-4o-miniで十分
5. **型不整合**: パラメータ抽出ノードの出力スキーマが次のノードの入力と一致しない
6. **セキュリティリスク**: ナレッジベース内のPIIがコネクタ経由で外部に送信されうるパス
7. **冗長なLLM呼出**: 同一プロンプトで同一モデルを複数回呼んでいる

12名のチームがエンタープライズ顧客のワークフローを1つずつレビューすることは物理的に不可能。自動検出の仕組みが必要。

### 市場調査結果

ソースコードの静的解析ツールは成熟市場（ESLint、SonarQube、Snyk Code、CodeRabbit等）。一方、AIエージェントワークフローの静的解析は以下の通り:

| ツール | 対象 | カテゴリ | 解析性質 | 成熟度 |
|---|---|---|---|---|
| FlowLint | n8n専用 | 従来型ワークフロー自動化 | 静的（retry, dead ends, secrets） | v0.3.7（2026-04初公開） |
| AI-BOM | n8n専用 | セキュリティ | 静的（APIキー漏洩、MCP接続先） | Early |
| LangSmith | LangGraph | AIエージェント | **ランタイム**観測 | Production |
| Systems Inspector | LangGraph | AIエージェント | **ランタイム**テスト | Prototype |
| (該当なし) | ADK / CrewAI / AutoGen / Dify / SamuraiAI | AIエージェント | 静的解析 | **市場空白** |

**結論: AIエージェントワークフローを「実行前に」構造的に検証するツールは、2026年4月時点で存在しない。**

FlowLintの登場（2026年4月）は、「ワークフロー静的解析」というカテゴリ自体が今まさに立ち上がっている証拠。ただしFlowLintはn8nの従来型自動化ワークフロー（Zapier的）が対象であり、AIエージェント固有の課題（LLMコスト爆発、エージェント推論ループ、プロンプトインジェクション経路）はカバーしていない。

## 選択肢

### 選択肢A: AIエージェントワークフロー静的解析エンジン（Shingan）
ワークフロー定義を実行せずにグラフとして解析し、構造的バグ・コストリスク・セキュリティリスクを検出する。

### 選択肢B: ワークフロー実行トレース & 監査API
ワークフロー実行後のログを構造化・蓄積・検索する。

### 選択肢C: LLMリバースプロキシ
LLM API呼出を仲介し、コスト制御・フォールバック・監査を行う。

### 選択肢D: MCPサーバービルドキット
MCPサーバーの作成・テスト・配信を支援する。

## 決定

**選択肢Aを採用する。**

## 根拠

### 選択肢B（トレース）を却下する理由
SamuraiAIには既に監査ログ機能が存在する（設定 > セキュリティ > 監査ログ）。差別化が困難。また、実行後の検出は「壊れてから直す」アプローチであり、100%精度の要件に対して構造的に不利。

### 選択肢C（プロキシ）を却下する理由
SamuraiAIのGUI操作型アーキテクチャとの接点が薄い。また、GoのHTTPプロキシ + SSEストリーミング処理はGo未経験者にとって2日間での実装難易度が高すぎる。

### 選択肢D（MCPビルドキット）を却下する理由
MCPサーバーの作成は既に公式SDK（TypeScript、Python、Go）で50行以下で可能。月間9700万DLのエコシステムが既に成立しており、差別化余地が限定的。

### 選択肢A（静的解析）を採用する理由

**前提1**: 100%精度を12名で達成するには、品質保証をユーザーの注意力に依存させない仕組みが必要。

**前提2**: コンパイラがランタイムエラーの前にコンパイルエラーを出すのと同様に、ワークフローの構造的バグは実行前に検出する方がコスト効率が高い。

**因果関係**: 静的解析エンジンがワークフロー保存時に自動実行されれば、構造的バグのあるワークフローがプロダクションに到達しない。これは12名のチームでもスケールする品質ゲート。

**結論**: Shinganは「AIエージェントワークフローのESLint」として、実行前品質ゲートを提供する。

### 市場タイミングの根拠

ESLintの初版リリースは2013年。それ以前にもJSLint（2002年）、JSHint（2011年）があり、JavaScriptの静的解析は約10年かけて成熟した。AIエージェントワークフローの静的解析は2026年にFlowLint（n8n専用）が登場した段階であり、汎用的な解析エンジンは未出現。カテゴリ形成期の初期プレイヤーとしてポジションを取れる。

## 結果

- AIエージェントワークフロー静的解析という新カテゴリでOSSを公開する
- 解析対象はADK-Goを初期ターゲットとし、アダプター層でn8n / Dify / SamuraiAIへの展開を可能にする
- 面接先（Kiva）に対して「事業課題からアーキテクチャを逆算できる人材」としてのシグナルを送る

### トレードオフ

- 静的解析は「実行時の非決定的挙動」は検出できない（LLMの出力は毎回異なる）。ランタイム観測（LangSmith的）との併用が前提。
- ワークフロースキーマが非公開のプラットフォーム（SamuraiAI）に対しては、アダプターの実装にリバースエンジニアリングまたは公式連携が必要。

---

<a id="adr-002"></a>
# ADR-002: 解析対象フレームワークの選定 — なぜADK-Goか

## ステータス
Accepted

## コンテキスト

Shinganが解析対象とするワークフロー定義フォーマットを選定する必要がある。候補は以下。

| フレームワーク | 言語 | ワークフロー定義形式 | オープン性 | GoとのI/F |
|---|---|---|---|---|
| LangGraph | Python | Python StateGraph API | OSS | なし（Python） |
| CrewAI | Python | Python DSL | OSS | なし（Python） |
| AutoGen | Python/.NET | Python/C# | OSS | なし |
| ADK (Python) | Python | Python Agent/Workflow classes | OSS | なし（Python） |
| ADK-Go | **Go** | **Go Agent/Workflow structs** | **OSS** | **ネイティブ** |
| n8n | TypeScript | JSON | OSS | JSON parse可 |
| Dify | Python | YAML/JSON | OSS | JSON parse可 |
| SamuraiAI | 非公開 | 非公開 | **クローズド** | 不明 |

## 選択肢

### 選択肢A: ADK-Go
Google Agent Development Kitの公式Go実装。SequentialAgent、ParallelAgent、LoopAgentによるワークフロー定義をGoの構造体として直接解析できる。

### 選択肢B: n8n JSON
JSONファイルとしてワークフローを解析。FlowLintとの直接比較が可能だが、「AIエージェント固有」の解析ルールとの接点が薄い。

### 選択肢C: LangGraph Python
最も普及したエージェントワークフローフレームワークだが、Python実装のみ。Go製ツールから解析するにはAST解析またはIR変換が必要。

## 決定

**選択肢Aを採用する。**

## 根拠

### 技術的根拠

**前提1**: ShinganはGoで実装する（ADR-003参照）。解析対象もGoネイティブであれば、パーサーの実装コストが最小化される。

**前提2**: ADK-GoはGoogleが2025年にOSSとして公開し、2026年4月時点でアクティブに開発が進んでいる。SequentialAgent、ParallelAgent、LoopAgentの3つのワークフローエージェントタイプが定義されており、SamuraiAIの14ノード型と構造的に対応する。

**因果関係**: ADK-Goの構造体をGoのreflectパッケージまたはAST解析で直接読み取れるため、中間表現への変換が不要。解析精度が最も高くなる。

### アーキテクチャ上の根拠（Onion Architectureとの整合）

Shinganのドメイン層は「ワークフローグラフ」という抽象を扱う。具体的なフレームワーク（ADK-Go、n8n、SamuraiAI）のパース処理はインフラ層のアダプターが担当する。

この設計により:
- 初期ターゲットはADK-Goアダプターのみ実装
- n8nアダプターを追加すればFlowLintの上位互換になる
- SamuraiAIアダプターはKiva入社後に社内スキーマへのアクセス権を得て実装

**面接での説明**: 「SamuraiAIの内部スキーマは非公開なので、同じワークフローパターンを持つGoogle ADK-Goで実証しました。Onion Architectureの依存逆転により、解析ルール（ドメイン）はフレームワークに依存しません。SamuraiAI対応はインフラ層のアダプターを差し替えるだけです。」

### SamuraiAI ↔ ADK-Go の構造マッピング

| SamuraiAI ノード | ADK-Go 対応 | 解析観点 |
|---|---|---|
| ワークフロー全体 | SequentialAgent | ノード実行順序、到達可能性 |
| ループノード | LoopAgent | 終了条件の存在、最大反復数 |
| 条件分岐 | Conditional edges | 分岐網羅性、dead branch |
| LLMノード | LlmAgent | モデル選定の妥当性、コスト推定 |
| ブラウザ操作 / コネクタ | Tool | エラーハンドリングの有無、タイムアウト |
| 承認/レビュー | Callback（before_agent_callback） | Human-in-the-loop の配置妥当性 |
| エージェント | LlmAgent（自律） | 再帰深度制限、ツール呼出上限 |
| パラメータ抽出 | Tool + structured output | 出力スキーマの型整合 |
| ナレッジベース検索 | Tool（RAG） | PII漏洩経路 |

### GCPとの親和性

ADK-GoはVertex AI Agent Engineにネイティブデプロイ可能。GCPクレジット20万円の活用と整合する。テスト用エージェントをCloud Run上で実行し、Shinganによる解析結果と実行結果の比較検証（解析精度の評価）が可能。

## 結果

- ADK-Goを初期解析対象とする
- WorkflowGraphインターフェースをドメイン層に定義し、ADK-Goパーサーはインフラ層に配置
- 将来のn8n / Dify / SamuraiAI対応はアダプター追加で実現

### トレードオフ

- ADK-Goはエコシステムとしてまだ初期段階。利用者母数はLangGraphより小さい。
- LangGraph対応を後回しにすることで、Python系ユーザーへのリーチが遅れる。ただし、LangGraphワークフローのJSON/YAML exportが今後提供される可能性があり、その場合はパーサー追加で対応可能。

---

<a id="adr-003"></a>
# ADR-003: アーキテクチャ設計 — Go + goa + Onion Architecture + Factory Pattern

## ステータス
Accepted

## コンテキスト

面接先（Kiva）の技術スタックは以下。

- サーバーサイド: Go, Echo, goa, OpenAPI
- 設計思想: Onion Architecture, Factory Pattern
- インフラ: AWS, Google Cloud, Cloudflare, Datadog, Sentry
- フロントエンド: TypeScript, React, Next.js, Tailwind CSS, shadcn/ui, Zustand

Shinganの技術選定は、面接先スタックとの整合性を考慮しつつ、プロダクトの要件に対して技術的に正当な判断を行う。

## 決定事項

### 3-1: プログラミング言語 → Go

**根拠**:
- ADK-Goのワークフロー定義をネイティブに読み取れる（同一言語）
- グラフ走査アルゴリズム（DFS、BFS、サイクル検出）の実装がGoの型システムと相性が良い
- 複数の解析ルールを並行実行する際、goroutineとchannelが自然にフィットする
- シングルバイナリ配布（CLI）、コンテナ化（API）の両方に対応
- 面接先のサーバーサイド主要言語

### 3-2: APIフレームワーク → goa（Echo不採用）

#### 選択肢

| | goa | Echo |
|---|---|---|
| API設計アプローチ | Design-first（DSL → コード生成） | Code-first（手書き） |
| OpenAPI生成 | 自動（DSLから生成、ドリフトゼロ） | 手動（swaggo等で注釈ベース） |
| 型安全性 | DSLレベルで保証 | ランタイムバリデーション依存 |
| 学習コスト | 高（DSL習得が必要） | 低（シンプルなルーティング） |
| コード生成量 | 多い（transport層が自動生成） | 少ない |

#### 決定: goaを採用

**根拠**:

**前提1**: Shinganの解析結果APIは、CI/CDパイプラインや他ツール（SamuraiAI本体等）から呼び出される。APIスキーマの正確性と安定性が重要。

**前提2**: goaのDSLからOpenAPIが自動生成される。これにより「APIドキュメントと実装のドリフト」がゼロになる。SamuraiAIのプラットフォーム側がShinganを統合する際、OpenAPIからクライアントSDKを自動生成できる。

**因果関係**: Design-firstアプローチにより、API契約の一貫性が構造的に保証される。これはShinganのような「他システムに組み込まれる部品」にとって、Code-firstアプローチより優位。

**Echoを不採用とする理由**: Echoはミドルウェア拡張が容易でプロキシ系に強いが、Shinganはプロキシではない。API契約の厳密性がより重要であり、goaが適切。

### 3-3: アーキテクチャ → Onion Architecture

#### 層構造

```
┌─────────────────────────────────────────────────┐
│                    Ports（外殻）                   │
│  HTTP Handler（goa generated）/ CLI / CI Plugin   │
├─────────────────────────────────────────────────┤
│              Infrastructure（インフラ層）            │
│  ADKGoParser / N8nParser / LLMClient /            │
│  JSONReporter / SARIFReporter / CloudSQL          │
├─────────────────────────────────────────────────┤
│              Application（ユースケース層）           │
│  AnalysisOrchestrator / RuleEngine /              │
│  ReportGenerator                                  │
├─────────────────────────────────────────────────┤
│              Domain（ドメイン層、最内殻）             │
│  WorkflowGraph / Node / Edge / AnalysisRule /     │
│  Finding / Severity / RuleResult                  │
└─────────────────────────────────────────────────┘
```

#### 各層の責務と依存方向

**ドメイン層**（外部依存ゼロ）:
- `WorkflowGraph`: ノードとエッジで構成されるグラフ構造。フレームワーク非依存。
- `Node`: ノードの抽象表現。型（LLM / Tool / Control / Human）、入出力スキーマ、設定。
- `Edge`: ノード間の接続。条件付きエッジを含む。
- `AnalysisRule`: 解析ルールのインターフェース。`Analyze(graph *WorkflowGraph) []Finding`
- `Finding`: 検出された問題。重要度、該当ノード、説明、修正提案を含む。
- `Severity`: Critical / Warning / Info の3段階。

**ユースケース層**（ドメイン層のみに依存）:
- `AnalysisOrchestrator`: 複数のAnalysisRuleを並行実行し、結果を集約する。goroutine + channelで並行化。
- `RuleEngine`: ルール登録・フィルタリング・優先度管理。
- `ReportGenerator`: Findingのリストからレポートを生成する（フォーマットはインフラ層に委譲）。

**インフラ層**（ユースケース層のインターフェースを実装）:
- `ADKGoParser`: ADK-Goのエージェント定義を読み取り、WorkflowGraphに変換する。`WorkflowParser`インターフェースを実装。
- `N8nParser`（将来）: n8nのJSON定義を読み取る。同じインターフェースを実装。
- `JSONReporter`: FindingをJSON形式で出力。`ReportFormatter`インターフェースを実装。
- `SARIFReporter`: FindingをSARIF形式で出力（GitHub Code Scanning統合用）。同じインターフェースを実装。
- `LLMClient`: コスト推定ルールで使用するモデル料金情報の取得。

**ポート層**（エントリポイント）:
- goa生成HTTPハンドラ: API経由での解析リクエスト受付
- CLIコマンド: ローカル実行、CI/CDパイプライン組込
- （将来）CI Plugin: GitHub Actions / GitLab CI統合

#### 依存方向の原則

```
Ports → Infrastructure → Application → Domain
         ↑                    ↑
         └── implements ──────┘
         （インフラ層がユースケース層の
           インターフェースを実装する）
```

ドメイン層は何にも依存しない。ユースケース層はドメイン層のインターフェースのみに依存する。インフラ層がユースケース層で定義されたインターフェースの具象実装を提供する。これがOnion Architectureの依存逆転原則。

### 3-4: Factory Pattern の適用箇所

Factory Patternは「実行時の状態や設定に基づいて、適切な具象実装を生成する」責務を分離する。Shinganでは以下の3箇所で適用する。

#### Factory 1: AnalyzerFactory（解析ルール生成）

```go
// domain/analyzer.go
type AnalysisRule interface {
    Name() string
    Analyze(graph *WorkflowGraph) []Finding
}

// infrastructure/factory/analyzer_factory.go
type AnalyzerFactory struct{}

func (f *AnalyzerFactory) Create(ruleType string) (AnalysisRule, error) {
    switch ruleType {
    case "cycle_detection":
        return NewCycleDetector(), nil
    case "unreachable_node":
        return NewReachabilityChecker(), nil
    case "missing_error_handler":
        return NewErrorHandlerChecker(), nil
    case "cost_estimation":
        return NewCostAnalyzer(), nil
    case "security_pii_leak":
        return NewPIILeakScanner(), nil
    case "redundant_llm_call":
        return NewRedundantLLMDetector(), nil
    default:
        return nil, fmt.Errorf("unknown rule type: %s", ruleType)
    }
}

func (f *AnalyzerFactory) CreateAll() []AnalysisRule {
    return []AnalysisRule{
        NewCycleDetector(),
        NewReachabilityChecker(),
        NewErrorHandlerChecker(),
        NewCostAnalyzer(),
        NewPIILeakScanner(),
        NewRedundantLLMDetector(),
    }
}
```

**なぜFactoryか**: 新しい解析ルールの追加はFactoryに1行追加するだけ。ユースケース層のAnalysisOrchestratorは`AnalysisRule`インターフェースのみに依存し、具象ルールを知らない。ルールの追加・削除がOrchestratorに影響しない。

#### Factory 2: ParserFactory（ワークフローパーサー生成）

```go
// application/parser.go (interface definition in usecase layer)
type WorkflowParser interface {
    Parse(input []byte) (*WorkflowGraph, error)
    SupportedFormat() string
}

// infrastructure/factory/parser_factory.go
type ParserFactory struct{}

func (f *ParserFactory) Create(format string) (WorkflowParser, error) {
    switch format {
    case "adk-go":
        return NewADKGoParser(), nil
    case "n8n":
        return NewN8nParser(), nil    // 将来実装
    case "samurai":
        return NewSamuraiParser(), nil // Kiva入社後に実装
    default:
        return nil, fmt.Errorf("unsupported format: %s", format)
    }
}
```

**なぜFactoryか**: フレームワーク非依存設計の要。「どのパーサーを使うか」の判断をFactoryに集約することで、新しいフレームワーク対応がドメイン層・ユースケース層に一切影響しない。面接で「Onion Architectureの依存逆転をFactoryで実現した」と具体的に説明できるポイント。

#### Factory 3: ReporterFactory（出力フォーマット生成）

```go
// application/reporter.go
type ReportFormatter interface {
    Format(findings []Finding) ([]byte, error)
    ContentType() string
}

// infrastructure/factory/reporter_factory.go
type ReporterFactory struct{}

func (f *ReporterFactory) Create(format string) (ReportFormatter, error) {
    switch format {
    case "json":
        return NewJSONReporter(), nil
    case "sarif":
        return NewSARIFReporter(), nil  // GitHub Code Scanning用
    case "markdown":
        return NewMarkdownReporter(), nil
    default:
        return nil, fmt.Errorf("unsupported format: %s", format)
    }
}
```

**なぜFactoryか**: SARIF出力はGitHub Code Scanning統合に必須。JSON出力はAPI応答用。Markdown出力はCLI / 人間可読用。出力先が変わっても解析ロジックは一切変わらない。

### 3-5: 並行処理設計

AnalysisOrchestratorは複数の解析ルールをgoroutineで並行実行する。

```go
// application/orchestrator.go
func (o *AnalysisOrchestrator) Analyze(graph *WorkflowGraph, rules []AnalysisRule) []Finding {
    findings := make(chan []Finding, len(rules))
    var wg sync.WaitGroup

    for _, rule := range rules {
        wg.Add(1)
        go func(r AnalysisRule) {
            defer wg.Done()
            findings <- r.Analyze(graph)
        }(rule)
    }

    go func() {
        wg.Wait()
        close(findings)
    }()

    var allFindings []Finding
    for f := range findings {
        allFindings = append(allFindings, f...)
    }

    sort.Slice(allFindings, func(i, j int) bool {
        return allFindings[i].Severity > allFindings[j].Severity
    })

    return allFindings
}
```

**なぜgoroutineか**: 解析ルールは互いに独立（各ルールはWorkflowGraphを読み取り専用でアクセスし、副作用を持たない）。6つのルールを直列実行すると合計200ms以上かかりうるが、並行実行で最遅ルールの時間に収束する。これはGoの並行処理モデルの最も自然な適用例。

## 結果

- Go + goa + Onion Architecture + Factory Pattern の構成でShinganを実装する
- goaのDSLからOpenAPIを自動生成し、API契約の一貫性を構造的に保証する
- Factory Patternを3箇所（Analyzer / Parser / Reporter）に適用し、拡張ポイントを明確化する
- goroutineで解析ルールを並行実行し、Goの並行処理モデルの強みを活かす

---

<a id="adr-004"></a>
# ADR-004: インフラストラクチャ設計 — GCP構成

## ステータス
Accepted

## コンテキスト

利用可能なクラウドクレジットはGoogle Cloud 20万円分。AWSは選択肢外。ADK-GoはGCPネイティブ（Vertex AI Agent Engine）であり、GCPとの親和性が高い。

## 決定

### 構成

```
┌─────────────────────────────────────────────┐
│                  Cloud Run                    │
│  ┌──────────┐    ┌──────────────────────┐   │
│  │ Shingan  │    │ ADK-Go Test Agent    │   │
│  │ API      │    │ (解析対象サンプル)      │   │
│  └──────────┘    └──────────────────────┘   │
├─────────────────────────────────────────────┤
│  Cloud SQL (PostgreSQL) — 解析結果の永続化    │
│  Artifact Registry — コンテナイメージ管理      │
│  Cloud Build — CI/CD                         │
└─────────────────────────────────────────────┘
```

### コスト見積

| サービス | 月額概算（円） | 備考 |
|---|---|---|
| Cloud Run (Shingan API) | 3,000 | 0.5 vCPU, 512MB, 低トラフィック想定 |
| Cloud Run (Test Agent) | 2,000 | 検証用、常時起動不要 |
| Cloud SQL (PostgreSQL) | 5,000 | db-f1-micro, 10GB |
| Artifact Registry | 500 | コンテナイメージ保管 |
| Cloud Build | 0 | 無料枠内（120分/日） |
| **合計** | **約10,500/月** | **20万円 ≈ 19ヶ月運用可能** |

### なぜCloud Runか

- コンテナベースでサーバーレス。Goのシングルバイナリとの相性が良い。
- スケールtoゼロにより、低トラフィック時のコストが最小。
- ADK-GoもCloud Runへのデプロイが公式ドキュメントでサポートされている。

### CLIとしての配布

Shinganは API だけでなく CLI としても動作する。GoのクロスコンパイルによりLinux/macOS/Windows向けバイナリを生成し、GitHub Releasesで配布する。CI/CDパイプラインでの利用はCLIが主。

```bash
# ローカル実行
shingan analyze --format adk-go --input ./agents/ --output report.json

# CI/CD統合（GitHub Actions）
- uses: hatyibei/shingan-action@v1
  with:
    format: adk-go
    path: ./agents/
    fail-on: critical
```

## 結果

- GCP（Cloud Run + Cloud SQL）で20万円クレジット内に収まる構成
- API（goa）+ CLI（cobra）のデュアルインターフェース
- ADK-Goテストエージェントを同一GCP環境にデプロイし、解析精度の検証が可能

---

<a id="adr-005"></a>
# ADR-005: 実装スコープとスケジュール — 2日間の集中開発

## ステータス
Accepted

## コンテキスト

最終面接は2026年4月17日（木）。

- **4/14（火）夜 〜 4/15（水）朝**: ADR完成（本文書）
- **4/15（水）**: Agentic開発で全力ビルド。モック + PoCが動く状態まで持っていく
- **4/16（木）**: 動作確認、修正、デモ準備。未完でもいい
- **4/17（木）**: 最終面接

方針: **完璧なコードより、動くデモ。** モックとPoCを回すことに全力を注ぐ。

## 決定: ビルド順序

Agentic開発（Claude Code / AgenticTeam）に渡すタスクの実行順序。依存関係を考慮し、**前のタスクが終わらないと次が始められない**順に並べる。

### Phase 1: 骨格（最優先、ここが動かないと何も始まらない）

| # | タスク | 成果物 | 依存 |
|---|---|---|---|
| 1 | Go module初期化 + Onion層ディレクトリ構造 | `go.mod`, パッケージ構成 | なし |
| 2 | ドメイン層: WorkflowGraph, Node, Edge, Finding, Severity, AnalysisRule interface | `domain/` | #1 |
| 3 | テスト用WorkflowGraph手動構築ヘルパー（ADK-Goパーサーなしで解析ルールをテストできるようにする） | `domain/testutil/` | #2 |
| 4 | 解析ルール #1: CycleDetector | `domain/rules/cycle.go` + `_test.go` | #2, #3 |
| 5 | 解析ルール #2: ReachabilityChecker | `domain/rules/reachability.go` + `_test.go` | #2, #3 |
| 6 | 解析ルール #3: ErrorHandlerChecker | `domain/rules/errorhandler.go` + `_test.go` | #2, #3 |

**Phase 1完了条件**: `go test ./domain/...` が全部通る。グラフ構造を手動で作って3つのルールがバグを検出できる。

### Phase 2: 組み上げ（解析パイプライン完成）

| # | タスク | 成果物 | 依存 |
|---|---|---|---|
| 7 | AnalyzerFactory | `infrastructure/factory/analyzer.go` | #4, #5, #6 |
| 8 | AnalysisOrchestrator（goroutine並行実行） | `application/orchestrator.go` + `_test.go` | #7 |
| 9 | JSONReporter + MarkdownReporter | `infrastructure/reporter/` | #2 |
| 10 | ReporterFactory | `infrastructure/factory/reporter.go` | #9 |
| 11 | CLI: `shingan analyze` コマンド（cobra）— ファイル入力 → 解析 → JSON出力 | `cmd/shingan/` | #8, #10 |

**Phase 2完了条件**: `shingan analyze --input testdata/buggy.json` でJSON結果が出力される。この時点ではADK-Goパーサーなしで、手動定義のJSON入力。

### Phase 3: ADK-Go接続（PoCの核）

| # | タスク | 成果物 | 依存 |
|---|---|---|---|
| 12 | ADK-Goパーサー: ワークフロー定義 → WorkflowGraph変換 | `infrastructure/parser/adkgo.go` | #2 |
| 13 | ParserFactory | `infrastructure/factory/parser.go` | #12 |
| 14 | ADK-Goテストエージェント（意図的にバグを仕込んだサンプル3パターン） | `testdata/agents/` | なし（並行可） |
| 15 | E2Eテスト: ADK-Goサンプル → Shingan解析 → 期待通りのFinding | `e2e_test.go` | #11, #12, #14 |

**Phase 3完了条件**: 実際のADK-Goエージェント定義に対してShinganが解析結果を出力する。**これがデモの核。**

### Phase 4: API + 追加ルール（時間があれば）

| # | タスク | 成果物 | 依存 |
|---|---|---|---|
| 16 | goa DSL定義: `/analyze` エンドポイント | `design/` | #8 |
| 17 | goa codegen → OpenAPI spec生成 | `gen/`, `openapi.json` | #16 |
| 18 | HTTP API実装（goa generated handlers + Orchestrator接続） | `cmd/api/` | #17, #8 |
| 19 | 解析ルール #4: CostAnalyzer | `domain/rules/cost.go` + `_test.go` | #2, #3 |
| 20 | 解析ルール #5: RedundantLLMDetector | `domain/rules/redundant.go` + `_test.go` | #2, #3 |
| 21 | README.md（アーキテクチャ図、使い方、解析ルール一覧） | `README.md` | 全部 |

**Phase 4は未完でもいい。** CLIでADK-Goのバグを検出するデモ（Phase 3）が動けば面接で戦える。goa APIとOpenAPIは「設計は完了、実装途中」でもADRで語れる。

### 面接日（4/17 木）の最低ラインと理想ライン

**最低ライン（Phase 3完了）**:
- CLI: `shingan analyze --format adk-go --input ./testdata/agents/`
- 3つの解析ルールが動いてバグを検出
- ADR文書で設計判断を説明可能
- 「goa APIは設計完了、OpenAPI specはDSLから生成済み、HTTP実装は進行中」と説明

**理想ライン（Phase 4完了）**:
- 上記 + HTTP APIが動く
- 5つの解析ルール
- OpenAPI spec公開
- READMEとアーキテクチャ図

### 面接後の拡張ロードマップ（参考）

| Phase | 期間 | 内容 |
|---|---|---|
| v0.1 | MVP（今回） | ADK-Go対応、3-5ルール、CLI（+ API） |
| v0.2 | 入社後1ヶ月 | n8nパーサー追加、SARIF出力、GitHub Actions統合 |
| v0.3 | 入社後3ヶ月 | SamuraiAIアダプター（社内スキーマアクセス）、セキュリティルール（PII漏洩経路） |
| v1.0 | 入社後6ヶ月 | LangGraphパーサー（Python AST経由）、Difyパーサー、マルチフレームワーク対応の安定版 |

## 結果

- 水曜日1日で「動くPoC」を完成させる
- 面接ではデモ（CLIでバグ検出）+ 設計判断の筋（ADR）+ 市場空白の発見プロセスを語る
- 未完の部分は入社後ロードマップで「SamuraiAIへの適用パス」を具体的に示す
- **完璧なコードより、動くデモ。未完でもストーリーが通ればいい。**

---

<a id="adr-006"></a>
# ADR-006: ESLint方式 visitor + selector + listener 採用

## ステータス
Proposed (2026-05-04) — Phase 0 完了後 v0.6.0 で実装予定

## コンテキスト

v0.5.0 時点で Shingan の各 `AnalysisRule` は `Analyze(graph *WorkflowGraph) []Finding` 1つの interface で動いており、各ルールが独立して graph 全体を for-range で走査している。

```go
// 現状の各ルール実装パターン (domain/rules/*.go)
func (r *DeprecatedModelRule) Analyze(g *WorkflowGraph) []Finding {
    for _, node := range g.Nodes() { /* check node.Model */ }
}
func (r *TemperatureMisuseRule) Analyze(g *WorkflowGraph) []Finding {
    for _, node := range g.Nodes() { /* check node.Temperature */ }
}
```

問題点:

1. ルール数 N に対して走査回数 N回 → 計算量 **O(N × (V+E))**
2. ルール 10 → 25 に拡張 (Phase 2) すると解析時間が線形悪化
3. LSP 統合時 (ADR-009) typing latency が許容外 (200-500ms)
4. 新ルール追加時に毎回 graph walk 実装が必要 → ルール作成コスト高い

## 検討対象

| 選択肢 | 内容 | 評価 |
|---|---|---|
| **A** | 現状維持 (各ルール独立 walk) | スケーラビリティ不足 |
| **B** | ESLint 完全クローン (CSS-like 文字列 selector DSL + visitor) | DSL parse/補完/error表示/互換性が v0.x で全て沼る |
| **C** | ESLint **思想**だけ採用 (型付き selector struct + 1walk dispatcher) | Go型安全と相性、最小実装で最大効果 |

## 決定

**選択肢 C を採用する。**

走査は `application/walker.go` (新規) に集約し、各ルールは `Listener` interface を返す。

```go
// domain/visitor.go (新規)
type NodeHandler func(ctx *RuleContext, node *Node)
type EdgeHandler func(ctx *RuleContext, edge *Edge)

type Listener struct {
    OnNode  map[NodeType]NodeHandler  // NodeType.LLM 等で限定発火
    OnEdge  EdgeHandler                // 全 edge で発火
    OnGraph func(g *WorkflowGraph)     // walk 後の集計用
}

type Selector struct {
    NodeTypes  []NodeType   // 反応する Node 種類 (string DSL は使わない)
    Predicates []Predicate  // Inside, FanIn, EdgeFrom 等の構造的フィルタ
}

type Rule struct {
    Meta   RuleMeta
    Create func(ctx *RuleContext) Listener
}
```

ルール側 (ESLint 風記法):

```go
// domain/rules/deprecated_model.go (refactor 後)
func DeprecatedModel() Rule {
    return Rule{
        Meta: RuleMeta{Name: "deprecated_model", Severity: Warning, Fixable: true},
        Create: func(ctx *RuleContext) Listener {
            return Listener{
                OnNode: map[NodeType]NodeHandler{
                    NodeTypeLLM: func(c *RuleContext, n *Node) {
                        if isDeprecated(n.Model) {
                            c.Report(Finding{
                                Node: n,
                                Message: fmt.Sprintf("%s is deprecated", n.Model),
                                Confidence: 1.0,
                                ConfidenceReason: ReasonExactStaticMatch,  // ADR-008
                                AutoFix: &TextEdit{Range: n.SourcePos, NewText: latest(n.Model)},
                            })
                        }
                    },
                },
            }
        },
    }
}
```

## 根拠

**A 却下**: スケーラビリティ不足。25 ルール時に 70ms (推定) → C 方式で **20-25ms**。LSP typing latency も同様に改善。

**B 却下**: ESLint の esquery (CSS-like 文字列 selector) は強力だが、Go 上で同等の DSL を作ると parse / 補完 / error表示 / 互換性 / ドキュメント生成 が v0.x で全て沼る。ESLint 内部 API も型付きで、esquery の文字列 selector はオプション機能。Go の型安全性を活かして **コンパイル時に selector 妥当性を検証**できる方が圧倒的に得。

**C 採用**: 型付き struct なら IDE 補完が効き、ルール作成コストが激減。selector 表現力が将来不足したら拡張する (YAGNI)。

## 性能予測

| 指標 | 現状 | ESLint方式後 |
|---|---|---|
| 25 ルール時の graph walk回数 | 25 | **1** (Local) + 数回 (Path/Global) |
| 1000 ノード解析時間 (推定) | ~70ms | **20-25ms** |
| ルール追加コスト | graph walk 実装必要 | listener 関数 1個 |
| LSP typing latency (cache hit) | 200-500ms | **10-30ms** |

## 結果

- **新規**: `domain/visitor.go` (Listener/Selector/Rule定義), `application/walker.go` (1walk dispatcher)
- **書き換え**: 既存 `domain/rules/*.go` 10 ルール全部 (Local 5 / Path 4 / Global 4 の3層に再分類、ADR-007 参照)
- **テスト維持**: 全 298 テストを green に保ったまま refactor (別ブランチ `refactor/visitor-pattern` で分離実装)

## トレードオフ

- 大規模 refactor → Phase 0 (ブランチ収穫) と並行で進めると複雑度高い、別ブランチで隔離
- selector の表現力が ESLint esquery より制限あり → 必要が出たら拡張、現状の 7 NodeType + 数個の Predicate で十分

---

<a id="adr-007"></a>
# ADR-007: Local / Path / Global の3層ルール分離

## ステータス
Proposed (2026-05-04) — **本ADR群で最重要**

## コンテキスト

ADR-006 で 1walk dispatcher を採用するが、`cycle` / `reachability` / `pii_leak` のような全域解析ルールは 1walk では成立しない。

当初は「Local rule (1walk) と Global rule (専用パス)」の **2 分類** で計画していたが、別 AI レビューで重要な指摘を受けた:

> ESLint はコード AST が対象なので Local rule が大半。一方 Shingan は workflow graph が対象で、PII leak / prompt injection sink / cost estimation / error handler 等の **「限定的経路を見るルール」** が多い。これを Local rule に押し込むと selector が過剰複雑化、Global rule に押し込むと O(V+E) で済むものが O(V²) になる。

つまり Shingan は **Graph Linter** であり、ESLint clone ではない。ESLint 外形コピーは失敗パターン。

## 決定

**Local / Path / Global の3層に分離する。**

| 層 | 判定範囲 | 走査方式 | 計算量 | v0.5 該当ルール (10個) |
|---|---|---|---|---|
| **🟢 Local** | 1 node または 1 edge メタデータ | 1walk + listener (ADR-006) | O((V+E) + N×const) | `deprecated_model` `secret_exposure` `temperature_misuse`*<br>`loopguard` `redundant_llm_call` |
| **🟡 Path** | source → sink, loop内 subgraph, 直接近傍 | 限定経路解析 (逆BFS, subgraph抽出) | O(sinks × (V+E)) 最悪 | `pii_leak` `errorhandler`<br>`cost_estimation` (loop内LLM) |
| **🔴 Global** | graph 全域必要 | graph 全域 1pass | O(V+E) | `cycle` `reachability`<br>`max_parallel_branches` |

\* `temperature_misuse` は Phase 2 新ルール、現状未実装

Phase 2 新ルール (10個追加予定) の振り分け:
- **Local 追加**: `unbounded_tool_arg` `model_card_mismatch` `dynamic_node_construction` `missing_eval_dataset` `secret_in_prompt_template`
- **Path 追加**: `prompt_injection_sink` `retry_storm` `eval_missing` `circular_dep_agents`
- **Global 追加**: なし (graph 全域は既存 3 ルールで十分)

最終構成: **Local 10 / Path 7 / Global 3 = 20 ルール**

## 実装

```go
// domain/local_rule.go (新規) — ADR-006 の Listener 形式
type LocalRule interface {
    Meta() RuleMeta
    Listener(ctx *RuleContext) Listener
}

// domain/path_rule.go (新規)
type PathRule interface {
    Meta() RuleMeta
    Sources(g *WorkflowGraph) []*Node      // 起点抽出 (例: PII source ノード)
    Sinks(g *WorkflowGraph) []*Node        // 終点抽出 (例: external API ノード)
    Propagate(ctx *PathContext) []Finding  // 経路解析本体
}

// domain/global_rule.go (新規)
type GlobalRule interface {
    Meta() RuleMeta
    AnalyzeGlobal(g *WorkflowGraph) []Finding
}
```

実行順序 (`application/orchestrator.go` 改修):

```
Pass 1: Global rules を goroutine 並列実行
        (cycle/reachability の結果を Pass 3 が利用するため最初)
Pass 2: 1walk + Local rule listener 発火 (ADR-006)
Pass 3: Path rules を goroutine 並列実行
        (Pass 1 の reachability 情報を再利用、無駄な探索回避)
Merge:  Sort by Severity DESC → Confidence DESC → RuleName ASC
```

## 根拠

1. **計算複雑度の型強制**: 各層で異なる walker を持つことで、ルール作成時に「どの計算特性に属するか」が型で強制される。間違って Path rule を Global にしてしまう事故を回避。
2. **Path rule 共通インフラの再利用**: `pii_leak` `prompt_injection_sink` 等は taint propagation の同じ仕組みを使う → `application/path_walker.go` に集約。
3. **Global → Path への移行余地**: 将来 reachability を Path rule の起点拡張で代替可能になった場合の移行が綺麗。
4. **ESLint 外形コピー回避**: Shingan の本質は Graph Linter であり、Local 主役の ESLint と異なる。3層化で本質を反映。

## 結果

- **新規**: `domain/{local_rule,path_rule,global_rule}.go` interface 定義
- **新規**: `application/{walker,path_walker,global_walker}.go`
- **書き換え**: 既存 10 ルールを 3 層のいずれかに振り分けて refactor

## トレードオフ

- ESLint には無い 3 分類 → 学習コストやや上がる、`docs/rule-authoring.md` で補う
- 実装複雑度が 2 分類より高い → ADR-010 で外部 Plugin SDK 公開を v1.0 まで defer する判断と整合

---

<a id="adr-008"></a>
# ADR-008: Confidence × ConfidenceReason 二次元品質管理

## ステータス
Accepted — Confidence は v0.4 で実装済、ConfidenceReason を Phase 0 で追加

## コンテキスト

v0.4 で全 Finding に Severity (Critical/Warning/Info) × Confidence (0.0-1.0) の 2 次元属性を導入済み。`--min-confidence` CLI フラグで閾値調整可能。

しかし Confidence は単一数値のため意味が曖昧化しやすい。別 AI レビューで指摘:

> Confidence には少なくとも3種類ある:
> 1. **解析確度** — 静的にどれくらい確実に言えるか
> 2. **影響確度** — 本当に問題につながる可能性
> 3. **ルール成熟度** — そのルール自体の信頼度
>
> これを全部 `0.5` に混ぜると、ユーザーが解釈できなくなる。

特に動的グラフの over-approximation (LangGraph の `conditional_edges` で戻り値型 untyped → 全 reachable nodes に保守的 edge) を confidence 0.5 で記録する場合、ユーザーは「何が 0.5 の根拠か」を知りたい。

## 決定

**`ConfidenceReason` enum を Finding に追加する。**

```go
// domain/finding.go (修正)
type ConfidenceReason string
const (
    ReasonExactStaticMatch        ConfidenceReason = "exact_static_match"        // 1.0 推奨
    ReasonOverApproximatedDynamic ConfidenceReason = "over_approximated_dynamic" // 0.5 推奨
    ReasonParserFallback          ConfidenceReason = "parser_fallback"           // 0.4 推奨
    ReasonExperimentalRule        ConfidenceReason = "experimental_rule"         // 0.6 推奨
    ReasonHeuristicPattern        ConfidenceReason = "heuristic_pattern"         // 0.3-0.7
)

type Finding struct {
    Severity         Severity
    Confidence       float64
    ConfidenceReason ConfidenceReason  // ← 追加
    Node             *Node
    Message          string
    Suggestion       string
    AutoFix          *TextEdit
    // ...
}
```

各ルールは Finding 生成時に Reason を必ず指定する (`go vet` 風の static check で未指定を検出)。

## 出力例

LSP hover / SARIF properties / Markdown reporter で Reason を表示:

```
[WARN] LangGraph conditional edge target dynamic
  confidence: 0.5
  reason: over_approximated_dynamic
  detail: "conditional_edges fn returns str (untyped); all reachable nodes were
          conservatively connected as edge candidates. To eliminate this warning,
          annotate return type as Literal['a', 'b', 'c']."
```

SARIF v2.1.0 では `properties.confidenceReason` に格納:
```json
{
  "ruleId": "dynamic_node_construction",
  "level": "warning",
  "properties": {
    "confidence": 0.5,
    "confidenceReason": "over_approximated_dynamic"
  }
}
```

## 根拠

- **解釈可能性**: `--min-confidence=0.7` でフィルタしても、なぜそのルールが落ちたかをユーザーが理解できる
- **ルール作成者へのガイドライン**: Confidence 値を選ぶ時に「どの Reason に該当するか」を考える習慣付け
- **デバッグ容易性**: 偽陽性報告時に Reason を見れば対処方針が分かる (parser 改善 / DSL 拡張 / type annotation 推奨)

## 結果

- **修正**: `domain/finding.go` に `ConfidenceReason` enum と Finding フィールド追加
- **修正**: 既存 10 ルールの Finding 生成箇所で Reason を指定 (refactor 必要)
- **新規**: `infrastructure/reporter/{json,markdown,sarif}.go` で Reason 表示

## トレードオフ

- enum 追加で API 表面が増える → v0.x 期間中は破壊変更可、v1.0 で固定
- 既存ルール 10 箇所の Finding 生成箇所修正必要 → ADR-006 の refactor と同時実施

---

<a id="adr-009"></a>
# ADR-009: LSP 差分実行 + degraded mode

## ステータス
Proposed (2026-05-04) — Phase 0 A-2 で実装

## コンテキスト

phase2plan.md で LSP 統合を Phase 2 のフラッシップとして位置付け (`feat/lsp-server` ブランチ存在)。但し検証で `feat/lsp-server` HEAD = `feat/source-pos` HEAD で **実装は SourcePos foundation のみ、LSP本体ゼロ** が判明。

LSP は IDE 統合の核だが、典型的失敗パターンが存在する:

1. **typing latency**: 毎キー入力で full re-analysis → エディタ体験崩壊
2. **Python parser 起動コスト**: subprocess fork + import で 数百ms、LSP に耐えない
3. **parser crash 時の挙動**: 解析失敗で diagnostic ごと消えると「壊れた」と誤認される
4. **環境差分**: Python 仮想環境/依存解決失敗で起動不能

別 AI レビューで指摘:

> LSP で一番ダメなのは、解析が失敗してエディタ体験ごと壊れること。

## 決定

**LSP は『差分実行 + SHA256 LRU cache + degraded mode』の3層防衛で実装する。**

### 層1: 差分実行 + SHA256 LRU cache

```go
// infrastructure/cache/sha256_lru.go (新規)
type AnalysisCache struct {
    lru *simplelru.LRU  // SHA256(file_content) → []Finding
}

// cmd/shingan-lsp/diagnostics.go (新規)
func (s *Server) didChange(uri string, content string) {
    hash := sha256.Sum256([]byte(content))
    if cached, ok := s.cache.Get(hash); ok {
        s.publishDiagnostics(uri, cached)        // 10-30ms (cache hit)
        return
    }
    graph := s.parse(content)
    findings := s.walker.Walk(graph, s.listeners)
    s.cache.Add(hash, findings)
    s.publishDiagnostics(uri, findings)          // 200-500ms (cold)
}
```

cache size = 512 (SHA256 keyed)。debounce 200ms で連続 typing を抑制。

### 層2: 長寿命 Python subprocess (LangGraph parser)

ADR-006 の C 案を実装する `infrastructure/parser/python_runtime.go`:

```
LSP 起動時:
  1. python_health.Check() — Python 可用性 + langgraph import 確認
  2. 健康なら長寿命 worker fork (`scripts/export_langgraph_server.py`)
  3. JSON-RPC で stdin/stdout 通信
  4. heartbeat 監視 (30秒毎)
```

毎ファイル毎に subprocess fork は **絶対しない**。1セッション = 1 worker。

### 層3: Degraded mode (parser unavailable / crashed)

```
ケース A: Python parser available + healthy
  → 全層 (Local/Path/Global) 解析実行
  → 完全な diagnostic セット publish

ケース B: Python parser unavailable (起動時検出)
  → degraded mode、Local rule のうち Python不要なもののみ実行
  → 例: secret_exposure (regex), deprecated_model (string match)
  → diagnostic に "limited analysis: python not found" 注記表示
  → ユーザーに `pip install shingan-langgraph` を案内

ケース C: Python parser crashed mid-session
  → 直前の cache 結果を保持
  → parser_warning diagnostic 出す (Severity: Info)
  → 再起動 attempt 3回 (exponential backoff: 1s, 5s, 30s)
  → 全部失敗で degraded に降格
```

`infrastructure/parser/python_health.go` (新規) でヘルスチェック実装。

## 根拠

1. **エディタ体験を絶対に壊さない**: degraded でも軽量な検出 (secret/deprecated_model) は出続ける、ユーザーは「Shingan が動いている」と認識できる
2. **環境差分への耐性**: Python 環境が無い CI 環境や Win 環境でも基本検出は動作
3. **cache で typing latency 解決**: 同じファイル内容ならハッシュ一致で即返却

## 結果

- **新規**: `cmd/shingan-lsp/{main,server,diagnostics,hover,codeaction}.go`
- **新規**: `infrastructure/cache/sha256_lru.go`
- **新規**: `infrastructure/parser/python_health.go` `python_runtime.go`
- **新規**: `scripts/export_langgraph_server.py` (long-lived JSON-RPC worker)

## トレードオフ

- degraded mode のドキュメント整備が必要 → `docs/lsp-degraded-mode.md`
- Python crash 時の cache 信頼性 → cache TTL を 1時間に制限
- LSP 起動時間 (Python健康チェック含む) で 1-2秒のオーバーヘッド → 許容範囲

---

<a id="adr-010"></a>
# ADR-010: Plugin SDK internal-first 戦略

## ステータス
Proposed (2026-05-04)

## コンテキスト

ESLint のエコシステム拡大の最大要因は plugin (`eslint-plugin-react` 等)。Shingan も同様に外部ルール開発を可能にしないと検出能力が頭打ち。

当初計画 (plan file ADR-015) では:
- ABI: `init()` 関数による静的 registration
- v0.x 期間中は `experimental:` prefix 必須、no stability promise

但し別 AI レビューで指摘:

> v0.x で Plugin SDK を出すと API 固定圧力が発生する。
> WorkflowGraph / Node / Edge / Finding / Selector / Listener / RuleMeta / SourcePos
> ここはまだ変わるはず。
>
> でも「公開SDK」として見せすぎると、使う人は勝手に期待する。
> 人間は README の `experimental` を読まない。危険物取扱説明書より読まない。

特に ADR-006 (visitor pattern) と ADR-008 (ConfidenceReason) で API 表面が変わる。これを v0.x で公開固定すると後悔する。

## 決定

**Plugin SDK は v1.0 まで internal-only、外部公開は v1.0 以降に限定する。**

| 項目 | 旧計画 | 新方針 |
|---|---|---|
| Plugin 公開タイミング | v0.x で `experimental:` prefix 公開 | **v1.0 まで internal only**、公開は v1.0 以降 |
| v0.x で出すもの | 外部 sample rule (`shingan-rule-template` 別 repo) | **authoring guide のみ** (`docs/rule-authoring.md`) |
| ABI 安定化対象 | `experimental` 注記付きで部分安定 | **全て v1.0 まで破壊変更可能** |
| 内部 registry | `domain/rules/registry.go` | 同左 (内部 builtin rule 用に使用) |

## 実装

```go
// domain/rules/registry.go (新規、internal only)
package rules

var builtinRegistry = make([]Rule, 0)

func registerBuiltin(r Rule) {  // ← 小文字、外部から呼べない
    builtinRegistry = append(builtinRegistry, r)
}

func AllBuiltins() []Rule {  // ← 大文字、内部 application 層から呼ぶ
    return builtinRegistry
}

// domain/rules/deprecated_model.go
func init() {
    registerBuiltin(DeprecatedModel())  // builtin only
}
```

外部から `rules.Register(myRule)` は呼べない。これにより:

1. v0.x で API 安定化圧力が発生しない
2. 内部 builtin rule は同じ visitor パターンで書ける (ADR-006)
3. v1.0 で `Register()` を public 化するだけで外部公開可能

## v0.x 期間中の代替策

外部ルール作成希望者には:

- **`docs/rule-authoring.md`** で内部ルールの実装パターン解説
- **fork して builtin として追加 → upstream PR** の方法を案内
- v1.0 リリース時に `Register()` public 化 + sample plugin repo 公開

## 根拠

1. **負債回避**: ADR-006 の visitor 移行 + ADR-007 の3層分離 + ADR-008 の ConfidenceReason 追加で API 表面が複数回変わる、v0.x で固定すると全部 breaking
2. **ユーザー期待管理**: `experimental` の注釈は人間に読まれない、最初から「外部公開なし」と明確化したほうが誠実
3. **エコシステム形成タイミング**: v1.0 で対応FW (ADR-011) が LangGraph + ADK-Go + 1-2 個の段階で外部ルール需要が顕在化、それまでは builtin 拡充が優先

## 結果

- **新規**: `domain/rules/registry.go` (internal builtin registry)
- **新規**: `docs/rule-authoring.md` (実装パターン解説、外部公開ではなく fork & PR の案内)
- **明示記載**: README に「Plugin SDK は v1.0 で公開予定、v0.x は builtin rule のみ」

## トレードオフ

- 外部 contributor の参入障壁が上がる → 一方で「正規 PR 経由」で品質確保される
- 早期エコシステム形成は遅れる → カテゴリ確立が先、ESLint も plugin 公開は v1.0 後

---

<a id="adr-011"></a>
# ADR-011: 主戦場 LangGraph シフト (ADR-002 補正)

## ステータス
Proposed (2026-05-04) — ADR-002 の補正

## コンテキスト

ADR-002 (2026-04-14) では ADK-Go を初期解析対象とし、その理由として:

- Goネイティブ実装、解析コスト最小
- Vertex AI Agent Engine ネイティブデプロイ可能
- SamuraiAI (Kiva) との構造的対応
- 面接アピール

を挙げた。これは2026年4月時点 (Kiva面接前) の判断として妥当だった。

但し 2026-05-04 のユーザー方針再設定 (「使える静的解析ツール化」優先) と別 AI レビューで:

> ADK-Go はあなたの技術アピールには良い。でもカテゴリ形成には LangGraph の方が強い。
>
> 1. workflow graph という Shingan の思想に一番合う
> 2. Python AI agent 界隈で認知がある
> 3. 動的性もあり、静的解析の価値を説明しやすい
> 4. ADK-Go より市場が広い
> 5. n8n より開発者向け lint 文化に近い

ADK-Go と LangGraph の市場規模差:

| FW | 推定 AI agent開発者シェア (2026年5月) | 静的解析価値 |
|---|---|---|
| LangGraph | 10-15% (Python AI agent の主流の一つ) | 高 (動的グラフ、conditional_edges 等) |
| ADK-Go | 1-2% (Google 押しだが採用限定) | 中 (Go ネイティブで精度高いが対象狭い) |

## 決定

**主戦場を LangGraph に変更する。ADK-Go は 2 番手として維持・強化。**

新しい parser 実装優先順位:

| 順位 | FW | 理由 |
|---|---|---|
| 1 | **LangGraph** | カテゴリ形成、認知広い、動的性で価値説明 |
| 2 | ADK-Go (既存) | Go ネイティブ差別化、Kiva SamuraiAI 連携 |
| 3 | CrewAI | LangGraph の Python shim 再利用 |
| 4 | n8n | JSON-DSL で軽量実装 |
| 5 | Mastra | TS bridge、ROI 判断分岐 |

## 実装

Phase 1 (Reach 拡張) 着手対象を LangGraph 一点突破に変更:

- 新規: `infrastructure/parser/langgraph.go` (Python subprocess wrapper)
- 新規: `scripts/export_langgraph_server.py` (long-lived JSON-RPC worker、ADR-009 の degraded mode と統合)
- 新規: `testdata/langgraph/` (simple_chain / branching / react_loop / rag / multi_agent)
- 修正: `infrastructure/factory/parser.go` で `"langgraph"` ケース追加
- 修正: `cmd/shingan/analyze.go` で `--format=langgraph` 対応
- 新規: `docs/langgraph.md`

ADK-Go parser は引き続き保持・強化:

- ADR-007 の Path rule (PII leak, error handler) を ADK-Go でも動作確認
- Kiva 入社後 SamuraiAI 連携時に ADK-Go の go/types ネイティブ精度を活用

## 根拠

1. **「使えるツール」化との整合**: 2026-05-04 user direction = マーケ defer、実用優先。LangGraph 対応は最大数のユーザーが触れるための前提
2. **ESLint 方式との整合**: ESLint も最初は JS only → 1 エコシステム集中で天下取り。Shingan の最初は LangGraph (Python AI agent エコシステム)
3. **ADR-002 の前提変化**: ADR-002 は面接前提 (Kiva技術アピール) で書かれた、Kiva 入社決定後の今は別判断軸 (市場形成) が優先
4. **動的性の価値説明**: LangGraph の `conditional_edges` 動的グラフ → ADR-007 の Path rule + ADR-008 の ConfidenceReason の価値が最も明確に伝わる

## ADR-002 との関係

ADR-002 を **Superseded** にはしない。理由:

- ADR-002 の Onion + Factory による拡張性確保の判断は今も有効
- Parser は Onion で疎結合 → 「主戦場」が変わっても ADR-002 のアーキテクチャ判断は不変
- 「ADK-Go を 2 番手」とするだけで、捨てるわけではない

## 結果

- Phase 1 着手対象を LangGraph に変更 (plan file `/home/hatyibei/.claude/plans/jiggly-crunching-whistle.md` に反映)
- README の対応マトリクス更新 (LangGraph を Phase 1 primary、ADK-Go を current/maintained と明記)
- ADK-Go テスト・ドキュメントは引き続きメンテ

## トレードオフ

- ADK-Go の Go ネイティブ精度 (面接アピール) が一時的に薄まる → Kiva 入社後 SamuraiAI 連携で再度活用
- Python subprocess 依存が増える → ADR-009 の degraded mode で耐性確保
- LangGraph API 変動 (0.x → 1.x) のメンテコスト → バージョン pinning + 週次 CI で実 LangGraph examples 解析

---

<a id="adr-012"></a>
# ADR-012: multi-file directory analysis — per-file independent graph

## ステータス
Proposed (2026-05-05) — Phase 2 着手の前提条件 (#9 解決案)

## コンテキスト

`shingan analyze --format adk-go --input <directory>` は v0.5 以降、directory 配下の全 `.go` ファイルを 1 つの `WorkflowGraph` に merge する (`parseSourceDirectoryFiltered` at `cmd/shingan/analyze.go:259`)。

self-dogfood (2026-05-04 push 後) で、`testdata/agents/` で**偽陽性 7件**を確認:

```bash
$ /tmp/shingan-bin analyze --format adk-go --input ./testdata/agents
14 findings: critical 4 / warning 8 / info 2
- unreachable_node × 7 (偽陽性、各 file の独立 agent)
```

**testdata/agents/** の 3 file は本来別々の独立 agent definition:
- `infinite_loop.go` — `LoopAgent retry_loop`
- `missing_handler.go` — `LlmAgent planner + tools`
- `unreachable.go` — `SequentialAgent orchestrator`

merge 後の graph は entry を 1 つしか持たないため、最初に encountered された entry (= `retry_loop`) から到達できない他 file の node がすべて `unreachable_node` Warning/Info として誤検出される。これは Phase 2 で `unreachable_node` 派生ルール (例: `dead_branch`) を増やすほど偽陽性が乗算される構造。

別 AI レビュー観点 (Codex iter1 P1) との整合: 「diff-mode は graph semantics を変えるな」と同根の問題で、**file merge も graph semantics に問題がある**。

## 検討対象

| 案 | 内容 | 偽陽性 | cross-file ref | domain変更 | 実装コスト |
|---|---|---|---|---|---|
| **A** | Per-file independent graph + multi-graph reporter | ✅ 根絶 | ❌ できない | なし | 中 |
| **B** | Multi-entry single graph (`EntryNodeIDs []string`) | ✅ 根絶 | ✅ 可能 | あり (大) | 高 |
| **C** | Opt-in merge flag (`--merge-files`) | △ default改善 | ✅ flag次第 | なし | 中-高 |
| **D** | Smart cross-file resolution (`go/packages` 横断 var) | ✅ 根絶 | ✅ 自動 | なし | 非常に高 |

## 決定

**A 案を採用する。**

各 `.go` ファイルを個別に parse → 別 `WorkflowGraph` として保持 → Orchestrator を file ごとに走らせ → findings を `SourceFile` 属性付きで集約。

## 根拠

1. **「使えるツール」化の最優先** (2026-05-04 user direction): 偽陽性根絶 > 高度な cross-file 解析。Phase 2 で新ルール 10 個追加する前にここを直さないと偽陽性が乗算される
2. **domain 不変** = 既存 10 ルール (Local 4 / Path 3 / Global 3) への影響ゼロ。ADR-007 の3層分離 refactor を破壊するリスクなし (B 案最大の懸念回避)
3. **testdata/agents で偽陽性 7件 → 0件** が確実に達成可能 (regression test で gating)
4. **cross-file reference の需要は未検証**: 実プロジェクトで「複数 file にまたがる単一 agent definition」のケースが顕在化したら D 案として後年昇格 (新規 issue で track)
5. **Reporter 拡張は局所**: SARIF / Markdown / JSON すべてに `source_file` メタを足すだけで済む (1 日仕事)

## 影響

### 変更スコープ (見積もり: 6-8 ファイル, ~300 LOC)

- `cmd/shingan/analyze.go`: directory モードを per-file parse + 集約に置換
- `cmd/shingan-mcp/tools.go` の `loadGraph`: 同様
- `application/orchestrator.go`: `AnalyzeMulti(graphs []GraphWithSource, rules []AnalysisRule) []Finding` 追加 (既存 `Analyze` は維持)
- `domain/finding.go`: `SourceFile string` field 追加 (Pos.File と冗長だが directory モードで明示的に file 単位 attribute)
- `infrastructure/reporter/{json,markdown,sarif}.go`: `source_file` 属性出力
- 新規テスト: `TestAnalyze_MultiFileDirectory_NoSpuriousUnreachable` (testdata/agents で 0 件 unreachable_node)

### 変更しないもの (重要)

- `domain/graph.go` の `WorkflowGraph` (EntryNodeID は単数のまま)
- 10 既存ルール (Pos.File ベースで動くまま)
- LangGraph parser の directory モード (`--format=langgraph` で .py 再帰スキャン): こちらも将来 A 適用すべきだが別 PR / 別 issue

### 後続 (本 ADR 範囲外)

- LangGraph directory parse の per-file 化 → `[Bug] LangGraph directory analysis has same multi-file merge issue as #9` issue 切る
- D 案 (smart cross-file resolution) は v1.0 以降の拡張として `[Enhancement] cross-file ADK-Go agent reference resolution` 新規 issue
- Reporter Markdown table に `Source` 列追加 (CLI UX 改善)

### トレードオフ

- **失うもの**: cross-file agent reference (file1 の Sequential が file2 の var を `SubAgents` に持つケース) を統合 graph 上で解析できない。実プロジェクトでこのパターンが顕在化したら D 案へ移行
- **得るもの**: 偽陽性根絶 / domain 安定性 / 実装コスト最小 / Phase 2 着手の前提条件クリア

---



| 用語 | 定義 |
|---|---|
| 静的解析 | プログラムやワークフローを実行せずに、その構造を解析して問題を検出する手法 |
| DAG | Directed Acyclic Graph（有向非巡回グラフ）。ワークフローの理想形だが、ループノードがある場合は巡回グラフになる |
| DFS | Depth-First Search（深さ優先探索）。サイクル検出に使用 |
| BFS | Breadth-First Search（幅優先探索）。到達可能性解析に使用 |
| SARIF | Static Analysis Results Interchange Format。GitHub Code Scanningが採用する静的解析結果の標準フォーマット |
| Finding | 静的解析で検出された個々の問題 |
| Severity | 問題の重要度。Critical（ワークフロー実行が確実に失敗する）/ Warning（失敗する可能性がある）/ Info（改善推奨） |
| Confidence | 静的解析の確度。0.0-1.0 の連続値、`--min-confidence` フラグで閾値調整可能 (ADR-008) |
| ConfidenceReason | Confidence 値の根拠を示す enum (`exact_static_match` / `over_approximated_dynamic` 等)、ADR-008 で導入 |
| ADK-Go | Google Agent Development Kit の Go実装。エージェントワークフローをGoの構造体で定義する |
| LangGraph | LangChain の workflow graph フレームワーク。`StateGraph` API で node/edge を定義、Python製。Shingan v0.6 主戦場 (ADR-011) |
| goa | GoのDesign-first Webフレームワーク。DSLからサーバー、クライアント、OpenAPIを自動生成 |
| Visitor pattern | AST/Graph を 1walk して node 種別ごとに listener を発火する走査方式。ESLint で採用、Shingan v0.6 で導入 (ADR-006) |
| Selector | Visitor pattern で listener が反応する node 条件を表す型付き構造体 (NodeTypes + Predicates)、ESLint の esquery 文字列DSLは採用しない (ADR-006) |
| Listener | Selector に該当する node が来た時に発火する handler 関数群 (`OnNode` `OnEdge` `OnGraph`) (ADR-006) |
| Local rule | 1 node または 1 edge メタデータで判定可能な軽量ルール、1walk dispatcher で実行 (ADR-007) |
| Path rule | source → sink、loop内、直接近傍など限定経路を見るルール、Shingan の主役 (ADR-007) |
| Global rule | graph 全域必要なルール (cycle/reachability等)、専用パスで実行 (ADR-007) |
| Degraded mode | LSP で Python parser unavailable/crashed 時に Local rule のうち軽量検出のみ動作させるモード (ADR-009) |
| LSP | Language Server Protocol、IDE と言語サーバの通信プロトコル。Shingan v0.6 で stdio JSON-RPC 実装 |
| MCP | Model Context Protocol、Claude Desktop / Cursor 等から外部ツール呼出する Anthropic 標準 |

---

<a id="appendix-b"></a>
# Appendix B: Generic GUI Workflow ↔ ADK-Go ノードマッピング（詳細）

> 注: 元は SamuraiAI 想定で書かれた表ですが、任意の GUI ワークフローエディタ (n8n / Dify / Voiceflow / 自社 SaaS) を ADK-Go の WorkflowGraph 構造に正規化するときの参考テンプレートとして再利用できます。「14 ノード型」は当時の SamuraiAI 想定スキーマで、汎用化する場合は自社のノード定義と読み替えてください。

| SamuraiAI | ノード型 | ADK-Go | ADK-Go型 | マッピング根拠 |
|---|---|---|---|---|
| ワークフロー全体 | 制御 | SequentialAgent | Workflow | 直列実行のオーケストレーション |
| ループ | 制御 | LoopAgent | Workflow | 反復実行、終了条件による脱出 |
| 条件分岐 | 制御 | Conditional edges | Graph | 条件に基づくルーティング |
| LLM | AI | LlmAgent | Agent | LLM呼出による推論 |
| エージェント | AI | LlmAgent (autonomous) | Agent | 自律的なツール選択と実行 |
| 自動判定 | AI | LlmAgent (classification) | Agent | Intent分類 |
| パラメータ抽出 | AI | LlmAgent + structured output | Agent | 構造化データ抽出 |
| ナレッジベース検索 | AI | Tool (RAG) | Tool | 外部知識の検索と取得 |
| ブラウザ操作 | 外部 | Tool (browser) | Tool | 外部システムとのインタラクション |
| 外部連携コネクタ | 外部 | Tool (API) | Tool | 外部API呼出 |
| MCP Tool | 外部 | Tool (MCP) | Tool | MCP経由のツール呼出 |
| コード実行 | 外部 | Tool (code_execution) | Tool | カスタムロジック実行 |
| 承認/レビュー | 人間 | Callback (before_agent) | Callback | Human-in-the-loop |
| 回答を出力 | 出力 | Agent response | Output | 最終出力 |
| メモ | なし | (対応なし) | - | 設計時のみ、実行時無視 |

---

<a id="appendix-c"></a>
# Appendix C: 解析ルール詳細仕様

## Rule 1: CycleDetector（サイクル検出）

**検出対象**: 終了条件のない無限ループ

**アルゴリズム**: DFS（深さ優先探索）によるback edge検出。LoopAgentのmax_iterations設定の有無を確認。

**Severity**:
- max_iterations未設定のループ → Critical
- max_iterationsが100以上のループ → Warning
- 正常なループ → 検出しない

**実装概要**:
```
1. グラフのエントリポイントからDFSを開始
2. 訪問済みノードを3状態で管理: 未訪問 / 処理中 / 完了
3. 処理中のノードに再到達した場合、サイクルを検出
4. サイクル内にLoopAgentが含まれる場合、max_iterations設定を確認
5. LoopAgentでないサイクル（グラフ定義の誤り）はCritical
```

## Rule 2: ReachabilityChecker（到達不能ノード検出）

**検出対象**: エントリポイントから到達できないノード

**アルゴリズム**: エントリポイントからのBFS（幅優先探索）。訪問されなかったノードが到達不能。

**Severity**:
- 到達不能なLLM/Toolノード → Warning（実装の無駄）
- 到達不能なメモノード → Info（意図的な可能性）

**実装概要**:
```
1. グラフのエントリポイントからBFSを実行
2. 全ノードの訪問状態を記録
3. BFS完了後、未訪問ノードを到達不能として報告
4. ノード型に応じてSeverityを判定
```

## Rule 3: ErrorHandlerChecker（エラーハンドリング欠落検出）

**検出対象**: 外部I/Oを伴うノードの直後にエラーハンドリングフローがないケース

**対象ノード型**: Tool（ブラウザ操作、コネクタ、MCP Tool、コード実行）

**アルゴリズム**: 対象ノードの出力エッジを確認。条件分岐ノードへの接続がない場合、エラーハンドリング欠落と判定。

**Severity**:
- ブラウザ操作ノード後のエラーハンドリング欠落 → Critical（GUI操作は最も失敗しやすい）
- 外部APIコネクタ後のエラーハンドリング欠落 → Warning
- コード実行ノード後のエラーハンドリング欠落 → Info

## Rule 4: CostAnalyzer（コスト推定）

**検出対象**: LLMノードのモデル選定が過剰なケース

**ロジック**:
```
1. LLMノードのモデル設定を取得（GPT-4o, GPT-4o-mini, Claude 3.5 Sonnet等）
2. ノードの前後のコンテキストからタスクの複雑度を推定
   - 単純な分類/抽出 → miniモデルで十分
   - 複雑な推論/生成 → 上位モデルが適切
3. ループ内のLLMノードは反復回数 × 単価でコスト推定
4. 推定月間コストが閾値を超える場合に報告
```

**Severity**:
- ループ内のGPT-4oで推定月間コスト$100超 → Warning
- 単純タスクにGPT-4o使用 → Info

## Rule 5: RedundantLLMDetector（冗長LLM呼出検出）

**検出対象**: 同一プロンプトテンプレートで同一モデルを複数回呼んでいるケース

**アルゴリズム**: LLMノードのプロンプト設定をハッシュ化し、重複を検出。変数部分はプレースホルダーとして正規化した上で比較。

**Severity**:
- 同一フロー内で同一プロンプト×同一モデルが2回以上 → Warning
- 並列フロー内での重複 → Info（意図的な可能性）

---

# ADR-013: CrewAI parser — LangGraph PythonWorker 再利用戦略

**ステータス**: Accepted (2026-05-06)

## 背景

v0.7.0 で 5 frameworks (ADK-Go / JSON / Samurai / LangGraph / n8n) 対応に到達した。次の主戦場は **CrewAI** — Python 製 multi-agent framework で、`Crew` × `Agent` × `Task` の 3 概念で構成される。GitHub stars 30k+ (2026-05 時点)、LangGraph 0.50k+ に次ぐ Python マルチエージェント FW として採用が広い。

ADR-006 (多言語フロントエンド戦略) で Python 系は **長寿命サブプロセス + JSON-RPC** で統一すると決めた。ADR-011 で LangGraph parser を実装した際、`infrastructure/parser/python_worker.go` (build tag 分離 unix/windows) と `scripts/export_langgraph_server.py` のパターンを確立済み。CrewAI も Python なのでこのインフラを **再利用** すべきか、**専用 worker** を作るべきかが本 ADR の主題。

## 決定

**LangGraph で確立した PythonWorker インフラを framework-agnostic 化して再利用する。** CrewAI 専用の Python シム (`scripts/export_crewai_server.py`) を新規作成するが、Go 側のサブプロセスラッパは LangGraph と同一の `python_worker.go` を共用する。

### CrewAI → WorkflowGraph IR マッピング

| CrewAI 概念 | Shingan IR | Confidence | ConfidenceReason |
|---|---|---|---|
| `Agent(role=, goal=, backstory=, tools=[T1,T2])` | `NodeTypeLLM` (Config: model/provider/role/goal を保持) | 1.0 | `exact_static_match` |
| `Task(description=, expected_output=, agent=A)` | `NodeTypeTool` (description/expected_output を Config) | 1.0 | `exact_static_match` |
| `Tool` (`@tool` decorator or `BaseTool` subclass) | `NodeTypeTool` (Config["category"]="tool") | 0.8 | `name_heuristic` (tool 種別は名前推定) |
| `Crew(agents=, tasks=, process=Process.sequential)` | `Tasks` 順次 `Edge` で連結、`entry_node_id = Tasks[0]` | 1.0 | `exact_static_match` |
| `Crew(process=Process.hierarchical, manager_llm=)` | manager Agent から各 worker Agent への放射 + 戻り `Edge` | 0.7 | `over_approximated_dynamic` (manager LLM が実行時に worker を選ぶ) |
| Agent.tools の各 Tool | Agent から Tool への `Edge` | 1.0 | `exact_static_match` |
| `delegation=True` で Agent A が他 Agent に委譲 | A → B 双方向 `Edge` (over-approximation) | 0.6 | `over_approximated_dynamic` |

### 実装ポリシー

- **Python シム**: `scripts/export_crewai_server.py` は `crewai.Crew` / `crewai.Agent` / `crewai.Task` を import → object inspection (`getattr` ベース、API tolerant) → JSON 出力。`requirements-shim.txt` に `crewai>=0.50.0` を追記 (Pydantic v2 以降のみサポート)。
- **Go 側**: `infrastructure/parser/crewai.go` で `WorkflowParser` 実装。内部で既存 `PythonWorker.Call("parse_file", {path})` を呼び、JSON を `domain.WorkflowGraph` に変換。NodeType マッピングは上表に従う。
- **Factory**: `infrastructure/factory/parser.go` に `case "crewai":` 追加。エラーメッセージ更新。
- **CLI**: `--format=crewai` を `cmd/shingan/analyze.go` で受理。directory walk は許可 (LangGraph と同様、`.py` 走査)。

## 結果

### 利点

- LangGraph 用に書いた worker (subprocess management, JSON-RPC framing, degraded mode, build-tag 分離) を 100% 流用 → 実装コスト 50% 減
- Python 環境セットアップが LangGraph と共通 (`pip install langgraph crewai`) → ユーザー側のインストール手間も同じ
- `python_worker.go` の degraded mode (Python が不在/壊れた場合の Info diagnostic) が CrewAI でも自動的に効く

### 欠点

- LangGraph と CrewAI の Python シムを **同じ Python プロセスで共存** させない (1 worker = 1 framework)。format ごとに別プロセスを spawn するため、混在ディレクトリでは LangGraph と CrewAI の auto-detect は v0.9 以降に defer
- CrewAI の Pydantic v1 (`<0.40.0`) は非対応。古い CrewAI を使うユーザーは v0.50.0 以降にアップグレードを強制
- `Process.hierarchical` の保守的解析は manager Agent から全 worker への edge を張るため、`circular_dep_agents` ルールが偽陽性気味になる可能性 → confidence 0.7 で gate、`--min-confidence=0.8` でユーザー側抑制可能

### 検出可能性

CrewAI で発火する builtin rule:
- `cycle_detection` — `delegation` 経路の循環
- `circular_dep_agents` — Agent A.tools=[B], B.tools=[A] パターン
- `loop_guard` — Crew(max_iter=) 未設定
- `eval_missing` — Agent.tools に code-execution Tool がある場合
- `prompt_injection_sink` — Agent.backstory or Task.description に user input 経路
- `temperature_misuse` — Agent.llm の temperature 設定不一致
- `model_card_mismatch` — Agent.llm の model 名と provider 不整合

既存 20 ルールがそのまま動く想定。CrewAI 固有の追加ルールは v0.9 以降。

## 代替案 (却下)

### 代替案 A: CrewAI 専用 worker を新規実装 (`crewai_worker.go`)

- 利点: Python シムごとにラッパも分離されるので、依存関係が明確
- 欠点: subprocess 管理ロジックの二重実装、build-tag 分離も二重メンテ → ROI 低
- → 却下

### 代替案 B: Python シム 1 つに LangGraph + CrewAI 両対応

- 利点: 1 worker で複数 framework 切り替え可能 (実行時 dispatch)
- 欠点: Python シムの責務が肥大化、`pip install langgraph crewai` 両必須 (LangGraph しか使わないユーザーにも CrewAI インストール強制)、import 時間が伸びる
- → 却下 (1 worker = 1 framework の単純さを優先)

### 代替案 C: AST ベースの静的解析 (Python シム不要)

- 利点: Python ランタイム不要 (`go-python` や CGo 不要)
- 欠点: CrewAI の Pydantic 動的属性, `@tool` decorator のメタプログラミングが追えない → 解析精度が CrewAI の半分以下に劣化
- → 却下 (LangGraph で既に「ランタイム inspect」を選択した同じ理由)

## Out of Scope (v0.8 では非対応)

- **Sub-crew (`Crew` を Agent.tools に渡すパターン)**: nested Crew は v0.9 以降。今は `tool` 単独 Tool 扱い
- **Knowledge / Memory / Cache レイヤー**: 実行時 RAG は `Config["memory"]=true` のメタデータのみ、ルールトリガー無し (ADR-016 defer 方針に従う)
- **Custom Process** (`process=custom_process_fn`): 動的 Process は static 解析対象外、Critical で警告 (新ルール `dynamic_process_construction` は v0.9 候補)

## 検証

`testdata/crewai/` に 5 fixture:
- `simple_crew.py` — 1 Agent + 1 Task の最小例 (clean)
- `sequential_pipeline.py` — 3 Agent + 3 Task の Process.sequential
- `hierarchical.py` — manager + 2 workers の Process.hierarchical
- `multi_tool.py` — 1 Agent + 3 tools (eval_missing 発火想定)
- `circular_delegation.py` — A.tools=[B], B.tools=[A] (circular_dep_agents 発火想定)

公式 `crewAIInc/crewAI-examples` から 3-4 パターンを weekly CI で回し、known best-practice 違反を検出できているか確認 (LangGraph と同形)。

---

# 変更履歴

| 日付 | 変更内容 | 変更者 |
|---|---|---|
| 2026-04-14 | 初版作成。ADR-001〜005、Appendix A〜C | hatyibei |
| 2026-04-14 | ADR-005スケジュール修正。4/15水で全力ビルド、4/16木は動確・修正に変更。Phase 1-4の依存関係ベース実行順序に再構成 | hatyibei |
| 2026-05-04 | ADR-006〜011 追加。「使える静的解析ツール」化方針への転換に伴い、ESLint方式 visitor pattern (006) / Local-Path-Global 3層分離 (007) / ConfidenceReason 二次元化 (008) / LSP差分実行+degraded mode (009) / Plugin SDK internal-first (010) / 主戦場 LangGraph シフト (011, ADR-002 補正) を確定。別AI壁打ちレビューの6盲点指摘を反映。Appendix A に新用語追加 | hatyibei |
| 2026-05-05 | ADR-012 追加。self-dogfood で `testdata/agents` の `unreachable_node` 偽陽性 7件を発見し、multi-file directory analysis の per-file independent graph 化を確定 (#9 解決案、Phase 2 着手の前提条件) | hatyibei |
| 2026-05-06 | ADR-013 追加。v0.7.0 出荷後に CrewAI parser を v0.8 主目標と確定。LangGraph で確立した PythonWorker インフラを framework-agnostic 化して再利用、CrewAI 専用シム `scripts/export_crewai_server.py` を新規追加する戦略を明文化 | hatyibei |
| 2026-05-09 | ADR-014 追加。OSS dogfood (gpt-researcher / crewAI-examples) で実コードの大半が instance method / decorator-driven factory 内構築だと判明 → runtime introspection だけではカバー率 3% 程度。AST-based fallback parser を hybrid 戦略として導入し、LangGraph (StateGraph + add_node/add_edge/add_conditional_edges/set_entry_point) と CrewAI (Agent/Task/Crew constructor + allow_delegation) の両方で AST 抽出を採用。実 OSS hit 率 3% → 37.5% に。 | hatyibei |
