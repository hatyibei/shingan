# Shingan Phase 2 — 開発者体験（Playボタン横の解析ボタン）

> 本ドキュメントは Shingan 本番化ロードマップ Phase 2 の実装計画。Phase 1（Parserプラグイン化＋LangGraph対応）完了を前提とする。

## Context

Shingan は現在 PoC 段階で、CLI + GitHub Action + ADK Web UI デモまで動く。本番化の理想像は以下：

> ESLint / Ruff 相当の体験を LangGraph / ADK / SamuraiAI 等のエージェントワークフローに持ち込む。開発者が Play ボタン横の「解析ボタン」をワンクリック、或いは CLI で一発実行すると、無限ループ／コスト爆発／PII 漏洩の指摘と「この処理は GPT-4o-mini で十分」「このノードはそもそも LLM 不要」等のモデル選定提案が即座に返る。

Phase 2 の目的は、CI 頼みをやめて **書いた瞬間にフィードバック** を返せる状態に到達すること。フラッグシップは VS Code 拡張、横展開として LSP / MCP / ADK Web UI パネルを揃える。

---

## 2-A. LSP (Language Server Protocol) サーバ

**狙い**: 一度書けば VS Code / Cursor / JetBrains / Neovim / Zed すべてで同じリアルタイム解析体験を提供する。Linter としての最短経路。

### 実装

- 新規 `cmd/shingan-lsp/main.go`
- ライブラリ: `go.lsp.dev/protocol` + `go.lsp.dev/jsonrpc2`（ADK と同じ Go エコシステムで統一）
- 対応メッセージ:
  - `initialize` / `initialized` / `shutdown` / `exit`
  - `textDocument/didOpen` / `didChange`（debounce 500ms）/ `didSave` / `didClose`
  - `textDocument/publishDiagnostics`（severity, range, message, code, codeDescription）
  - `textDocument/codeAction`（2-F の自動修正）
  - `textDocument/hover`（finding の詳細・ルールへの link）
- 対応言語:
  - **Go** (`.go`、`adk-go` パーサ経由、パッケージ単位で解析)
  - **Python** (`.py`、LangGraph / Python ADK、AST 解析は行わず ファイル保存時に `python -m shingan_export <file>` を呼び出して graph JSON を取得 → 既存 JSON パーサに流す)
  - **JSON/YAML**（SamuraiAI / Dify ワークフロー定義ファイルをファイル種判定してそのまま解析）
- キャッシュ: ファイル内容の SHA256 → findings の in-memory LRU (maxSize=512)。同一内容の連続保存で無駄な再解析を避ける。

### ディレクトリ構造

```
cmd/shingan-lsp/
  main.go           — エントリポイント、stdio JSON-RPC loop
  server.go         — LSP サーバ本体（状態管理）
  diagnostics.go    — Finding → LSP Diagnostic 変換
  codeaction.go     — fix-hint 生成
  hover.go
  export_python.go  — python -m shingan_export 呼び出しラッパ
  lsp_test.go       — プロトコル準拠テスト（go-lsp-mock クライアント）
```

### range 計算の重要点

`domain.Finding` は現状 `NodeID` しか位置情報を持たない。Phase 2 では **Parser 側で各ノードに source position を付与する責務** を追加する。

- `domain/graph.go` の `Node` 構造体に以下を追加（optional、LSP 用）:
  ```go
  type SourcePos struct {
      File string
      Line int
      Col  int
  }
  ```
- Go パーサ (`infrastructure/parser/adkgo.go`) は AST ノードから `token.FileSet` 経由で取得可能（既に parse 時に FileSet は持っている）。
- JSON パーサ は `_meta.line`/`_meta.col` がペイロードにあれば拾う、なければ node.id の文字列位置で近似する。

---

## 2-B. VS Code 拡張

**狙い**: 「Play ボタン横に解析ボタン」体験のフラッグシップ。Marketplace で配布してノーセットアップで試せる状態にする。

### 実装

- 新規 `extensions/vscode-shingan/`（TypeScript, `vsce` でビルド）
- 依存: `vscode-languageclient` を使って前述の LSP サーバを spawn。
- 追加 UI:
  - **Status bar アイテム**: 現在ファイルの findings 数 `✓` / `⚠ 3` / `✗ 1` を常時表示、クリックで Problems パネル。
  - **Code Lens**: ADK/LangGraph の agent/graph 構築関数のすぐ上に `▶ Analyze` / `⚡ Quick Fix all` を表示。
  - **ツリービュー**（Shingan サイドバー）:
    - 現在ワークスペース全体の findings を severity 別にツリー表示。
    - 各ノードをクリックで該当箇所へジャンプ、右クリックで suppress。
  - **コマンド**:
    - `Shingan: Analyze Current File`
    - `Shingan: Analyze Workspace`
    - `Shingan: Show Rules Documentation`
    - `Shingan: Suppress Finding on This Line`
