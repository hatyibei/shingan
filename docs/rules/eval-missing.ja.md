> 🌐 Language: [English](./eval-missing.md) | **日本語**

# eval_missing — 設計・実装ドキュメント

> **対象バージョン**: Phase 2 #2 (v0.6 系列)
> **実装ファイル**: `domain/rules/eval_missing.go`
> **テスト**: `domain/rules/eval_missing_test.go` (15 ケース、サブテスト込み)
> **層 (ADR-007)**: Path rule — Sources / Sinks / Propagate を実装

---

## 1. 背景・動機

LLM ベースのエージェントが「自然言語で書かれた手順を **コードとして実行**」させるパターンは、生産性が高い反面、最も危険な脆弱性カテゴリの一つに直結する:

- LLM 出力は **動的に生成** されるため、攻撃者が prompt injection / RAG poisoning / Tool 戻り値経由でコード文字列を捏造できれば、`eval()` / `exec()` / `Function()` / `code_interpreter` 経由で **任意コード実行 (RCE)** につながる。
- 結果として、**認証情報の漏洩**、**システム内部ファイルへのアクセス**、**外部送信**、**他テナントへの侵害** が発生する。

このルールは、ワークフローグラフの **静的構造** から「LLM ノード → コード実行系 Tool ノードへの到達可能性」を検出し、設計時に **runtime sandbox に頼る前** に問題を顕在化させる。

> **静的解析の限界**: runtime での schema validation / output classifier / sandboxed evaluator は graph 上に現れないため検出できない。本ルールは「構造的な攻撃面」を可視化するに過ぎず、「実際に攻撃可能かどうか」までは断定しない。Confidence 0.9 を割り当てているのはそのため。

---

## 2. 検出対象

### 2.1 Source (LLM ノード)

`NodeType.LLM` の任意のノードを source とみなす。Severity 自体は Source 側の特性ではなく path 上の gate に依存するので、Source の絞り込みは行わない (= 過剰検出を許容する設計判断)。

### 2.2 Sink (コード実行系 Tool ノード)

`NodeType.Tool` のうち、以下のいずれかに該当するものを sink とみなす:

| 判定経路 | 判定条件 |
|---|---|
| Config["category"] | `code_execution` または `code_eval` |
| Config["tool"] | `eval` / `exec` / `code_interpreter` / `python_runner` / `shell` のいずれか (大小文字無視) |
| Name / ID regex | `(?i)(eval\|exec\|code[_]?runner\|python[_]?runner\|shell\|bash)` 部分一致 |

`code_runner` / `python_runner` のような snake_case と `CodeRunner` / `PythonRunner` のような PascalCase の両方にマッチする。

### 2.3 Path 上の gate

Path 上に挟まれているノードの種類が Severity を決める:

| Path 上の最強 gate | Severity | Confidence | ConfidenceReason |
|---|---|---|---|
| 何も挟まない (LLM → Tool 直結 / 中間に LLM/Tool だけ) | **Critical** | 0.9 | `heuristic_pattern` |
| `NodeType.Condition` ノードあり | **Warning** | 0.6 | `heuristic_pattern` |
| `NodeType.Human` ノードあり | **(skip)** | — | — |

「Condition だけある」は **明示的な validation の存在を運用者が認識している** という弱いシグナルだが、自動化されたコードチェックは攻撃文字列を完全には弾けない (例: `eval("__import__('os').system(...)")` を簡易 syntax check で見逃す)。**完全な無効化ではなく Severity を 1 段階下げる** のが本ルールの設計判断。

「Human gate がある」は **承認者が明示的にコードを目視できる** ため、ルール上の path を成立させない (PII leak rule の Human-gate 規則と同形)。

---

## 3. 検出アルゴリズム

PII leak rule (reverse-BFS) と異なり、本ルールは **forward BFS** を採用する。理由は次のとおり:

1. Severity が **path 上の gate 種別に依存** するため、frontier に `viaCondition` フラグを持たせて forward に伝播するほうが自然 (reverse-BFS だと sink から逆向きに「Condition を通過したか」を再構成する必要がある)。
2. 「LLM が eval にたどり着くか?」という人間の読み方とも一致するので、コードリーディングが楽。

```
Step 1: Sources(g) で LLM ノードを抽出 (O(V))
Step 2: Sinks(g) で code_execution Tool ノードを抽出 (O(V))
Step 3: 各 source から forward BFS。frontier = {node, viaCondition bool}。
        - 次ノードが Human → 展開停止 (path drop)
        - 次ノードが Condition → viaCondition = true で展開
        - 次ノードが Sink → Finding 発火 (Severity = viaCondition ? Warning : Critical)
Step 4: visited は (node, viaCondition) のペアで重複排除。
        既に viaCondition=false で訪問した node に viaCondition=true で再到達しても
        downgrade させない (より強い path が dominate する)。
```

**計算量**: O(sources × (V+E))。典型ワークフローでは sources << V のため実質 O(V+E)。

---

## 4. 実装の設計判断

### 4.1 「Human gate を境界にする」理由

PII leak rule と同じ判断: 人間が承認画面でコードを目視確認できるなら、自動化された eval を上回る防衛線になる。Condition だけでは validation のロジック自体が静的に検証できないので、「不充分だが明示」という意味で Severity を 1 段下げて Warning に留める。

### 4.2 Severity を sink 分類ではなく path 上の gate で決める理由

