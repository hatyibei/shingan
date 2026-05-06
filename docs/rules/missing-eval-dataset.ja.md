> 🌐 Language: [English](./missing-eval-dataset.md) | **日本語**

# missing_eval_dataset — 設計・実装ドキュメント

> **対象バージョン**: Phase 2 #6 (v0.6 系列)
> **実装ファイル**: `domain/rules/missing_eval_dataset.go`
> **テスト**: `domain/rules/missing_eval_dataset_test.go` (19 ケース)
> **層 (ADR-007)**: Local rule — `OnGraph` aggregation で 1 graph 完結判定

---

## 1. 背景・動機

production / staging に投入される AI agent ワークフローで **eval dataset / regression test set を運用していない** ケースは、モデルアップデートで挙動が静かに変わるリスクを抱える:

- LLM provider がモデルバージョンを silent rollover する (例: `gpt-4o` の minor update)
- prompt の微調整で classification 結果が偏る
- tool 出力フォーマットが変わって downstream parser が失敗する

これらは **eval suite を回せば PR で気付ける** が、ワークフロー定義に eval dataset 参照が無い場合、production 投入後の support ticket / 顧客クレームでしか発覚しない。

本ルールは **deploy signal がある場合に、eval reference が graph のどこにも無ければ Warning** を出すことで、CI で eval を回す仕組み作りを促す。

---

## 2. 検出対象

### 2.1 Deploy signal の判定

`g.Nodes` を走査し、以下のいずれかを持つノードがあれば deploy signal あり:

| Config キー | 値 |
|---|---|
| `deployment` | `bool true` |
| `deploy` | `bool true` |
| `env` | `string` で `prod`/`production`/`staging`/`stg` (case-insensitive、trim) |
| `environment` | 同上 |

`env` 値の集合は `domain/rules/missing_eval_dataset.go` の `deployEnvValues` map にハードコード。新しい alias (`pre-prod` 等) を追加する場合はこの map を更新。

### 2.2 Eval signal の判定

同じく `g.Nodes` を走査し、以下のいずれかを持つノードがあれば eval signal あり:

| Config キー | 受理する値型 |
|---|---|
| `eval_dataset` | string (非空) / map / array |
| `test_set` | 同上 |
| `benchmark` | 同上 |
| `eval` | 同上 |
| `evals` | 同上 |
| `test_dataset` | 同上 |
| `regression_set` | 同上 |

string の場合は `strings.TrimSpace` 後に空文字でないことを要求 (whitespace-only は missing 扱い、ADR-008 の「typo を silently pass しない」原則)。

### 2.3 判定ロジック

```
deploy signal:  ANY ノードが上の条件を満たす? → has_deploy
eval signal:    ANY ノードが上の条件を満たす? → has_eval

if has_deploy && !has_eval:
    Warning (Confidence 0.7, heuristic_pattern)
else:
    silent
```

- pre-prod (`env=dev` / `env=staging` 以外) → silent
- deploy あり + eval あり → silent (理想ケース)
- 1 graph あたり **最大 1 finding**。multi-deploy-flag でも 1 件のみ
- NodeID は **deploy signal を持つ最初のノード** を指す (map 走査の順序は決定的でないが、`len(findings) == 1` の保証は維持される)

---

## 3. 実装の設計判断

### 3.1 Local tier として登録した理由

`OnGraph` で 1 度だけ判定し、per-node visit は不要。Path / Global tier を使うほどの計算量ではない。`redundant_llm_call` (`redundant.go`) と同じ「OnNode で集計 → OnGraph で emit」パターンの簡易版。

### 3.2 「ANY ノードが deploy / eval を持つ」設計

Workflow author によって metadata 配置が分かれる:

- **pattern A**: orchestrator ノードに `deployment=true` を集約
- **pattern B**: 末端ノードや個別 step ノードに `env=prod` を散らす
- **pattern C**: `evaluator` という別ノードに `eval_dataset` を持たせる

ANY ノードでの存在チェックなら全パターンで動作する。NodeID は detection を補助する目的で deploy 側のノードを指すだけ。

### 3.3 deploy 値が boolean false / 不在で silent

`deployment=false` を明示的に書いているケース (環境 toggle) でも silent にする。理由は false positive 抑制 — 開発期 mock として `deployment=false` を残すフローは多い。

### 3.4 Confidence 0.7 の根拠

deploy 判定 (env 文字列マッチ) も eval 判定 (キー名マッチ) も **naming heuristic** で、schema-bound contract ではない:

- `env=prod-eu` のような非 canonical 値は false negative になる (`deployEnvValues` 拡張が必要)
- `eval_dataset` を `test_data` というキー名で書いている graph は false negative
- 「production deploy = eval が必須」という前提自体が業務文化依存

ADR-008 の Confidence guideline で 0.5-0.7 の幅に該当する強めヒューリスティック。

---

## 4. False Positive / False Negative

### False Positive

- **eval を別レポジトリで管理しているケース**: ワークフロー JSON に eval reference を書かず、CI ステップで別途実行している運用は false positive。回避策: workflow JSON 側に名目だけでも `Config["eval_dataset"] = "ci/eval-suite"` のような string を入れる、もしくは `--rules ...` から本ルールを除外。
- **canary deploy**: A/B 分割で limited cohort のみ運用しているケースでは、eval を回す前に deploy しているとも言える。Severity Warning 止まりなので exit code を上げない (CI は green)。

### False Negative

- **`env=production-eu`**: `deployEnvValues` に exact match しないので素通り。alias を増やすか、別ルール (`production_env_pattern_check`) に分離するかは Phase 3 の検討事項。
- **動的 deploy flag**: コード側で `if env == "prod": config["deployment"] = True` のように runtime 判定しているケース。Config に true 値が現れず検出できない。フレームワーク固有 parser (LangGraph / ADK-Go) で AST レベルの追跡が必要。
- **非 canonical eval キー**: `Config["test_data"]` / `Config["benchmark_path"]` 等の独自命名は対象外。`evalDatasetKeys` を拡張する PR を歓迎。

---

## 5. 関連 ADR

- **ADR-006**: ESLint visitor pattern — Local rule の OnGraph dispatcher を使用。
- **ADR-007**: Local / Path / Global の3層分離 — 本ルールは Local tier (graph-wide aggregation の OnGraph パターン)。
- **ADR-008**: ConfidenceReason — `ReasonHeuristicPattern` を採用。
- **ADR-010**: Plugin SDK internal-only (v1.0 まで) — 本ルールも `init()` で `registerBuiltin()` 経由の登録。