- 設定項目 (`contributes.configuration`):
  - `shingan.lspPath` — バイナリパス（default: `shingan-lsp`）
  - `shingan.enabledRules` — オン/オフ切替
  - `shingan.severityThreshold` — Problems に表示する最小 severity
  - `shingan.analyzeOnSave` — default `true`
  - `shingan.modelCostProfile` — Phase 1 のモデル選定ルール用、利用中のモデル単価表を指定
- バンドル戦略: LSP バイナリを各 OS 向けにクロスコンパイル (`GOOS=linux,darwin,windows × GOARCH=amd64,arm64`) して `bin/` に同梱。初回起動時に該当バイナリを `~/.vscode-shingan/` にコピー。

---

## 2-C. LangGraph Studio 統合（MCP Server 経由）

**狙い**: LangGraph Studio の Play ボタン横に解析ボタンを出すのが本命だが、Studio が closed な Electron アプリで直接 UI を差し込めない。**MCP サーバとして公開し、Studio の MCP 設定で接続してもらう** 戦略に倒す。

### 実装

- 新規 `cmd/shingan-mcp/main.go`
- MCP 仕様: 公式 `github.com/modelcontextprotocol/go-sdk` を使用。
- 公開ツール:
  - `shingan_analyze_graph(graph_json: string) → FindingList`
  - `shingan_analyze_file(path: string, framework: string) → FindingList`
  - `shingan_explain_rule(rule_name: string) → string`
  - `shingan_suggest_model(node_description: string, input_token_estimate: int) → ModelRecommendation`
- Studio 以外のメリット: Claude Desktop / Cursor / 他 MCP クライアントからも呼べる。エコシステム的に拡がりが出る。
- 並行運用: LSP と MCP は両立（同じ Analyzer コアを呼ぶだけの薄い層）。

---

## 2-D. ADK Web UI の解析パネル拡張

**狙い**: 既に動いている `cmd/shingan-web` を Phase 1 の成果（多フレームワーク対応）と接続。デモ体験を拡張する。

### 実装

- 現状: 403 レスポンスの JSON はブラウザ Console に `Forbidden` としか出ない。
- 変更:
  - `cmd/shingan-web/main.go` に `/api/shingan/findings/:agent` エンドポイントを追加し、直近の解析結果を返す。
  - ADK 純正フロントに **gzip で差し込むパッチスクリプト** を同梱（`scripts/patch-adk-ui.sh`）— 純正 UI は Angular 製で直接は手が出ないので、静的アセット配信時に sidebar 注入する JS を `<head>` に追加する。
  - 注入する JS の内容: `/api/run_sse` の fetch レスポンスを wrap し、403 時は `#shingan-panel` という Floating Panel に findings をリスト表示、各 finding の `Suggestion` を「このボタンでコピー」。
- Agent 編集機能は Phase 3（CRUD API）まで見送り、Phase 2 では読み取り専用パネルに徹する。

---

## 2-E. Diff モード & Progressive Adoption

**狙い**: 既存の巨大ワークフロー（findings 数千件）を Shingan に接続したとき、一気に赤くなって無視されるのを避ける。

### 実装

- `shingan analyze --since=<git-ref>` フラグを `cmd/shingan/main.go` に追加。
- 内部動作:
  1. `git diff --name-only <ref>..HEAD` で変更ファイル一覧を取得
  2. 変更ファイルのみ解析
  3. さらに前回の baseline 結果（`shingan-baseline.json`、`shingan analyze --save-baseline` で生成）と比較し、**新規 finding のみ** 返す
- GitHub Action (`action.yml`) に `baseline-file` 入力を追加し、PR 時に差分のみを SARIF で出す運用を可能にする。
- LSP にもベースライン機構を実装し、「今回のブランチで追加された findings だけハイライト」モード（`shingan.diffMode: "since-main"` 設定）を提供。

---

## 2-F. Code Action（自動修正）

**狙い**: `MaxIterations=3` 追加程度は手で打たせず、`⌘.` 一発で入る。ESLint `--fix` 体験。

### 実装

