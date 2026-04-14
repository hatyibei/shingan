# Shingan（心眼）

## AI Agent Workflow Static Analyzer — Architecture Decision Records

```
作成者:    hatyibei (中堀広夢)
作成日:    2026-04-14
対象企業:  株式会社Kiva（SamuraiAI）
リポジトリ: github.com/hatyibei/shingan
ステータス: Draft → Implementation Ready
```

---

# 目次

1. [ADR-001: プロダクト選定](#adr-001)
2. [ADR-002: 解析対象フレームワークの選定](#adr-002)
3. [ADR-003: アーキテクチャ設計](#adr-003)
4. [ADR-004: インフラストラクチャ設計](#adr-004)
5. [ADR-005: 実装スコープとスケジュール](#adr-005)
6. [Appendix A: 用語集](#appendix-a)
7. [Appendix B: SamuraiAI ↔ ADK-Go ノードマッピング](#appendix-b)
8. [Appendix C: 解析ルール詳細仕様](#appendix-c)

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

<a id="appendix-a"></a>
# Appendix A: 用語集

| 用語 | 定義 |
|---|---|
| 静的解析 | プログラムやワークフローを実行せずに、その構造を解析して問題を検出する手法 |
| DAG | Directed Acyclic Graph（有向非巡回グラフ）。ワークフローの理想形だが、ループノードがある場合は巡回グラフになる |
| DFS | Depth-First Search（深さ優先探索）。サイクル検出に使用 |
| BFS | Breadth-First Search（幅優先探索）。到達可能性解析に使用 |
| SARIF | Static Analysis Results Interchange Format。GitHub Code Scanningが採用する静的解析結果の標準フォーマット |
| Finding | 静的解析で検出された個々の問題 |
| Severity | 問題の重要度。Critical（ワークフロー実行が確実に失敗する）/ Warning（失敗する可能性がある）/ Info（改善推奨） |
| ADK-Go | Google Agent Development Kit の Go実装。エージェントワークフローをGoの構造体で定義する |
| goa | GoのDesign-first Webフレームワーク。DSLからサーバー、クライアント、OpenAPIを自動生成 |

---

<a id="appendix-b"></a>
# Appendix B: SamuraiAI ↔ ADK-Go ノードマッピング（詳細）

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

# 変更履歴

| 日付 | 変更内容 | 変更者 |
|---|---|---|
| 2026-04-14 | 初版作成。ADR-001〜005、Appendix A〜C | hatyibei |
| 2026-04-14 | ADR-005スケジュール修正。4/15水で全力ビルド、4/16木は動確・修正に変更。Phase 1-4の依存関係ベース実行順序に再構成 | hatyibei |
