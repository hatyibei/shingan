# testdata/circular_agents/

`circular_dep_agents` ルールの動作確認用サンプルデータ。

## ファイル構成

| ファイル | 説明 | 期待 circular_dep_agents Findings |
|---|---|---|
| `cycle.json` | `planner_agent` ↔ `worker_agent` の 2-agent delegation cycle (両方 `agent_role` 設定済み) | 1件 (Warning, Confidence 0.85, ConfidenceReason exact_static_match) |
| `acyclic.json` | orchestrator → planner → worker → output の linear flow (循環なし、orchestrator パターン) | 0件 |

## 検証コマンド

```bash
# cycle: circular_dep_agents Warning + cycle_detection Critical の同居を確認
shingan analyze --format json --input testdata/circular_agents/cycle.json

# acyclic: circular_dep_agents は 0件 (cycle_detection も 0件)
shingan analyze --format json --input testdata/circular_agents/acyclic.json
```

`cycle.json` は cycle_detection (Critical) と circular_dep_agents (Warning) **両方** を発火させる設計です。これは ADR-007 / 本ルールの doc コメントで明記したとおり、cycle_detection は構造的 back-edge を Critical で報告し、circular_dep_agents は agent-delegation 限定の Warning として補足を出すという**意図的な overlap** です。

## 設計メモ

- **cycle.json** は 2 つの agent (LLM with `agent_role`) が直接互いを呼び合う最小構成。
  - 2-agent cycle なので Confidence は 0.85 / Severity Warning / ConfidenceReason `exact_static_match`。
  - 同時に cycle_detection が走ってくると Critical (`exact_static_match` 1.0) として graph 全体の back-edge も報告される。**Severity の差** で 2 つのルールの主眼を見分けられる。
- **acyclic.json** は orchestrator パターンの良い例:
  - orchestrator が sub-agents を**linear に呼ぶ** (順序: planner → worker → output) ので循環なし
  - back-edge を許す代わりに orchestrator 自身が router 兼 budget keeper として君臨
  - 実装上は `agent_role: "orchestrator"` + `sub_agents` 配列で declares
- 静的解析の限界: `agent_role` / `sub_agents` の Config キーが「使われていない名前で agent を declares」しているケースは本ルール検出できない (false negative)。フレームワーク固有 parser (LangGraph / ADK-Go) で補強する。
