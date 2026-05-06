> 🌐 Language: [English](./crewai.md) | **日本語**

# CrewAI サポート

> Phase 1 / 2 つ目の Python ターゲット — [CrewAI](https://github.com/crewAIInc/crewAI) の Crew/Agent/Task 定義を解析する Shingan パーサ。LangGraph で確立した PythonWorker インフラを再利用 (ADR-013)。

## 概要

Shingan は `crewai.Crew` インスタンスをモジュール直下で定義した `.py` ファイルを静的解析できる。LangGraph と同じく long-lived Python subprocess + JSON-RPC bridge で、CrewAI 自体の import は解析セッション中 1 回だけ。

```
┌──────────────────────┐  newline-JSON RPC  ┌──────────────────────────┐
│ shingan (Go process) │ ◄─────────────────►│ scripts/export_crewai_…  │
│   CrewAIParser       │                    │ (Python long-lived       │
│   PythonWorker       │                    │  worker)                 │
└──────────────────────┘                    └──────────────────────────┘
```

Go 側の `PythonWorker` は `--format=langgraph` で動いているのと**全く同じ実装**。ADR-013 でフレームワーク非依存化したため、shim スクリプトが個別に `scripts/` 配下に並ぶ形。

## インストール

CrewAI は Python 3.10+ と Pydantic v2 が必要:

```bash
python3 -m pip install -r scripts/requirements-shim.txt
# 最低限:
python3 -m pip install "crewai>=0.50.0"
```

Python や CrewAI が無い環境でも shingan のビルドと他 format の解析は動く。`--format=crewai` 指定時のみ availability check が走り、失敗時は明確なエラーで停止:

```text
create crewai parser: crewai parser: Python 3.x and `pip install crewai` (>=0.50.0) required for CrewAI format
```

## 使い方

単一ファイル:

```bash
shingan analyze --format crewai --input crew.py --output markdown
```

ディレクトリ (`.py` 再帰走査、ADR-012 の per-file independent graph):

```bash
shingan analyze --format crewai --input ./crews/ --output sarif --output-file findings.sarif
```

CI で baseline 付き:

```bash
shingan analyze \
  --format crewai \
  --input ./crews \
  --baseline .shingan/baseline.json \
  --since main
```

## NodeType マッピング (ADR-013)

| CrewAI 概念 | Shingan NodeType | Confidence | ConfidenceReason |
|---|---|---|---|
| `Agent(role=, goal=, backstory=, tools=[…])` | LLM | 1.0 | `exact_static_match` |
| `Task(description=, expected_output=, agent=A)` | Tool | 1.0 | `exact_static_match` |
| `Tool` (`@tool` / `BaseTool` 継承) | Tool (`Config["category"]` は heuristic) | 0.8 | `name_heuristic` |
| `Crew(process=Process.sequential)` | Tasks 順次連結 (Task[i] → Task[i+1]) | 1.0 | `exact_static_match` |
| `Crew(process=Process.hierarchical, manager_llm=)` | manager → 各 worker → manager (保守的展開) | 0.7 | `over_approximated_dynamic` |
| `Agent.tools[t]` | エッジ `Agent → Tool` (無条件; Edge.Condition は本来の制御フロー条件にだけ予約) | 1.0 | `exact_static_match` |
| `Task.agent = A` | エッジ `Task → Agent` (無条件; Task が Agent を呼び出す) | 1.0 | `exact_static_match` |
| `Agent(allow_delegation=True)` × 2 つ以上 | 該当 Agent 同士に双方向 delegate エッジ | 0.6 | `over_approximated_dynamic` |

### Tool カテゴリの推定

shim は tool の name + クラス名で部分一致:

| name / class に含まれる | `Config["category"]` |
|---|---|
| `eval`, `exec`, `code_runner`, `code_interpreter`, `python_repl`, `shell`, `bash`, `subprocess` | `code_execution` |
| `http`, `api`, `request`, `fetch`, `rest` | `api` |
| `search`, `browser`, `scrape`, `web` | `tool` |
| それ以外 | `tool` (デフォルト) |

`eval_missing` ルール (LLM → `code_execution` Tool path) や `unbounded_tool_arg` (Pydantic schema 検査) はこの category を消費する。

## エッジマッピング

CrewAI の 2 種類の `Process` モードはこう変換:

### `Process.sequential`

```
entry = Task[0]
Task[0] ──seq──► Task[1] ──seq──► Task[2]
   │                │                │
   │ uses_agent     │ uses_agent     │ uses_agent
   ▼                ▼                ▼
 Agent[0]         Agent[1]         Agent[2]
   │                │                │
   ▼ uses_tool      ▼ uses_tool      ▼ uses_tool
 Tool[…]          Tool[…]          Tool[…]
```

全 Task から全 Agent / Tool に推移的に到達できるため、reachability 系ルール (`unreachable_node`, `cycle_detection`) はグラフ全域で動く。

### `Process.hierarchical`

```
entry = manager (synthetic LLM、`manager_llm` または `manager_agent` を写像)
manager ──delegate──► Worker[k]   (Condition="delegate" — runtime LLM dispatch)
manager ─────────────► Task[i]    (manager が各 Task を dispatch)
Task[i] ─────────────► assigned Agent
```

manager → worker エッジは保守的展開 (実行時に LLM が分岐先を決めるため候補全部を列挙)。これらのエッジは Confidence 0.7 / Reason `over_approximated_dynamic`。`worker → manager` の "report" 戻りエッジは **生成しない** — 結果返却パスをグラフエッジとして表現すると `cycle_detection` Critical の偽陽性が出るため。

## Confidence と ConfidenceReason

CrewAI は hierarchical の manager dispatch 以外は静的。両モードを並列に提示:

| エッジ / ノード種別 | Confidence | ConfidenceReason |
|---|---|---|
| `Task[i] → Task[i+1]` (sequential) | 1.0 | `exact_static_match` |
| `Task → Agent` (`uses_agent`) | 1.0 | `exact_static_match` |
| `Agent → Tool` (`uses_tool`) | 1.0 | `exact_static_match` |
| name / class 推定の Tool category | 0.8 | `name_heuristic` |
| `manager → worker` (hierarchical) | 0.7 | `over_approximated_dynamic` |
| 双方向 delegate (delegating agent ≥2) | 0.6 | `over_approximated_dynamic` |

Hierarchical のノイズだけ抑えたい場合は `--min-confidence=0.7` で gate。

## サンプル

5 つの参照サンプルが `testdata/crewai/` にある:

| ファイル | パターン | 実測 findings (crewai 1.14.4) |
|---|---|---|
| `simple_crew.py` | 1 Agent + 1 Task、`Process.sequential` | Warning 1 件 (`error_handler_checker`: Task に error-handling 分岐なし) |
| `sequential_pipeline.py` | 3 Agent + 3 Task、`Process.sequential` | Warning 3 件 (各 Task に `error_handler_checker`) |
| `hierarchical.py` | 2 Agent + `manager_llm=LLM(model="gpt-4o-mini")`、`Process.hierarchical` | Warning 2 件 (各 Task の `error_handler_checker`)。v0.8 で `worker → manager` 戻りエッジを削除したため `cycle_detection` 偽陽性は消滅 |
| `multi_tool.py` | 1 Agent + 3 tools (web search / HTTP / `python_repl`) | Critical 1 件 (`eval_missing` Agent → `python_repl` の `code_execution` sink) + Warning 2 件 (Task と tool 持ち Agent の `error_handler_checker`) + Info 1 件 (`pii_leak_scanner` 30%、Task → `http_api_request` external API への path) |
| `circular_delegation.py` | 2 Agent 両方が `allow_delegation=True` | Critical 1 件 (`cycle_detection` 100% on alpha — 双方向 delegate cycle は本物) + Warning 3 件 (`circular_dep_agents` 85% alpha↔beta ペア + 各 Task の `error_handler_checker`) |

実行例:

```bash
shingan analyze --format crewai --input testdata/crewai/multi_tool.py --output markdown
```

> **注**: 上の findings は `crewai==1.14.4` で計測した実測値。CrewAI バージョン更新後に再走させて差分を issue へ。

## 出力例 (`multi_tool.py`)

```bash
$ shingan analyze --format crewai --input testdata/crewai/multi_tool.py --output markdown
# Shingan Analysis Report

## Summary

| Total | Critical | Warning | Info |
|-------|----------|---------|------|
| 4     | 1        | 2       | 1    |

## Critical

| Rule         | Node                       | Confidence | Message                                                                                                                                            |
|--------------|----------------------------|------------|----------------------------------------------------------------------------------------------------------------------------------------------------|
| eval_missing | crew::tool::python_repl    | 90%        | LLM node "crew::agent::multi_tool_assistant" reaches code-execution tool "crew::tool::python_repl" (no validation); LLM output flows into a code runner without sanitisation |

## Warning

| Rule                  | Node                                       | Confidence | Message                                                                                                          |
|-----------------------|--------------------------------------------|------------|------------------------------------------------------------------------------------------------------------------|
| error_handler_checker | crew::task::Answer_the_users_question-0    | 80%        | Tool node has no conditional outgoing edges: error handling is missing                                           |
| error_handler_checker | crew::agent::multi_tool_assistant          | 80%        | LLM node uses tool(s) but has no conditional outgoing edges: error handling for tool failures is missing         |

## Info

| Rule              | Node                          | Confidence | Message                                                                                                                                                                  |
|-------------------|-------------------------------|------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| pii_leak_scanner  | crew::tool::http_api_request  | 30%        | potential PII leak: path from RAG/PII node "crew::task::Answer_the_users_question-0" to external tool "crew::tool::http_api_request" (category="api") without Human gate |
```

## 設計参照

- ADR-013: CrewAI parser strategy — PythonWorker 再利用
- ADR-009: long-lived worker + degraded mode
- ADR-008: ConfidenceReason 二次元品質管理
- ADR-002: Onion + Factory parser 拡張性

実装ファイル:

- `scripts/export_crewai_server.py` (Python shim)
- `infrastructure/parser/python_worker.go` (subprocess wrapper、LangGraph と共有)
- `infrastructure/parser/crewai.go` (`WorkflowParser` 実装)
- `infrastructure/factory/parser.go` (Factory 登録 `case "crewai"`)
- `cmd/shingan/analyze.go` (`--format=crewai` フラグ + ディレクトリ走査)
- `domain/testutil/generate.go` (プロパティテスト用 `GenerateCrewAIGraph`)
- `cmd/shingan-gen/main.go` (サンプル生成用 `--pattern=crewai-simple`)

## トラブルシューティング

| 症状 | 原因 | 対処 |
|---|---|---|
| `Python … not found in PATH` | Python 未インストール | Python 3.10+ を入れる |
| `pip install crewai (>=0.50.0) required` | crewai 未インストール or v0.50 未満 | `pip install "crewai>=0.50.0"` |
| `parse_file …: ModuleNotFoundError: No module named 'crewai_tools'` | カスタム Tool subclass が兄弟モジュールを import している | 対象を実行できる環境で解析 (ローカル venv 推奨) |
| グラフが空 | `Crew` を関数内で構築している | `Crew(…)` をモジュール直下に移す (lazy crew は Phase 2 範疇) |
| 双方向 delegate エッジが過剰に見える | `allow_delegation=True` の Agent が複数 | 不要な Agent から削るか、過剰展開を受容 (Confidence 0.6) |

## バージョン互換性

- `crewai >= 0.50.0`: shim でテスト済 (新バージョンが出るたび CI で更新)
- `crewai < 0.50.0`: 非対応 (Pydantic v1 の attribute アクセスが違いすぎ shim が脆弱)
- `crewai >= 1.0` (将来): private 属性名が変わる場合、shim の `getattr` fallback が吸収するが、API 破壊変更には追加対応必要
