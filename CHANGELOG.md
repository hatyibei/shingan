# Changelog

All notable changes to Shingan are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), versioning follows [SemVer](https://semver.org/).

## [Unreleased]

### Added
- **`dynamic_node_construction` rule (Phase 2 #3)** — Local tier (ADR-007)、15 番目の builtin。
  - **走査対象**: Node.Config の curated subset (`body` / `fn` / `handler` / `callback` / `code` / `factory` / `builder`) の文字列値を再帰的に scan
  - **検出パターン**: `eval(` / `exec(` / `Function(` / `compile(` / `__import__(` / `getattr(` / `setattr(` (regex `\bX\s*\(`)
  - **Severity マトリクス**: eval/exec/Function → Critical (0.95, exact_static_match) / compile/__import__ → Warning (0.85, exact_static_match) / getattr/setattr → Info (0.6, heuristic_pattern)
  - **Per-key collapsing**: 1 つの Config 値に複数パターンマッチ時は最高 Severity 1 件に集約 (例: `getattr(...)(eval(...))` → Critical 1件)
  - **Placeholder 抑制**: `${VAR}` / `{{var}}` のみの文字列は skip。Mixed (`eval(${PAYLOAD})`) は strip-then-recheck で eval( が残るため発火 — `secret_exposure_scanner.placeholderPattern` を共有
  - **新規ファイル**: `domain/rules/dynamic_node_construction.go` / `domain/rules/dynamic_node_construction_test.go` (16 ケース) / `testdata/dynamic_construction/{eval,safe}.json` + `README.md` / `docs/rules/dynamic-node-construction.md`
  - **生成 CLI**: `shingan-gen --pattern dynamic-construction` を追加 (`testutil.GenerateDynamicNodeConstructionGraph`)
  - **Factory 自動登録**: `init()` 内で `registerBuiltin()`、`AnalyzerFactory.Create("dynamic_node_construction")` で取得可能
  - **位置付け**: 兄弟ルール `eval_missing` (構造的攻撃面 = LLM → code_execution Tool reachability) と補完関係。文字列レベルの攻撃面を本ルールが、構造レベルを eval_missing が担当
- **`eval_missing` rule (Phase 2 #2)** — Path tier (ADR-007)、14 番目の builtin。
  - **Sources**: `NodeType.LLM` の任意のノード (Severity は path 上の gate に依存するため source 側絞り込みは行わない)
  - **Sinks**: `NodeType.Tool` のうち以下のいずれか — Config["category"] ∈ {code_execution, code_eval} / Config["tool"] ∈ {eval, exec, code_interpreter, python_runner, shell} / Name または ID が `(?i)(eval|exec|code[_]?runner|python[_]?runner|shell|bash)` に部分一致
  - **Severity マトリクス**: 何も挟まない → Critical (0.9, heuristic_pattern) / 中間に NodeType.Condition → Warning (0.6, heuristic_pattern) / 中間に NodeType.Human → skip
  - **アルゴリズム**: 各 source から forward BFS、frontier に `viaCondition` フラグを持たせて per-path Severity を決定。Human ノード以降は展開停止 (PII leak rule の Human-gate 規則と同形)。
  - **新規ファイル**: `domain/rules/eval_missing.go` / `domain/rules/eval_missing_test.go` (15 ケース) / `testdata/eval_missing/{leak,safe}.json` + `README.md` / `docs/rules/eval-missing.md`
  - **生成 CLI**: `shingan-gen --pattern eval-missing` を追加 (`testutil.GenerateEvalMissingGraph`)
  - **Factory 自動登録**: `init()` 内で `registerBuiltin()`、`AnalyzerFactory.Create("eval_missing")` で取得可能
- **`docs/rule-authoring.md`** — internal builtin rule writers 向け実装ガイド (#11 解消)
  - 13 セクション構成: tier フローチャート / Local・Path・Global テンプレート / ConfidenceReason 選択ガイド (ADR-008) / Severity 判断軸 / TDD パターン / 既存 10 ルール設計記録 / `check_confidence_reason.sh` 解説 / 命名規約 / `registerBuiltin` 自動登録 (ADR-010) / v1.0 plugin 移行パス / Phase 2 で追加予定 10 ルールの当てはめ
  - 全 code template を `go build -tags=authoringguide_verify ./domain/rules/...` でコンパイル検証 (Local / Path / Global × `domain.LocalRule` / `domain.PathRule` / `domain.GlobalRule` / `domain.AnalysisRule` の dual-implementation を interface assertion で確認)
  - README.md の "ドキュメント" セクションにリンク追加
  - ADR-010 で確定した「Plugin SDK は v1.0 まで internal-only」方針に沿い、外部公開ではなく **fork → builtin として upstream PR** の経路を案内
- **`model_card_mismatch` Local rule (Phase 2)**
  - LLM ノードで `Config["model"]` (gpt-* / claude-* / gemini-* / o1-* / text-bison* / chat-bison*) と `base_url` / `provider` が矛盾しているケースを検出 — 実行時に確実に 4xx で落ちる
  - 既知 prefix + プロバイダ/URL 不一致 → Critical / 1.0 / `exact_static_match`
  - 既知 prefix + `provider` が一致 (custom base_url 許可) → noop (legitimate proxy / Azure OpenAI / Vertex AI 想定)
  - 未知 prefix + `provider` 設定あり → Info / 0.4 / `heuristic_pattern` (knowledge gap surface、テーブル拡張トリガ)
  - 未知 prefix + `base_url` のみ → noop (false positive 抑制)
  - `domain/rules/model_card_mismatch.go` + 16 テスト (positive 3 / negative 7 / unknown 2 / meta 2 / Reason stamp 1 / nil guard 1)
  - `domain/testutil/generate.go`: `GenerateModelCardMismatchGraph` (gpt-4o on api.anthropic.com) + 3 テスト
  - `cmd/shingan-gen/main.go`: `--pattern model-mismatch` 追加
  - `testdata/model_mismatch/{wrong,correct}.json` + README
  - `cmd/shingan-mcp/explain.go`: `model_card_mismatch` 説明追加 (factory parity 維持)
  - `docs/model-card-mismatch.md`: 検出ロジック / プロバイダテーブル / 例
- **`temperature_misuse` Local rule (Phase 2)**
  - LLM ノードで `temperature > 0` と決定論的タスク (structured_output / extraction / classification / code_generation) が同居しているケースを検出
  - 優先度付き signal 評価: `structured_output=true` または `response_format="json_object"` → Warning / 0.9 / `exact_static_match` → `task=classification` (temp>0.3) / `code_generation` (temp>0) → Warning / 0.7 / `heuristic_pattern` → `task=extraction` / `structured_output` → Info / 0.5 / `heuristic_pattern`
  - `Config["task"]` 不在時は `node.Name` のキーワード (extract / classif / codegen) で fallback
  - `domain/rules/temperature_misuse.go` + 15 テスト (positive 4 / negative 4 / edge 2 / meta 2 / nil guard 1 / 全 finding に ConfidenceReason stamp 1)
  - `domain/testutil/generate.go`: `GenerateTemperatureMisuseGraph` (structured_output + temp 0.7) + 3 テスト
  - `cmd/shingan-gen/main.go`: `--pattern temperature-misuse` 追加
  - `testdata/temperature/{misuse,ok}.json` + README
  - `cmd/shingan-mcp/explain.go`: `temperature_misuse` 説明追加 (factory parity 維持)
  - `docs/temperature-misuse.md`: 検出ロジック / Suggestion / 例
- **`prompt_injection_sink` rule (Phase 2 #1)** — Path tier (ADR-007)、13 番目の builtin。
  - **Sources**: `Config["source"] == "user_input"` ノード、または name/ID が `(?i)^(user[_\-].*|.*[_\-]input|query|request|user_query|user_request)$` にマッチするノード
  - **Sinks**: `NodeType.LLM` で Config に template-like field (system tier: `system_prompt` / `system` / `instruction(s)`、user tier: `prompt_template` / `user_message_template` / `user_template` / `prompt`) を持つノード
  - **Substitution 検出**: `{{var}}` / `${var}` / `{var}` 3 種の構文を正規表現で検出
  - **Severity マトリクス**: system tier × substitution あり → Critical (0.9) / system tier × なし → Warning (0.7) / user tier × あり → Info (0.5)。すべて `ConfidenceReason = heuristic_pattern`
  - **アルゴリズム**: 各 sink から逆方向 BFS、`pii_leak_scanner` の reverse-BFS と同型 (Human-gate 境界は持たない)
  - **新規ファイル**: `domain/rules/prompt_injection_sink.go` / `domain/rules/prompt_injection_sink_test.go` (12 ケース) / `testdata/prompt_injection/{leak,safe}.json` + `README.md` / `docs/rules/prompt-injection-sink.md`
  - **生成 CLI**: `shingan-gen --pattern prompt-injection-sink` を追加 (`testutil.GeneratePromptInjectionSinkGraph`)
  - **Factory 自動登録**: `init()` 内で `registerBuiltin()`、`AnalyzerFactory.Create("prompt_injection_sink")` で取得可能
- **ADR-012: multi-file directory analysis を per-file independent graph に変更 (#9 解決)**
  - self-dogfood で `testdata/agents` の `unreachable_node` 偽陽性 7件を発見、原因は merge 戦略
  - `domain.Finding.SourceFile` field 追加 — directory モード時に file 単位 attribution
  - `application.GraphWithSource` 新規 + `Orchestrator.AnalyzeMulti(inputs, rules)` 追加 (既存 `Analyze` は維持)
  - `cmd/shingan/analyze.go`: directory 経路を `loadAsMulti` + `AnalyzeMulti` に置換
  - `cmd/shingan-mcp/tools.go`: 同様に `loadGraphsAsMulti` + `runAnalysisMulti`、ParseFile 優先
  - `infrastructure/reporter/json.go`: `source_file` / `confidence_reason` フィールド出力 (omitempty)
  - 結果: testdata/agents で 14 findings (Critical 4 / Warning 8 / Info 2) → **6 findings (Critical 4 / Warning 2)** へ偽陽性 8件削減
  - regression test: `TestADKGo_Directory_NoSpuriousUnreachable` / `TestAnalyzeMulti_{StampsSourceFile,EmptyInputs,NilGraphSkipped}`
- ESLint方式 visitor pattern + 3層ルール分離 (ADR-006/007/008/010) — refactor/visitor-pattern ブランチ
  - `domain/visitor.go` — `Listener`/`Selector`/`RuleContext` を新規追加。Listener は `OnNode[NodeType]` / `OnAny` / `OnEdge` / `OnGraph` の 4 ハンドラ束で、走査と判定を分離する。
  - `domain/rule.go` — `LocalRule` / `PathRule` / `GlobalRule` の 3 interface を追加。`AnalysisRule` は **Deprecated** 注記付きで残し、テスト double / 旧 caller は無改修で動く。
  - `domain/finding.go` — `ConfidenceReason` enum (`exact_static_match` / `over_approximated_dynamic` / `parser_fallback` / `experimental_rule` / `heuristic_pattern`) と Finding フィールドを追加 (ADR-008)。
  - `domain/rules/registry.go` — internal-only builtin registry (`registerBuiltin` 小文字、ADR-010 の Plugin SDK internal-first 戦略を反映)。
  - `application/walker.go` — 1walk dispatcher。`graph.Nodes` map を 1 周し、登録された全 LocalRule の listener にディスパッチする。BFS-from-entry ではなく map 走査を採用 (孤立ノードに対するルール検出を維持するため、reachability は別 GlobalRule で担当 — ADR-007 と整合)。
  - `application/path_walker.go` — Path tier 用。reverse adjacency を 1 度だけ構築し、各 PathRule に goroutine で配ってシェアする。
  - `application/global_walker.go` — Global tier 用。各 GlobalRule を goroutine 並列実行する。
  - `application/orchestrator.go` — 3-pass 構成 (Global → Local → Path → legacy fallback) に書き換え。型 assertion で Global > Path > Local > AnalysisRule の優先順に振り分けるので、refactor 済みルールは新パイプラインへ、未対応ルール (テスト double 等) は従来 goroutine fan-out へと自動的に流れる。`Analyze(graph, []AnalysisRule)` の public シグネチャは維持 (CLI / MCP / web / HTTP API 互換)。
  - 10 ルール全部を 3 層へ振り分け & ConfidenceReason 付与
    - **Local (4)**: `deprecated_model` `loop_guard` `redundant_llm_call` `secret_exposure_scanner` (`OnNode[NodeTypeLLM]` / `OnNode[NodeTypeLoop+Control]` / `OnAny` を使い分け)
    - **Path (3)**: `pii_leak_scanner` (Sources=RAG/PII tools, Sinks=external Tool, reverse-BFS) / `error_handler_checker` (Sources=Tool, Sinks=LLM, 2-hop) / `cost_estimation` (Sources=LLM, loop subgraph DFS)
    - **Global (3)**: `cycle_detection` `unreachable_node` `max_parallel_branches`
  - 各ルールが `init()` 内で `registerBuiltin()` を呼び、`AnalyzerFactory.{Create,CreateAll}` は `rules.AllBuiltins()` をスキャンする方式に切り替え (新規ルール追加時にファクトリ編集不要)。
  - `scripts/check_confidence_reason.sh` + Makefile target `check-reason` / `lint` — `domain.Finding{...}` リテラルに `ConfidenceReason` が欠けていないかを CI でチェック (空 sentinel `Finding{}` は除外)。Pure Go では struct field を必須化できないので静的解析で代替 (ADR-008)。
  - 性能: `application/bench_test.go` を 10 ルール builtins セットへ更新。N=1000 ノードで Orchestrator (3-pass + 1walk Local dispatch) **37.9ms** vs 全ルール sequential fallback **85.2ms** (約 55% 削減、目標 25-50% を上回る)。
  - **Backward compatibility**: 既存テスト 355 (subtests 込みで 445) すべて green、`AnalysisOrchestrator.Analyze` シグネチャ不変、`Confidence == 0.0 → 1.0` 正規化ロジック維持、`fakeRule` (`AnalysisRule` のみ実装) も legacy bucket で動作。Walker / Registry の直接ユニットテストは追加せず、Orchestrator 経由の既存テストでカバー (改善余地)。
- `cmd/shingan-lsp` LSP サーバ本実装 (Phase 0 A-2 / ADR-009)
  - stdio JSON-RPC LSP サーバ、`go.lsp.dev/protocol` v0.12 ベース
  - 7 ハンドラ実装: `initialize`, `initialized`, `shutdown`, `didOpen`, `didChange`, `didClose`, `hover`, `codeAction` (残り60+ メソッドは `baseServer` で no-op スタブ化)
  - **層1**: SHA256 LRU 差分キャッシュ (`infrastructure/cache/sha256_lru.go`) — `(format, sha256(content))` 複合キー、512 entries、TTL 1時間、cache hit ≈ 10–30ms / miss ≈ 80–250ms
  - **層2**: 長寿命 Python subprocess の health 接続点 (`infrastructure/parser/python_health.go`) — `python3 --version` + 任意の `import langgraph` 確認、30秒キャッシュ + コマンド/時計の DI で hermetic test
  - **層3**: Degraded mode — Python 不在時に `shingan_degraded_mode` Info diagnostic 自動付与 (現在は通知のみ; Track P で LangGraph 依存ルール導入時に severity 引き上げ)
  - 診断変換 (`diagnostics.go`): `Node.Pos` (1-based) → LSP `Range` (0-based)、未設定時は `(0,0)-(0,1)` フォールバック、`Source = "shingan"` ラベル、`Code = RuleName`
  - Hover (`hover.go`): finding ごとに rule/severity/message/suggestion/confidence% を Markdown でレンダリング、複数 finding が同じ位置にあれば co-located として併記
  - CodeAction (`codeaction.go`): Suggestion を持つ findings を QuickFix として返却 (TextEdit は ADR-008 AutoFix フィールド導入後に追加)
  - テスト 13 本: ユニット (`recordingPublisher` で publishDiagnostics 検証) + integration 1 本 (`bidiPipe` で in-memory duplex pipe を組み protocol.NewServer/NewClient 経由の handshake → didOpen → publishDiagnostics 全往復)
  - `cmd/shingan-lsp` を `.goreleaser.yaml` の 6 番目のバイナリとして追加 (linux/darwin/windows × amd64/arm64)
  - `docs/lsp.md` 新規 — VS Code / Cursor / Neovim / Helix / Zed セットアップ例、診断 shape マッピング表、cache TTL とパフォーマンス特性、degraded mode 解説、ADR-009 への back-link
  - `infrastructure/cache/` 新規パッケージ + 5テスト (format isolation / hit-miss / nil normalize / LRU eviction / TTL expiration)
  - `infrastructure/parser/python_health*.go` 新規 + 6テスト (uninitialized state / healthy / no-binary / unexpected-output / cache hit / status reflection)
  - 新規依存: `go.lsp.dev/protocol` v0.12.0, `go.lsp.dev/jsonrpc2` v0.10.0, `go.lsp.dev/uri` v0.3.0, `go.uber.org/zap` v1.21.0; `github.com/hashicorp/golang-lru/v2` を indirect → direct に昇格
- **LangGraph parser** (Phase 1 主戦場、ADR-011)
  - `infrastructure/parser/langgraph.go` — `WorkflowParser` 実装。`SupportedFormat() = "langgraph"`。`Parse(input []byte)` (in-memory) と `ParseFile(path)` (sys.path resolution 込み) の両系統を提供
  - `infrastructure/parser/python_worker.go` — long-lived Python subprocess wrapper。newline-delimited JSON-RPC、`Setpgid` で process group ごと kill 可能、per-call 30s timeout、goroutine-safe (mutex serialise)
  - `scripts/export_langgraph_server.py` — Python shim (stdlib only)。`langgraph.graph.StateGraph` を import して node/edge/conditional_edges/entry_point を抽出、Shingan WorkflowGraph JSON で返却。stdout 規律 (`sys.stdout = sys.stderr` で stray print を吸収)、langgraph 不在でも degraded で起動継続
  - `scripts/requirements-shim.txt` — `langgraph>=0.2.0`、`pydantic>=2.0`
  - `infrastructure/factory/parser.go` — `case "langgraph"` 追加
  - `cmd/shingan/analyze.go` — `--format=langgraph` フラグ追加、ディレクトリ入力時に `.py` を再帰スキャン (新規 `parseSourceDirectoryFiltered` で `.go`/`.py` を統一処理)、`fileParser` capability 経由で path を直接 parser に渡す経路追加
  - `testdata/langgraph/{simple_chain,branching,react_loop,rag,multi_agent}.py` — 5 リファレンスサンプル + `expected/*.json` ゴールデンファイル
  - `docs/langgraph.md` — 設計、対応機能、Confidence/Reason、トラブルシュート、互換性
  - `infrastructure/parser/python_worker_test.go` / `infrastructure/parser/langgraph_test.go` — protocol tests (Python あれば実行) / integration tests (langgraph 必要、無ければ skip)
  - `cmd/shingan/langgraph_e2e_test.go` — E2E (`shingan analyze --format=langgraph` を subprocess 起動して exit code 検証、langgraph 必要時のみ実行)
  - `conditional_edges` は **over-approximation**: mapping 各 value を `Edge.Condition` 付きで全候補登録 (ADR-008 `over_approximated_dynamic`)
- `extensions/vscode-shingan` VS Code extension MVP (Phase 2-B) — `shingan-lsp` を spawn して diagnostics を表示する LSP client、status bar widget、3 commands (analyze file / analyze workspace / show rules)。`npx vsce package` で `.vsix` 生成可能
- `domain.SourcePos{File, Line, Col}` 構造体追加 — `Node` の optional フィールド `Pos` に付与 (Phase 2 基盤、LSP/CodeAction/VS Code 拡張の前提)
  - `SourcePos.IsZero()` ヘルパー — 位置情報の有無判定規則
  - `domain/graph_test.go`: `TestSourcePos_IsZero` 追加 (6ケース table-driven)
- ADK-Go Parser (`infrastructure/parser/adkgo.go`): 既存 `token.FileSet` から位置を取得して `Node.Pos` に埋め込み
  - `sourcePos(token.Pos) SourcePos` ヘルパーメソッド追加
  - `processAgentLit` / `processRealAPIConfig` / `processToolElement` / `extractRealSubAgents` / `processSubAgent` の全 Node 生成経路で Pos をセット
  - `TestADKGoParser_SourcePos_BareStructLiteral` / `TestADKGoParser_SourcePos_RealAPI` 追加
- JSON Parser: `pos` フィールドが入力に含まれていれば自動デコード (Node.Pos の `json:"pos,omitempty"` タグ経由、Parser 本体は無変更)
  - `TestJSONParser_PreservesSourcePos` / `TestJSONParser_NoPosField_BackwardCompat` 追加
- SamuraiAI Parser: `SamuraiNode.Pos *SourcePos` 追加、入力にあれば保持 (想定スキーマのため optional)
- `docs/source-pos.md` 追加 — 設計意図、IsZero 規則、Parser 別埋め方、LSP/CodeAction との関係
- Phase 2-E 差分モード & progressive adoption (feat/diff-mode)
  - `--since=<git-ref>` CLI フラグ — `git diff --name-only <ref>..HEAD` で得た変更ファイルのみ解析。変更ゼロなら 0 findings で exit 0。
  - `--save-baseline=<path>` CLI フラグ — 現在の findings を baseline JSON として永続化。
  - `--baseline=<path>` CLI フラグ — baseline に含まれる findings を抑止。fingerprint は `(rule, node_id, message)` の組で比較。
  - `--baseline` + `--save-baseline` 併用時は filter 後の findings のみ保存（新規 finding だけを次の baseline に載せる）。
  - `domain/baseline.go` — `Baseline`, `FindingFingerprint`, `Contains`, `Fingerprint`, `NewBaselineFromFindings` を追加（stdlib only, I/O なし）。
  - `infrastructure/baseline/baseline_io.go` — `Save` / `Load` を Onion 原則で infrastructure 層に分離。
  - `action.yml` — `baseline-file` と `since` 入力を追加。既存フローは完全後方互換。
  - `docs/diff-mode.md` — 典型ロールアウトフロー、baseline JSON スキーマ、progressive adoption cookbook。
- `cmd/shingan-mcp` — Model Context Protocol サーバ実装 (Phase 2-C)
  - 公式 SDK `github.com/modelcontextprotocol/go-sdk` v1.5.x を使用、stdio transport
  - Claude Desktop / Cursor / LangGraph Studio / Claude Code / 他 MCP クライアントから呼び出し可能
  - 4 tools 公開:
    - `shingan_analyze_graph(graph_json)` — in-memory JSON graph → `FindingList`
    - `shingan_analyze_file(path, framework)` — ファイル/ディレクトリ (json/adk-go/samurai) → `FindingList`
    - `shingan_explain_rule(rule_name)` — 10ルールの詳細説明 (Severity根拠・例)
    - `shingan_suggest_model(node_description, input_token_estimate)` — ヒューリスティック LLM モデル推奨
  - `docs/mcp-server.md` — 設定方法 (Claude Desktop / Cursor / Studio / Claude Code) と JSON 応答例

### Backward Compatibility
- 既存 testdata (`testdata/**.json`) は `pos` フィールドを持たないまま動作 (`TestJSONParser_NoPosField_BackwardCompat` で gating)
- 既存 consumer (Reporter / Orchestrator) は `Pos` フィールドを参照しないため挙動不変
- 注意: `Pos` は値型 (struct) のため `json:"pos,omitempty"` タグでも `WorkflowGraph` を JSON 出力すると常に `"pos": {...}` キーが出る (空でも `"pos": {}`)。consumer 側で未知フィールドを許容していれば問題ないが、出力サイズ最小化が必要な場合は将来 `*SourcePos` 化を検討
- 新規 CLI フラグ (`--since`, `--baseline`, `--save-baseline`) は省略時は従来挙動
- `action.yml` の `baseline-file` / `since` 入力は省略時 no-op

## [0.5.0] - 2026-04-15

### Added
- `max_parallel_branches` ルール (Issue #1 実装)
  - fan-out >= 100 → Critical (Confidence=1.0)
  - fan-out >= 20  → Warning  (Confidence=0.9)
  - fan-out >= 10  → Info     (Confidence=0.7)
  - `Config["max_concurrency"]` 設定済みノードはスキップ
  - `testdata/parallel/` — high_fanout.json, chunked.json, max_concurrency.json
  - `domain/testutil`: `GenerateHighFanOutGraph(seed, fanout)` 追加
  - `cmd/shingan-gen`: `--pattern high-fanout` オプション追加
  - `docs/parallel-branches.md` — 検出ロジック・ParallelAgent関連・max_concurrency解説
- `deprecated_model` ルール (Issue #2): 停止済み/非推奨LLMモデルを検出
  - `modelShutdown` → Critical (confidence 1.0): 実行時に API エラーが発生するモデル
  - `modelDeprecated` → Warning (confidence 0.9): ~6 ヶ月以内に shutdown 予定のモデル
  - OpenAI 7件 (shutdown) + 2件 (deprecated)、Anthropic 7件 (shutdown) + 1件 (deprecated)、Google 3件 (shutdown)、計20モデルをカバー
  - `testdata/deprecated/`: `shutdown_models.json` (Critical×3)、`deprecated_models.json` (Warning×1)、`active_models.json` (0件)
  - `domain/testutil/generate.go`: `GenerateDeprecatedModelGraph(seed)` 追加
  - `cmd/shingan-gen`: `--pattern deprecated-model` オプション追加
  - `docs/deprecated-models.md`: モデル分類テーブル、マイグレーション推奨先、各プロバイダの公式 deprecation policy リンク

## [0.4.0] - 2026-04-15

### Added
- `Finding.Confidence float64` フィールド追加 (0.0–1.0, `domain/finding.go`)
  - 1.0 = 確定的検出 (DFS back-edge, BFS 到達性など)
  - <0.5 = ヒューリスティック (名前ヒントベース)
- 全8ルールが各検出に Confidence を付与:
  - `cycle_detection`: 1.0 (DFS back-edge検出は確定)
  - `loop_guard`: 1.0 (Config["max_iterations"]の有無は確定)
  - `unreachable_node`: 1.0 (BFS到達性は確定)
  - `error_handler_checker`: 0.8 (2ホップ先ヒューリスティック)
  - `redundant_llm_call`: 0.9 (prompt_template完全一致)
  - `cost_estimation`: 0.7 (モデル価格階層は変動あり)
  - `pii_leak_scanner`: 0.6 (RAGソース) / 0.3 (名前ヒント)
  - `secret_exposure_scanner`: 0.95 (Critical/Warning パターン) / 0.5 (Info汎用パターン)
- `--min-confidence` CLI フラグ (float64, デフォルト 0.0) — 閾値未満の Finding を除外
- Orchestrator ソート順: Severity DESC → Confidence DESC → RuleName ASC
- Orchestrator: Confidence 0.0 を 1.0 に正規化 (後方互換)
- JSONReporter: `findings[*].confidence` フィールド追加、`summary.high_confidence_count` (>=0.9 の件数) 追加
- MarkdownReporter: Confidence 列追加 (例: "95%", "⚠ 30%"=低信頼マーク)
- SARIFReporter: `result.properties.confidence` で拡張フィールドに格納、`rule.properties.precision` ("high"/"medium"/"low") 追加
- `docs/confidence-scoring.md` — 設計思想、各ルール根拠、CI統合例

### Changed
- Orchestrator のソート: 同一 Severity 内で Confidence 降順が第2ソートキーに (従来は RuleName のみ)

## [0.3.0] - 2026-04-15

### Added
- `secret_exposure_scanner` rule — 8つのシークレットパターンを検出 (AWS/OpenAI/Anthropic/GitHub/Slack/JWT/Generic)
  - Critical: AWS Access Key (`AKIA...`), PEM秘密鍵, OpenAI/Anthropic APIキー
  - Warning: GitHub Token (`gh[pousr]_...`), Slack Bot Token (`xox[bpars]-...`)
  - Info: JWT (`eyJ...`), Generic パターン (password=XXX, token=XXX)
  - 除外ロジック: `${VAR}`, `{{placeholder}}`, `process.env.X`, `os.Getenv()` は誤検知なし
  - 対象: `Node.Config` の string / map / slice 値を再帰的にスキャン
- `testdata/secrets/exposed.json` — AWS/OpenAI/Anthropic キーをハードコードしたサンプル (Critical×3)
- `testdata/secrets/safe.json` — 環境変数参照のみのサンプル (0件)
- `domain/testutil/generate.go`: `GenerateSecretExposureGraph(seed)` — Critical 発火パターン生成関数追加
- `cmd/shingan-gen`: `--pattern secret-exposure` オプション追加
- `testdata/generated/secret-exposure-seed42.json` — シード42の生成済みサンプル
- `docs/secret-detection.md` — 検出パターン一覧、Severity判定、除外ロジック解説、v0.4 entropy scanner 予定

## [0.2.0] - 2026-04-15

### Added
- `cmd/shingan-gen` CLI — 7パターンのワークフロー生成 (random, clean, buggy, infinite-loop, unreachable, pii-leak, cycle)
  - `--pattern`, `--size`, `--seed`, `--output` フラグ対応
  - `shingan analyze --format json` と完全互換のJSON出力（nodes配列形式）
  - シード固定による再現性保証
- `domain/testutil/generate.go` — 6つのパターン生成関数を追加
  - `GenerateCleanGraph(n, seed)` — 全7ルールをパスする正常グラフ
  - `GenerateInfiniteLoopGraph(seed)` — loop_guard + cycle_detection 発火
  - `GenerateUnreachableGraph(n, seed)` — unreachable_node 発火
  - `GeneratePIILeakGraph(seed)` — pii_leak_scanner 発火
  - `GenerateCycleGraph(n, seed)` — cycle_detection (非Loopノード) 発火
  - `GenerateBuggyGraph(seed)` — 全7ルール同時発火
- `testdata/generated/` — 各パターンの生成済みサンプルJSON (7ファイル、合計約64KB)
- `docs/sample-generator.md` — shingan-gen 使い方ガイド、パターン解説、教育目的活用法
- `Makefile`: `gen-cli`、`sample-%` ターゲット追加
- `infrastructure/parser/adkgo.go` に `go/types` ベースのセカンドパス追加。`functiontool.New[TArgs, TResults]` のジェネリクス型引数を `types.Info.Instances` で取得し、TArgs の struct 名・フィールド名から Tool カテゴリを推定。`ParseFile(path string)` API 経由で利用可能。ロード失敗時は自動で AST-only にフォールバック
- `ADKGoParser` に `WithoutTypes()` オプション追加。型情報パスを無効化してAST-onlyパスを強制（テスト高速化、ネットワーク非接続環境向け）
- `NodeTypeLoop` (iota=5) — LoopAgent相当。`max_iterations` 必須。ADK-Go の `LoopAgent` はこの型に解析される。
- `NodeTypeCondition` (iota=6) — if/switch/条件分岐相当。`max_iterations` 不要。
- `testutil.Builder.AddLoopNode(id, maxIter)` / `AddConditionNode(id, expression)` ヘルパーメソッド
- `ErrorHandlerChecker` に2ホップ追跡 — Tool直後が Condition ノードで条件付きエッジを持つ場合、エラーハンドリングあり判定
- `ErrorHandlerChecker` に `Config["reliable"]=true` フラグサポート — 決定的アルゴリズム（sort等）を除外
- `loopguard.go` に `isLoopNode()` ヘルパー関数（NodeTypeLoop + deprecated NodeTypeControl を判定）

### Changed
- `PIILeakScanner` を O(sources · (V+E)) から O(V+E) に最適化。逆方向隣接リスト事前構築 + Sink起点逆BFS により、n=1000 で 275ms → 26ms（10倍以上の改善）。既存テスト (11ケース) 互換性を維持しつつ大規模ケース2件を追加。
- `NodeTypeControl` を deprecated に変更。JSON `"control"` 文字列は後方互換のため `NodeTypeLoop` として扱う（iota値 2 は維持）。
- `LoopGuardChecker` の対象を `NodeTypeLoop` / deprecated `NodeTypeControl` のみに変更。`NodeTypeCondition` は対象外。
- `CycleDetector` の Loop 判定を `isLoopNode()` に統一。`NodeTypeCondition` 単体のサイクルは Critical（グラフ定義誤り）のまま。
- `ADKGoParser`: `LoopAgent` → `NodeTypeLoop`（従来は NodeTypeControl）。Sequential/Parallel は NodeTypeControl のまま。
- `SamuraiParser`: `"loop"` → `NodeTypeLoop`、`"condition"` → `NodeTypeCondition`（従来は両方 NodeTypeControl）。
- `reachability.go` の `nodeTypeName()` に Loop/Condition ケース追加。
- `testdata/meta/shingan_pipeline.json`: `parse_error_branch`, `format_error_branch` を `condition` 型に変更。`sort_by_severity` に `reliable=true` を追加。

### Fixed
- `loop_guard` が条件分岐ノード (`parse_error_branch`, `format_error_branch`) を誤検知 (Critical×2)
- `error_handler_checker` が Condition ノードを介したエラーハンドリングを見逃す誤検知 (Info×2)
- `error_handler_checker` が決定的ツール (`sort_by_severity`) を誤検知 (Info×1)
- 上記5件すべてが `testdata/meta/shingan_pipeline.json` で0件になることを自己検証確認 (`docs/self-dogfood.md`)

## [0.1.0] - 2026-04-15

### Added
- 7 analysis rules:
  - `pii_leak_scanner` (v0.3 preview) — RAG→外部送信パスでHuman gateなし
- functiontool.New() 経由で登録したToolのAST検出対応（error_handler_checker強化）
- Playwright スクリーンショット自動化スクリプト (`scripts/screenshots/`、10枚)
- Marp 面接プレゼンスライド15枚 (HTML生成済、`slides/pitch.md`)
- GitHub Action (`action.yml`) — `uses: hatyibei/shingan@v0.1.0`
- Multi-stage Dockerfile (distroless, 4バイナリ)
- Performance benchmarks (`domain/rules/bench_test.go`, `application/bench_test.go`, `infrastructure/parser/bench_test.go`) + `docs/benchmarks.md`
- Self-dogfood verification (`docs/self-dogfood.md`) — 既知誤検知5件の文書化
- 6 base analysis rules:
  - `cycle_detection` — 非Controlノードのサイクル、LoopAgent管理下のサイクル
  - `loop_guard` — Control型ノードのMaxIterations未設定検出（独立ルール）
  - `unreachable_node` — エントリから到達不能なLLM/Tool
  - `error_handler_checker` — 外部I/Oノード後のエラーハンドリング欠落
  - `cost_estimation` — ループ内高額LLMモデル、単純タスクへの高額モデル適用
  - `redundant_llm_call` — 同一prompt_template×modelの重複呼出
- 3 input formats:
  - `json` — Shingan独自のWorkflowGraph JSON
  - `adk-go` — Google ADK-Go (`google.golang.org/adk v1.1.0`) のAST解析
  - `samurai` (Alpha) — SamuraiAI想定スキーマのParser skeleton
- 3 output formats:
  - `json` (API応答向け)
  - `markdown` (CLI・レポート向け)
  - `sarif` (GitHub Code Scanning統合)
- 4 entry points:
  - `cmd/shingan` — CLI (cobra)
  - `cmd/api` — goa v3 Design-first HTTP API + 自動生成OpenAPI
  - `cmd/runner` — Vertex AI Gemini でADK-Go Agentを安全実行
  - `cmd/shingan-web` — ADK Web UI + Shingan pre-execution middleware
- Onion Architecture + Factory Pattern 3箇所 (Analyzer/Parser/Reporter)
- goroutine並行ルール実行 (`sync.WaitGroup` + `chan []Finding`)
- CI: GitHub Actions (Go 1.25, lint/test/build, coverage artifact)
- Issue/PR templates, CONTRIBUTING.md
- ADR 5件 (shingan-adr.md) + docs/ (architecture, runtime-demo, sarif-output, samurai-adapter, cycle-detection-note, adk-webui-integration)

### Notable architectural decisions
- Go + goa + Onion Architecture + Factory Pattern を採用（Kivaスタック準拠）
- Design-first API契約で OpenAPI/実装のドリフトを構造的にゼロに
- 解析ルールはstatelessでWorkflowGraph読み取り専用 → goroutine並行実行が自然に成立
