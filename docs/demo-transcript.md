# 面接デモ逐語台本

> 2026-04-17 Kiva社最終面接用。
> `scripts/demo.sh` と `shingan-web` を使う8分デモの口頭原稿。
> 読み上げずに「喋る感覚」で頭に入れる。

## 0. オープニング (30秒)

**[画面: 自分のPC、ターミナル+ブラウザ準備済み]**

> 「本日はお時間いただきありがとうございます。hatyibeiと申します。
>
> SamuraiAIのようなGUIでワークフローを組むAIエージェントが、**実行する前に** 無限ループ・コスト爆発・PII漏洩経路を検出できる、Go製の静的解析ツール **Shingan** を作ってきました。今日はこれを8分で見ていただきます」

---

## 1. なぜ作ったか (45秒)

**[画面: shingan-adr.md をエディタで開く or Markdownレンダリング]**

> 「Kivaさんの事業を拝見して、**100%精度を12名のチームで実現する**という制約がある。エンタープライズ顧客のワークフロー全部を手でレビューするのは物理的に無理です。
>
> じゃあ機械検出するしかない。コンパイラがランタイムエラーの前にコンパイルエラーを出すのと同じ発想で、**ワークフローの設計時バグを静的解析で検出する**ツールが要るんじゃないかと考えました。
>
> 調べてみたら、n8n専用のFlowLintとLangSmithのランタイム観測はあるけど、**AIエージェント汎用の実行前静的解析は2026年4月時点で市場空白**。ESLintの立ち上がり期と同じフェーズだと判断して、初期プレイヤーのポジションを取りに行きました」

---

## 2. デモ — 静的解析 (60秒)

**[ターミナルに切り替え、examples/runtime/infinite_loop_unbounded.go を cat]**

> 「まず、ADK-Goで書いたサンプル。LoopAgentでエージェントを繰り返し実行するんですが、**MaxIterationsが書かれていない**。非エンジニアがGUIで組んだとき、こういうのが起きがちです」

**[`./shingan analyze --format adk-go --input examples/runtime/infinite_loop_unbounded.go --output markdown` 実行、結果を指差し]**

> 「Shinganで解析すると一瞬で2件検出されます。
>
> Critical の `loop_guard` が主問題 — LoopAgentにMaxIterations無しで、無限ループの可能性を指摘。
> Warning の `cycle_detection` が副次情報 — そのループの内部で classifier という LLMノードが自己参照してる、と教えてくれる。
>
> **これを止めずに実行すると、Geminiが止まらず呼ばれて請求爆発します**。数千円 → 数万円の事故、SamuraiAIで絶対起きちゃいけない」

---

## 3. デモ — Runner safe-guard (30秒)

**[`./shingan-runner --sample infinite_loop_unbounded --dry-run` 実行]**

> 「次に、このバグありエージェントを実際に実行しようとすると、**Shinganが静的解析を挟んで、Criticalがあったら実行拒否します**。
>
> EXECUTION REFUSED、と。これは面接向けの見せ方だけど、**同じmiddlewareをCI/CDのPull Request段階に組み込めば、本番リリース前に自動で止まります**。SARIF出力対応してるのでGitHub Code Scanningにもそのまま流せます」

---

## 4. デモ — 本番実行 (60秒)

**[`./shingan-runner --sample infinite_loop_bounded` 実行]**

> 「同じループ構造でも MaxIterations=3 を入れた安全版。これはShinganがクリーン判定するので、Runnerが通して、実際にVertex AI Geminiで実行されます」

**[Gemini応答 "1 → 2 → 3 DONE" を指差し]**

> 「3回イテレーションしてDONEで止まる、期待通り。**Shinganで事前OKのエージェントだけが実機実行される** —— これが全体のストーリーです。
>
> コストも確認しておくと、gemini-2.0-flash-001で1デモ0.1円未満。CI予算として全然ペイします」

---

## 5. デモ — ADK Web UI統合 (60秒)

**[ブラウザで localhost:8080 を開く]**

> 「ここからが本題に近いんですが、**ADK Web UI**、Google公式のエージェント開発GUIです。SamuraiAIと同じく、ブラウザでエージェントを選んで会話しながら検証するUIです。
>
> Shinganはこの **`cmd/shingan-web/`** を新規に作って、ADKのRun APIに middleware として割り込んでます」

**[infinite_loop_unbounded を選択、チャット欄にメッセージ入力、送信]**

> 「バグありエージェントに話しかけると……」

**[403エラーの表示を指差し]**