prompt_injection_sink は **sink classification 時** に Severity が決まる (system_prompt + substitution → Critical)。一方 eval_missing では sink 自体の危険度はほぼ一定 (RCE) だが、間にどんな gate を挟むかが評価軸として遥かに重要。よって path-state を frontier に持たせる設計を採った。

### 4.3 Source を「全 LLM」と広く取る理由

`Config["allow_code_exec"]` のような明示フラグは存在しないし、LLM 自体が source として危険でないと言い切るのは難しい (RAG 経由で外部入力が混入することは普通)。**Sink 側の絞り込みで FP を抑える** 戦略にしている。

### 4.4 visited を `(node, viaCondition)` ペアで管理する理由

通常の BFS は `node` で visited 判定するが、本ルールでは frontier に `viaCondition` を含めるため、**同じ node に異なる state で再到達したら別 path として扱う** 必要がある。ただし dominance 規則 (Critical-eligible が既にあれば downgrade route は無視) を入れて、結果が安定するようにしている。

### 4.5 forward adjacency を Propagate で構築する件

PathContext の `Reverse` は path_walker が事前構築した reverse adjacency。本ルールは forward 流のため、自前で `forward` map を 1 度構築する。この cost は O(E)、PII leak rule の reverse 構築コストと同じ。将来 path_walker に `Forward` を生やす拡張余地はある (YAGNI で defer)。

---

## 5. 推奨される対策パターン

LLM 出力をそのまま eval / exec / Function に渡すアーキテクチャは可能な限り避け、以下のいずれかに置き換える:

```python
# OK: structured tool-call schema (function calling) — 引数が型チェック済みの構造体
client.chat.completions.create(
    model="gpt-4o",
    messages=[...],
    tools=[
        {
            "type": "function",
            "function": {
                "name": "run_query",
                "parameters": {
                    "type": "object",
                    "properties": {"sql": {"type": "string"}},
                    "required": ["sql"],
                },
            },
        },
    ],
)
# 戻り値の sql は parser を通してから実行 (allow-list / parameterized query)

# NG: LLM のテキスト応答を eval に直接食わす
result = eval(llm_response)  # ← 本ルールが Critical で発火
```

具体的な対策:

1. **Structured tool-call schema** — function calling / Tool Use API で引数を構造化、自由文字列を禁止
2. **Sandboxed evaluator** — `seccomp` / Docker / Firecracker / [Vercel Sandbox](https://vercel.com/docs/runtime/sandbox) のような分離環境で実行
3. **Allow-list dispatch** — `commands = {"sum": handler_sum, "diff": handler_diff}` のような明示テーブルに引数 mapping、`eval` は使わない
4. **Static AST validation** — `ast.parse` + AST visitor で許可された node 種別だけ通す
5. **Human-in-the-loop approval** — 高リスク操作 (DB 削除、外部送信) は人間承認を path 上に挿入

---

## 6. 既知の False Positive / False Negative

### False Positive

- **runtime sandbox 経由の eval**: LangGraph などで「Tool 内部で seccomp 経由 fork-exec」していても、graph 上は `code_execution` カテゴリの Tool が見えるだけなので Critical で発火する。`--min-confidence 0.95` で抑制可能、または Tool ノード側で `category` を `sandboxed_code` のような独自カテゴリに変更すれば fall through する。
- **Condition による downgrade ガードの過大評価**: Condition の中身が「常に true を返すスタブ」のような場合でも Warning は出る。逆に「実際にはほぼ完璧な validator」でも Severity は同じ Warning。中身の妥当性は静的解析の対象外。

### False Negative

- **Tool ノードを介さない eval**: LLM 出力をそのまま Python の `exec(llm_out)` で評価する LangChain 風書き方を、Tool ノード以外の場所 (例: コードベース直書き) でやられると graph 上に sink ノードが現れず検出不能。`dynamic_node_construction` rule (sibling rule) が `Config["body"]` 等の文字列値レベルで補完する。
- **Loop 経由の遅延注入**: Loop 内で次イテレーションの input に LLM 出力を挿入し、別系統の Tool で eval させるパターン。forward BFS では Loop の back edge 越しの伝播は見ているが、subgraph 上で source が visible でないと検出できない。
- **Tool category 名称が独自**: `Config["category"]` が `runtime_eval` / `dynamic_exec` のような未登録キーだと sink 判定を漏らす。命名規約を共有 (PR で追加) するか、name regex に拾われやすい命名にする運用が必要。

---

## 7. 関連ルール / ADR

- **ADR-006**: ESLint visitor pattern — Local rule との対比。本ルールは Path tier。
- **ADR-007**: Local / Path / Global の3層分離 — 本ルールが Path tier に属する根拠。
- **ADR-008**: ConfidenceReason — `ReasonHeuristicPattern` を採用。
- `prompt_injection_sink` (`docs/rules/prompt-injection-sink.md`) — 同じ Path rule の sibling、user_input → LLM の structural sink。
- `pii_leak_scanner` (`docs/pii-detection.md`) — reverse-BFS の鋳型、Human-gate 規則の元。
- `dynamic_node_construction` (`docs/rules/dynamic-node-construction.md`) — Local rule、Config 値レベルの `eval(`/`exec(`/`Function(` 直接検出。本ルールが構造的攻撃面、 dynamic_node_construction が文字列レベル攻撃面を担当する補完関係。