- `domain/rule.go` の Finding に既存の `Suggestion string` に加えて新フィールド `AutoFix *TextEdit` を追加:
  ```go
  type TextEdit struct {
      File     string
      StartPos SourcePos
      EndPos   SourcePos
      NewText  string
  }
  ```
- ルール側で AutoFix を持てるものから順次実装:
  - `loop_guard` — `MaxIterations` フィールド追加（まず `3` 仮値、ユーザに値選択させる QuickPick も用意）
  - `deprecated_model` — モデル名文字列の置換
  - `llm_unnecessary`（Phase 1 で新設）— LLM ノードを決定論的な `CustomFuncNode` テンプレートに置換するスニペット生成
  - `cost_estimation` / `model_selection` — モデル名を推奨モデルに置換
- LSP の `textDocument/codeAction` で `TextEdit` を返す。複数 findings ある場合は `Quick Fix All` で一括適用。

---

## 2-G. テレメトリ（opt-in）

**狙い**: 実運用でどのルールがどれくらい suppress / fix されるかを把握し、Phase 5 の Confidence 校正に繋げる。

### 実装

- VS Code 拡張の設定 `shingan.telemetry.enabled`（default **false**）。
- 有効時のみ、匿名化された「rule_name / severity / action (fix/suppress/ignore)」を `shingan-telemetry` サーバ（Phase 3 のインフラ）に POST。
- ユーザ初回オン時に明示的な同意ダイアログを表示。

---

## 実装順序（2週間想定）

| 期間 | 作業 |
|---|---|
| Week1 day1-2 | `domain/graph.go` に `SourcePos` 追加、既存 Parser で埋める |
| Week1 day3-5 | `cmd/shingan-lsp/` 骨格 + diagnostics |
| Week1 day6-7 | `extensions/vscode-shingan/` の最小版（status bar + diagnostics 表示のみ） |
| Week2 day1-2 | code action + autofix（`loop_guard` と `deprecated_model` 先行） |
| Week2 day3 | MCP サーバ |
| Week2 day4 | Diff モード + baseline ファイル |
| Week2 day5 | ADK Web UI パネル注入 |
| Week2 day6-7 | E2E テスト / Marketplace 提出準備 |

---

## 検証

- **LSP 単体**: `lsp_test.go` で `initialize` → `didOpen` → `publishDiagnostics` の順で期待 findings が返る（table-driven）。
- **VS Code 拡張**: `vscode-test` で headless 起動、サンプルファイル open → 問題パネルに findings 表示を E2E。
- **MCP**: Claude Desktop から `shingan_analyze_file` 呼び出し → JSON 応答を確認。
- **Diff mode**: 特定コミットと HEAD で findings 数が一致しないこと、baseline と比較で「new」だけ抽出されることを確認。
- **VSIX 配布**: `npx vsce package` でパッケージ化 → `code --install-extension shingan-x.y.z.vsix` で動作確認。

---

## Phase 2 で残るスコープ外（Phase 3 以降へ）

- 結果の永続化（LSP は in-memory LRU のみ）
- 組織／ワークスペース単位の設定共有
- ルール個別の有効／無効をチーム単位で強制するポリシー機能
- 自動修正の安全性スコアリング（「この AutoFix はいつでも安全」vs「レビュー推奨」の区別）

---

## 変更／新規ファイル一覧

### 新規

- `cmd/shingan-lsp/main.go` + `server.go` + `diagnostics.go` + `codeaction.go` + `hover.go` + `export_python.go` + `lsp_test.go`
- `cmd/shingan-mcp/main.go`
- `extensions/vscode-shingan/`（TypeScript プロジェクト一式）
- `scripts/patch-adk-ui.sh`
- `scripts/export_langgraph.py`（Phase 1 から継続利用）

### 変更

- `domain/graph.go` — `SourcePos` 追加、`Node` 構造体に optional フィールド付与
- `domain/rule.go` — `Finding` に `AutoFix *TextEdit` 追加
- `infrastructure/parser/adkgo.go` — AST から SourcePos 埋め込み
- `infrastructure/parser/*.go` — 各パーサで SourcePos 対応
- `cmd/shingan/main.go` — `--since` / `--save-baseline` フラグ
- `cmd/shingan-web/main.go` — `/api/shingan/findings/:agent` エンドポイント
- `action.yml` — `baseline-file` 入力

### 再利用（Phase 2 で変更しない）

- `domain/graph.go` の WorkflowGraph 抽象（既に十分）
- `application/orchestrator.go` の Analyzer 呼び出しロジック
- `infrastructure/factory/analyzer.go` のルール登録（Phase 1 の拡張を継承）
