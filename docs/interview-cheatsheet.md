# 面接チートシート — Shingan

> 2026-04-17 Kiva社最終面接用。時間配分・想定Q&A・技術判断の語り方。

## 30秒ピッチ

「Shinganは、AIエージェントのワークフローを **実行前に** 静的解析するGoツールです。無限ループ・到達不能ノード・エラーハンドリング欠落・コスト非効率・冗長LLM呼出を検出します。2026年4月時点で、AIエージェントワークフローの静的解析カテゴリはFlowLint（n8n専用）しか存在せず、汎用エンジンは市場空白。ESLintの立ち上がり期と同じフェーズです。

解析対象はGoogle ADK-Goで実証済み。Onion Architectureで設計してあるので、n8n / Dify / SamuraiAIへの展開はインフラ層のアダプター追加のみです。」

## 3分ピッチ（デモ込み）

1. **問題提起** (30秒): SamuraiAIのような「100%精度」を要求されるエージェントは、設計時バグが本番で致命的になる。12名のチームでエンタープライズ顧客のワークフローを手動レビューは不可能。
2. **市場ギャップ** (30秒): FlowLint（n8n）、LangSmith（ランタイム観測）はあるが、AIエージェント固有の課題（LLMコスト爆発、無限ループ、PII漏洩経路）を**実行前に**静的解析する汎用ツールは未出現。
3. **デモ** (90秒): `bash scripts/demo.sh` で4ステップ
   - Step1: 静的解析でCritical警告
   - Step2: Runnerが警告を受けて実行拒否（safe-guard）
   - Step3: 安全版（MaxIter=3）はクリーン判定で実行成功
   - Step4: Vertex AI Gemini で実際のAgent応答「こんにちは！」
4. **設計** (30秒): Onion Architecture + Factory Pattern 3箇所（Analyzer/Parser/Reporter）+ goroutine並行ルール実行 + goa design-first API。

## 想定Q&A

### 事業・市場観点

**Q1: なぜこのツールを作ろうと思った?**
A: Kivaの事業分析から逆算しました。100%精度と12名のチームという制約で、品質保証を人の注意力に依存させない仕組みが必要。コンパイラがランタイムエラー前にコンパイルエラーを出すのと同じ発想で、ワークフローにも静的解析が必要と考えました。

**Q2: FlowLintとの違いは?**
A: FlowLintはn8nの従来型自動化（Zapier的）専用。**AIエージェント固有の問題**、具体的にはLLMコスト爆発、エージェント推論ループ、冗長LLM呼出はカバーしていません。ShinganはAIエージェントファーストで設計しています。

**Q3: LangSmithやLangfuseでは?**
A: あれらはランタイム観測ツール。Shinganは**実行前の品質ゲート**。役割が違い、併用できます。壊れてから検知するか、壊れる前に防ぐかの違い。

**Q4: SamuraiAIにどう適用する?**
A: `shingan-adr.md` Appendix Bで14ノード全部をADK-Goの型にマッピングしてあります。Onion Architectureなので、SamuraiAIの内部スキーマへのアクセス権を得れば、infrastructure層にSamuraiAIAdapterを1つ追加するだけ。ドメイン層（ルール）は一切変更不要です。

**Q5: なぜOSSで出す?**
A: (1) カテゴリ形成期の初期プレイヤーとしてポジションを取る。(2) ADK-GoコミュニティからのContribution取得。(3) SamuraiAIに統合する場合、OSS基盤の方がエンタープライズ顧客が監査しやすい。

### 技術判断

**Q6: なぜGo?**
A: 3つ理由があります。(1) ADK-GoをネイティブにAST解析できる。(2) goroutineで複数ルールを並行実行するのが自然。(3) Kivaのサーバーサイド主要言語。

**Q7: なぜEchoでなくgoa?**
A: ShinganのAPIは他システム（CI/CD、SamuraiAI本体）から呼ばれる「部品」。API契約の厳密性が最重要です。goaはDesign-firstでDSLからOpenAPIが自動生成され、**スキーマと実装のドリフトが構造的にゼロ**になる。EchoはCode-firstなのでドリフトリスクあり。

**Q8: Onion Architectureの利点は? Cleanで良かったのでは?**
A: 本質的には同じ。Onionを選んだのはKivaの設計思想と合わせたため。ドメイン層（解析ルール）がフレームワークに依存しないこと、Factoryで依存逆転を明示してあることが重要で、ShinganはそれをParser/Analyzer/Reporterの3箇所で実践しています。

