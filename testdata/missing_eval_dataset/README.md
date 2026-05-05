# testdata/eval_missing

`missing_eval_dataset` ルールの動作確認用サンプルデータ。

| ファイル | 期待 `missing_eval_dataset` Findings |
|---|---|
| `prod.json` | 1件 (Warning, Confidence 0.7, ConfidenceReason `heuristic_pattern`)。`Config["deployment"]=true` + `Config["env"]="prod"` の deploy signal あり、eval signal は無し |
| `dev.json` | 0件 — `Config["env"]="dev"` は deploy signal とみなされないので silent (本ルールは pre-prod を policing しない) |

## 検証コマンド

```bash
shingan analyze --format json --input testdata/eval_missing/prod.json
shingan analyze --format markdown --input testdata/eval_missing/dev.json
```

## 設計メモ

- **Deploy signal** (true → deploy):
  - `Config["deployment"] == true`
  - `Config["deploy"] == true`
  - `Config["env"] in {prod, production, staging, stg}` (case-insensitive)
  - `Config["environment"]` 同上
- **Eval signal** (true → eval present):
  - `Config["eval_dataset"]` / `test_set` / `benchmark` / `eval` /
    `evals` / `test_dataset` / `regression_set` のいずれかが非空
  - 値は string (path / id) でも map (`{"name", "version"}`) でも OK
- 1 graph で **最大 1 finding** (multiple deploy flags でも単一 finding)。
  NodeID は最初に検出された deploy-flagged ノードを指す。
- 修正方針: graph のいずれかのノードに `Config["eval_dataset"]` を追加して
  versioned 回帰テスト set を指す。CI で eval を回す PR ワークフローを
  立ち上げる足場として使う。
