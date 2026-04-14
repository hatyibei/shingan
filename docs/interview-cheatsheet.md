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