**Q9: ADK-GoはGUIベースでは?**
A: ADK自体はコードベースSDK（Python/Go）で、ADK Web UIは補助ツールです。現状のShinganは**コード書いて組むワークフロー**を対象にしています。GUIベースのワークフロー対応はinfrastructure層のアダプターで実現予定（v0.2でn8n、v0.3でSamuraiAI）。今はADK-Goで「Onion Architectureによるフレームワーク非依存性」を実証しました。

**Q10: 実際にADK-Goで動かした?**
A: はい、`google.golang.org/adk v1.1.0`を実際にimportして、`examples/runtime/`で3サンプル、Vertex AI Geminiで実行確認済み。`demo_test.go`で自動検証してます。ただし現状の解析精度には既知の制約があって、`functiontool.New(…)`のgeneric呼出はAST解析では型情報が不足してToolノード識別できない。これは「型情報と連動した次世代parser」としてv0.2で解決予定です。

### 実装の深掘り

**Q11: cycle_detectionのメッセージが "non-Control node" と出るのはなぜ?**
A: LoopAgentの子エージェント（classifier: LlmAgent）がself-loopを形成しているから。`docs/cycle-detection-note.md`に詳細書いてます。仕様として意図的で、LoopAgent管理下のサイクルもユーザーには可視化する方針。ただしSeverity判定は改善余地があり、「LoopAgent階層由来ならWarning扱い」の最適化はv0.2のIssueに立ててあります。

**Q12: 並行処理のrace conditionどう防いだ?**
A: `AnalysisRule`は全て`WorkflowGraph`を**読み取り専用**でアクセスする契約。state持たない。`go test -race ./...`で全パッケージ検証済み。goroutineで起動、`sync.WaitGroup`で待機、`chan []Finding`で結果収集、`sort.SliceStable`でSeverity降順ソート。

**Q13: 開発期間は?**
A: ADR文書が1日、実装が1日、リアルADK-Go統合とランタイムデモで+半日。全部でおおよそ2.5日です。

**Q14: AI（Claude）にどこまで任せた?**
A: アーキテクチャ判断と設計はすべて自分。実装はClaude Codeを並列オーケストレーション（各フェーズで3-4エージェントを同時ディスパッチ）で高速化しました。テストファースト、Onion違反の即修正、`go test -race ./...`グリーン維持、などの守るべきルールはGlobal CLAUDE.mdで明示してあります。

**Q15: コード品質の担保は?**
A: 141テスト関数、全パッケージ`-race`グリーン、`go vet`クリーン、CIはGitHub Actions（lint/test/build）。複雑なロジック（CycleDetectorのDFS、ADK-Go AST解析）は単体テスト8-9ケースずつ。

**Q16: エンタープライズ展開の課題は?**
A: (1) 既存ワークフローへの後付けなので、誤検知率の低さが要件。現状`testdata/clean.json`で誤検知ゼロを確認。(2) SARIF出力でGitHub Code Scanning統合（v0.2）。(3) 信頼度スコア導入で、Severity以外に「どれくらい確信があるか」も示す。

### 事業貢献

**Q17: Kivaに入社したら何をする?**
A: 短期（1-3ヶ月）: SamuraiAIAdapter実装、社内ワークフローで誤検知率ベンチマーク、SARIF出力でGitHub連携。中期（3-6ヶ月）: PIIリークルール、信頼度スコア、CI Plugin。長期（6ヶ月以降）: ランタイム観測機能（LangSmith的）との統合で「設計時検知 + 実行時検知」の両輪提供。

**Q18: SamuraiAIの既存アーキテクチャとの統合ポイントは?**
A: (1) ワークフロー保存時のWebhookでShinganを呼ぶ。(2) 管理画面に「解析結果」タブを追加、Findingを可視化。(3) Criticalなワークフローは公開ブロック（管理者許可でオーバーライド可）。どれもShingan側はAPIで提供するだけなので、SamuraiAI本体を大きく変えずに統合できます。

### ADK Web UI・middleware・最新機能

**Q19: ADK Web UI middleware統合、どう実装した?**
A: `cmd/shingan-web/` に4ファイル構成で実装しました。ADKのルーター生成関数 `web.BuildBaseRouter()` でまずベースルーターを作り、そこに `router.Use(shinganGuardMiddleware(...))` を先に登録してから `apiSL.SetupSubrouters()` で ADK API を追加します。Go の gorilla/mux はUseの順序が効くので、これだけでADKのRunAPI（`/api/run` と `/api/run_sse`）の前にShinganが割り込む。Critical Findingがあると403 + JSON、クリーンなら Vertex AI Gemini にそのままリクエストが届く構造です。middleware ユニットテスト7ケースで動作検証済み。

