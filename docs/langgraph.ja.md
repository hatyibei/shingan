> 🌐 Language: [English](./langgraph.md) | **日本語**

# LangGraph 対応

> Phase 1 / 主戦場 (ADR-011) ― Python AI agent 向け、LangGraph `StateGraph` を解析する Shingan parser。

## 概要

Shingan は LangGraph で書かれたエージェント定義を **実行前に** 静的解析できる。`langgraph.graph.StateGraph` API で構築された node / edge / conditional_edges / entry_point を抽出し、Shingan の汎用 `WorkflowGraph` に変換する。

実装方式: **長寿命 Python subprocess** + JSON-RPC。Go プロセスから毎回 fork するのではなく、1 セッションにつき 1 worker を保持し、stdin/stdout で newline-delimited JSON を交換する (ADR-009 の設計と整合)。

```
┌──────────────────────┐  newline-JSON RPC  ┌────────────────────────┐
│ shingan (Go process) │ ◄─────────────────►│ scripts/export_…py     │
│   LangGraphParser    │                    │ (Python long-lived     │
│   PythonWorker       │                    │  worker)               │
└──────────────────────┘                    └────────────────────────┘
```

## インストール

LangGraph parser を有効化するには Python 3.10+ と `langgraph` パッケージが必要:

```bash
python3 -m pip install -r scripts/requirements-shim.txt
# または最低限:
python3 -m pip install "langgraph>=0.2.0"
```

Python / langgraph が無い環境でも shingan のビルド・他フォーマット (json/adk-go/samurai) の解析は動く。`--format=langgraph` を指定したときだけ可用性チェックが走り、失敗時はわかりやすいエラーメッセージで止まる:

```text
create langgraph parser: langgraph parser: Python 3.x and `pip install langgraph` required for LangGraph format
```

## 使い方

単一ファイル:

```bash
shingan analyze --format langgraph --input agent.py --output markdown
```

ディレクトリ (`.py` 全部を再帰的にスキャンしてマージ):

```bash
shingan analyze --format langgraph --input ./agents/ --output sarif --output-file findings.sarif
```

CI 用の典型例 (進捗適用 / progressive adoption):

```bash
shingan analyze \
  --format langgraph \
  --input ./agents \
  --baseline .shingan/baseline.json \
  --since main
```

## 対応する LangGraph 機能

| 機能 | 対応 | 注意 |
|---|---|---|
| `StateGraph(State)` インスタンス検出 | OK | モジュール全体で1個目を採用 |
| `add_node(name, fn)` | OK | 関数の `inspect.getsourcefile/getsourcelines` で SourcePos を埋める |
| `add_edge(from, to)` | OK | 固定 edge |
| `add_conditional_edges(from, fn, mapping)` | OK (over-approximation) | mapping の各 key を `Edge.Condition` に詰めて全候補を edge として出力 |
| `START` / `END` sentinel | 仮想化 (ノード化しない) | LangGraph と同様に擬似 sentinel として扱い、`add_edge(START, x)` の `x` を `entry_node_id` に昇格、`add_edge(y, END)` は drop。Shingan の `loop_guard`/`reachability` を誤発火させないための重要な調整 (`NodeTypeControl` ⇒ `NodeTypeLoop` backward-compat エイリアス回避) |
| `set_entry_point(...)` / `entry_point` 属性 | OK | `add_edge(START, ...)` で取れない場合の fallback として graph オブジェクトの `entry_point` 属性も読む |
| `MessageGraph` / `Graph` 派生 | 部分対応 | クラス名一致で検出 (private 属性 `_nodes` 等にも fallback) |
| `builder.compile()` 経由のグラフ | OK | コンパイル後オブジェクトの `.builder` / `.graph` 属性経由で StateGraph に到達 |

### 非対応 / 制限事項

- **動的な `add_node` (実行時生成)**: モジュール import 時点で StateGraph が組み上がっていないものは検出できない。LangGraph の典型ユースケースではモジュールトップレベルで構築するため通常は問題にならない。
- **Subgraph (StateGraph as node)**: 子グラフは Phase 2 (ADR-013 で defer) で対応予定。現状は親 StateGraph のみ展開する。
- **ReAct の動的 tool 選択**: `should_continue()` が動的に target を返す場合、mapping に列挙された候補のみが edge として現れる。実際に呼び出される tool が mapping 外の場合は parser からは見えない (over-approximation の限界、ADR-013)。
- **多モジュール構成**: `parse_file` は対象 `.py` の sys.path に `os.path.dirname(path)` を一時 prepend する。それ以外の location からの import は実行時の `sys.path` に依存する。