> 「**Shinganの静的解析が走って、Critical検出で実行がブロックされます**。ユーザーには「このワークフローはMaxIterationsが未設定です」とメッセージが返る。
>
> これこそが **SamuraiAIに組み込まれたShinganの完成形イメージ** です。ユーザーがワークフローを保存または実行しようとした瞬間に、Shinganが自動で検証する」

---

## 6. アーキテクチャ (60秒)

**[`docs/architecture.md` または README のアーキ図を見せる]**

> 「設計は **Onion Architecture + Factory Pattern 3箇所**。Kivaさんの技術スタックに寄せています。
>
> 重要なのは、**ドメイン層 (解析ルール7個) がフレームワーク非依存**なこと。ADK-Go, n8n, SamuraiAI、どれに対応するかはインフラ層のアダプターを差し替えるだけです。
>
> 実証として、`infrastructure/parser/samurai.go` に **SamuraiAI用Parser Skeleton** を置いてあります。ADR Appendix Bで SamuraiAIの14ノードを Shingan の NodeType にマッピング済み。入社後に社内スキーマに差し替えるだけでSamuraiAI対応完了、の状態です」

---

## 7. 開発プロセス (30秒)

**[docs/project-stats.md を見せる]**

> 「開発期間は **ADR 1日 + 実装 1.5日、計2.5日**。本体コード6,400行、テスト4,700行、206個のテスト関数、全パッケージ `-race` グリーン。
>
> Claude Code で並列オーケストレーション （各フェーズで3-4エージェント並行）を使って高速化しましたが、アーキテクチャ判断と設計は全部自分でやってます。AIは手足であって頭脳じゃない」

---

## 8. クロージング (15秒)

> 「以上です。GitHubは `hatyibei/shingan` でprivate公開してます。ADR 5件、面接向けの技術ノート、逆質問リストも全部リポジトリに入れています。
>
> 技術質問でも事業質問でも、何でも聞いてください」

---

## 想定される中断ポイントと対処

### パターン1: 「実際にADK-Goで動かした?」 (デモ2の途中で差し込まれる)

> 「はい、`google.golang.org/adk v1.1.0` をimportして、`examples/real/` と `examples/runtime/` に本物SDK準拠のサンプル置いてます。`demo_test.go` で自動検証してます。`functiontool.New(...)` の generic 呼出だけ v0.1 ではAST型情報が取れず検出スキップしてますが、v0.2で `go/types` セカンドパスで解決予定です」

### パターン2: 「ADKってGUIベースでは?」 (デモ5の前に差し込まれる)

> 「ADK自体はコードSDKです。Google公式のADK Web UIはAgentと対話するDev UIで、実行UI。今見ていただいたshingan-webは、そのADK Web UIに**実行前ガードのmiddleware**を挟んだものです。SamuraiAIと同じGUI体験の中にShinganを組み込んだ状態になります」

### パターン3: 「誤検知率は?」 (デモ3の後で)

> 「現状 `testdata/clean.json` で誤検知ゼロを確認していますが、大規模ベンチマークはKivaさんの実ワークフローでやりたいです。v0.3で **信頼度スコア** を導入予定で、「Critical だが信頼度70%」みたいに確信度を別軸で表現する計画です」

### パターン4: 「サーバー常駐させる場合の性能は?」 (デモ5の後で)

> 「v0.1では毎回ファイルをAST解析します。1ファイルあたり10-50ms、Gemini呼出 1-3秒と比較すると1桁以上速い。v0.2で hash-based キャッシュを追加予定。middlewareモード常駐でも数百RPS余裕です」

### パターン5: 「時間足りないからデモ省略して」 (面接官が忙しい場合)

「30秒ピッチ」+「Section 6 アーキ」+「Section 7 開発プロセス」だけに圧縮して2分で締める。

---

## トラブル時の復帰

| 症状 | 対処 |
|---|---|
| ADC切れ | 「すみません、認証切れたので dry-run モードで見ていただけますか」→ `--dry-run` で続行 |
| Vertex AI遅延 | 画面で別タブのスクショ (`scripts/screenshots/`) を見せる |
| shingan-web 起動失敗 | CLI デモだけに絞り込む、Web UI は「後でスクショでお見せします」 |
| Wi-Fi落ち | テザリングに切替。事前に OK確認済 |

---

## 時間配分サマリ

| セクション | 秒 |
|---|---|
| 0. オープニング | 30 |
| 1. なぜ作ったか | 45 |
| 2. 静的解析 | 60 |
| 3. Runner | 30 |
| 4. 本番実行 | 60 |
| 5. Web UI | 60 |
| 6. アーキ | 60 |
| 7. 開発プロセス | 30 |
| 8. クロージング | 15 |
| **合計** | **6分30秒** |

バッファ1分30秒でQ&Aに備える。