**Q20: Shinganのmiddlewareって、本番では性能ボトルネックにならない?**
A: v0.1の実装では、解析対象ファイルをリクエストごとにAST解析しています。静的解析なので1ファイルあたり10-50msの範囲で、エージェント実行（Gemini呼出で1-3秒）と比べると1桁以上速い。ただしワークフロー数が増えたときのキャッシュは必要で、v0.2でソースファイルのhash-based cacheを入れる予定です。エンタープライズで「実行前ガード」を挟むコストとしては許容範囲だと考えています。

**Q21: SamuraiAIのParser Stubは実際に使える? どう差し替える?**
A: `infrastructure/parser/samurai.go` に想定スキーマ（ADR Appendix B）ベースのスケルトンが実装済みです。入社後の差し替え手順は3ステップ: (1) `SamuraiWorkflow` 構造体のフィールドを実スキーマに合わせる、(2) `mapSamuraiNodeType` のcaseを実ノード名に更新、(3) `testdata/samurai/` の実JSONで `go test -race ./infrastructure/parser/...` を通す。`domain/` 層・`application/` 層は一切変更しないので、既存の6つの解析ルールがそのままSamuraiAIワークフローに適用されます。変更ファイルは最大2つだけです。

**Q22: functiontool.New で登録したTool、AST解析で認識できない問題、どう解決した?**
A: Q10でも触れましたが、`functiontool.New(myFunc)` はジェネリック呼出なのでASTレベルでは型引数が見えず、ToolノードとしてのIDが取れない問題があります。v0.1では「認識できない場合はスキップ、かつ解析ログにUnknownToolとして記録」する安全側への倒し方で対処しています。根本解決はv0.2で `go/types` パッケージの型情報を使ったセカンドパスASTを組む予定。`go/types` はAST + パッケージ型情報を連動させるので、ジェネリックの型引数まで追跡できます。現時点での誤検知より検知漏れを優先した判断です。

**Q23: SARIF出力とGitHub Code Scanningの統合で何が実現できる?**
A: PRの "Files changed" タブにShinganの警告がインライン表示されます。Shinganのワークフローグラフはソースファイルの行情報を持たないので、`workflow://nodes/<nodeID>` の合成URIを使って、GitHub Security タブにファイルレス注釈として出る形です。Branch Protection ルールで「shinganカテゴリのerror-level結果がゼロでないとマージ不可」に設定すれば、Critical Findingがある状態でのリリースをCIレベルで物理ブロックできます。GitHub Actions の統合ワークフローは `docs/sarif-output.md` に書いてあります。

**Q24: 6つのルール、どれが最も価値が高いと思う?**
A: `loop_guard` が最も直接的なビジネスインパクトがあると思います。MaxIterations未設定のLoopAgentは数百回Gemini APIを呼ぶ可能性があり、数千円〜数万円のコスト事故に直結する。次点で `error_handler_checker`、これはブラウザ操作や外部API呼出が失敗したときのハンドリング欠如を検出する。SamuraiAIが「100%精度」を要件とするなら、エラーが握り潰されているワークフローを事前に発見できることの価値は大きいです。

**Q25: PIIリークルール (v0.3) は具体的にどう検出する?**
A: ロードマップ段階なので設計案を話します。検出したいのは「ユーザー入力のプロンプトが、外部API（コネクタノード）にサニタイズなしで渡るパス」です。データフロー解析でInput節点からOutput節点へのエッジを追跡し、途中にサニタイズ/フィルタリングのToolノードが存在しないパスをFindingとして出す方針。AST静的解析では変数の値追跡が難しいので、ワークフローグラフの構造ベースで「LLM→外部API の直接エッジ」をヒューリスティックで検出するv0.3.1から始める計画です。

**Q26: 信頼度スコアって何? なぜ必要?**
A: SeverityはFindingの重大さを示しますが、「どれくらい確信を持って言えるか」は別の軸です。例えばcycle_detectionは DFS で確定的に検出できるので信頼度100%。一方、cost_estimationはノード構成からLLMコストを推計するので前提仮定が入る、信頼度70%程度。信頼度スコアを付けることで、CIでブロックすべきFinding（信頼度高）と人間レビューを促すFinding（信頼度低）を分けられます。誤検知による「狼少年問題」を防ぐための仕組みです。