## Confidence と ConfidenceReason

Phase 1 では parser が出力する `WorkflowGraph` の `metadata.conditional_edge_reason` に `over_approximated_dynamic` を埋める。各 Finding への適用は Track R (Visitor pattern refactor) 完了後 (ADR-006/008)。

予測される組み合わせ:

| ノード/edge 種別 | Confidence | ConfidenceReason (Track R 後) |
|---|---|---|
| `add_edge(a, b)` | 1.0 | `exact_static_match` |
| `add_conditional_edges` の各 mapping value | 0.8 | `over_approximated_dynamic` |
| `START` → `entry_point` ブリッジ | 1.0 | `exact_static_match` |
| handler 名による NodeType 推定 (`tool` / `llm`) | 0.6 | `name_heuristic` |

## サンプル

`testdata/langgraph/` 配下に 5 種類のリファレンスサンプルを置いている:

| ファイル | パターン | 期待 finding |
|---|---|---|
| `simple_chain.py` | 直列 3 node (START → classify → respond → END) | なし (健全) |
| `branching.py` | `add_conditional_edges` で 3-way 分岐 | なし (健全、over-approximation で各分岐 edge 検出) |
| `react_loop.py` | model⇄tools loop, 終了条件あり | `cycle_detection` (Critical) / `loop_guard` (Warning) |
| `rag.py` | RAG 検索 → LLM → 外部 webhook | `pii_leak_scanner` (Warning, Track R 後) |
| `multi_agent.py` | supervisor + 3 worker, 全 worker が supervisor に loopback | `cycle_detection` 周辺 |

各サンプルに対応する期待 `WorkflowGraph` は `testdata/langgraph/expected/*.json` に置いてある (E2E ゴールデンテスト用)。

## 解析結果の例 (`react_loop.py`)

```bash
$ shingan analyze --format langgraph --input testdata/langgraph/react_loop.py --output markdown
# Findings (2)

## Critical: cycle_detection
- Node: tools → model
- Confidence: 1.0 (DFS back-edge)
- Message: cycle detected: tools → model → tools

## Warning: loop_guard
- Node: model
- Confidence: 0.8 (heuristic)
- Message: cyclic component has no max_iterations guard
```

(exit code: `2`)

## 設計参照

- ADR-011: 主戦場 LangGraph シフト
- ADR-009: LSP 差分実行 + degraded mode (長寿命 worker)
- ADR-008: ConfidenceReason 二次元化
- ADR-002: Onion + Factory による parser 拡張性

実装ファイル:

- `scripts/export_langgraph_server.py` (Python shim)
- `infrastructure/parser/python_worker.go` (subprocess wrapper)
- `infrastructure/parser/langgraph.go` (`WorkflowParser` 実装)
- `infrastructure/factory/parser.go` (Factory 登録 `case "langgraph"`)
- `cmd/shingan/analyze.go` (`--format=langgraph` フラグ + ディレクトリ走査)

## トラブルシュート

| 症状 | 原因 | 対処 |
|---|---|---|
| `Python … not found in PATH` | Python 未インストール | Python 3.10+ を入れる |
| `pip install langgraph required` | langgraph 未インストール | `pip install langgraph` |
| `parse_file …: ModuleNotFoundError: No module named 'foo'` | 解析対象が外部依存に依拠 | 対象を実行できる環境で解析 (ローカル venv 推奨) |
| `call "parse_file" timed out after 30s` | 大きなモジュール / 重い import | `WithCallTimeout` でタイムアウト延長 (LSP/CLIで設定値調整) |
| 解析が空グラフ | StateGraph がモジュールトップレベルにない | 関数内 build パターンは Phase 2 対応 |

## バージョン互換性

- `langgraph >= 0.2.0`: テスト済み (CI で随時更新)
- `langgraph < 0.2.0`: API 不一致のため非サポート
- `langgraph >= 1.0` (将来): private 属性名が変わった場合 shim の `_nodes`/`_edges`/`_branches` fallback で吸収するが、API 変更時は追加対応必要

LangGraph API はまだ若いので shim 側で **API tolerant** に書いてある (`getattr` 連打、`isinstance` 不使用)。バージョン差で API が破綻した場合は shim の `_extract_*` 関数を更新するだけで対応できる。