**Q27: LangGraph対応(v1.0)、Python ASTをどう扱う?**
A: ロードマップ段階です。GoとPythonのAST形式は異なるので、Python用parserは `infrastructure/parser/` に新しくアダプターを追加する形で対応します。Python ASTのパースには `tree-sitter` を使う案を検討しています。Go側からCGOかRPCでPythonのASTをJSONシリアライズして受け取り、WorkflowGraphに変換する。Onion Architectureなので、Python対応もparser adapterの追加だけでドメイン層は変えなくて済むはずです。

**Q28: Shinganのライセンス(MIT)、商用利用はOK? Kivaで使う時は?**
A: MIT ライセンスは商用利用・改変・再配布すべて可で、著作権表示を残せばOKです。KivaがShinganを社内ツールとして使ったり、SamuraiAIに組み込んで提供する場合も問題ありません。ただしKivaが独自に改変した部分はMITの義務がないのでクローズドにできる。OSSとして育てつつ、Kivaの競争優位部分（SamuraiAI固有のルールセット等）は非公開にするモデルが自然だと思います。

**Q29: 他のワークフロー静的解析ツール(FlowLint等)のOSSコミュニティに逆貢献する予定は?**
A: FlowLintはn8n専用でAIエージェント固有の問題スコープが違うので直接の競合ではない。むしろShinganの「AIエージェント向け静的解析のデータモデル」（WorkflowGraph, Finding, Rule interface）を仕様として公開して、他ツールが実装できるインターフェース定義を提案する方向を考えています。ESLintがlintルールの標準フォーマットを作ってエコシステムを育てたのと同じ発想です。ただしv0.1時点ではまだ自分のコアを固める段階で、コミュニティ活動はv0.5以降の話です。

**Q30: 今回のPoC、一番難しかった部分と、それをどう乗り越えた?**
A: 一番難しかったのはADK Web UIへのmiddleware注入です。ADKの公開ドキュメントはRuntime APIだけで、内部のルーター組立順序はソースを読まないとわからなかった。`router.Use()` の適用範囲が「それ以降に追加されるルートだけ」という gorilla/mux の仕様と、ADKが `SetupSubrouters()` でルートを追加するタイミングの関係を実装で確認して、正しい登録順序を見つけました。「ドキュメントがない部分は実装を読む」と決めて、ADKのソースを1日かけてトレースしたことで突破できました。

## 技術判断の語り方（決めゼリフ）

- **Onion Architecture**: 「ドメイン層が外部に依存しないことで、フレームワーク変更のコストが1つのアダプターに収まる」
- **Factory Pattern**: 「Switch文を1箇所に集中させることで、拡張ポイントが明確。新ルール追加は Factory に1行追加 + ドメインに実装1つ」
- **goa**: 「API契約をコードから生成するのではなく、DSLから生成する。実装がAPI定義を裏切れない構造」
- **goroutine並行**: 「解析ルールがstatelessでsource-of-truthを参照するだけの設計だから、並行化が自然に成立する」
- **ADK-Go選定**: 「Python系のLangGraphと比較してGoのreflectとASTが使えるのが大きい。中間表現への変換が不要」

## デモ中の言い方

- 解析結果を見せるとき: 「このCriticalはLoopAgentにMaxIterations未設定のケース。実行すると数百回Gemini呼ばれて数千円消費する可能性がある。Shinganは1秒未満で検出します」
- runnerのsafe-guardを見せるとき: 「静的解析で警告したバグを、実際に実行しようとしたらruntimeが拒否する。Shingan APIをCI/CDに組み込めば、同じガードがPull Request時に効きます」
- Vertex AI実行を見せるとき: 「ここまで全部Shinganで検証して、クリーンだったAgentだけが実際にGeminiで走る。Before/After が並べて見られます」

## 面接前最終チェック

- [ ] `gcloud auth application-default login` 再実行（ADC 12h有効期限）
- [ ] `go build -o shingan ./cmd/shingan && go build -o shingan-runner ./cmd/runner`
- [ ] `bash scripts/demo.sh --dry-run` 確認
- [ ] `bash scripts/demo.sh` で本番フロー1回（コスト<1円）
- [ ] このチートシートをMac/スマホ両方で開けるようにしておく
- [ ] GCPプロジェクト `axial-mercury-486503-j5` がVertex AI有効か再確認
