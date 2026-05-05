# testdata/eval_missing/

`eval_missing` ルールの動作確認用サンプルデータ。

## ファイル構成

| ファイル | 説明 | 期待 eval_missing Findings |
|---|---|---|
| `leak.json` | LLM `plan_llm` → `python_runner` (Tool, `category="code_execution"`) に直結。バリデーションも Human 承認も挟まない | 1件 (Critical, Confidence 0.9, ConfidenceReason heuristic_pattern) |
| `safe.json` | 同じ LLM → `python_runner` の構造だが、間に Human (`human_approver`) を挿入。Human 承認が path 上に存在するため発火しない | 0件 |

## 検証コマンド

```bash
# leak: eval_missing Critical が 1件
shingan analyze --format json --input testdata/eval_missing/leak.json

# safe: eval_missing は 0件 (Human gate が path を断ち切る)
shingan analyze --format json --input testdata/eval_missing/safe.json
```

`leak.json` には他に `error_handler_checker` Warning が含まれる可能性がある (Tool ノードの条件分岐欠落) が、本サンプルの注目点ではない。

## 設計メモ

- **leak.json** の Critical 発火点 = LLM 出力が code_execution Tool に直接到達する経路。Severity は path 上の gate 種別で決まる: 何も挟まない → Critical / Condition だけ → Warning / Human が path 上にある → 発火しない。
- **safe.json** が安全な理由 = Human ノード `human_approver` が path 上に存在するため、forward BFS は Human 以降の経路を展開しない (PII leak scanner と同型の Human-gate 規則)。
- 静的解析の限界として、**runtime での sandbox / sanitization** は graph 上に現れないため検出できない。Confidence 0.9 でも true positive 確定ではなく、ユーザーがレビューすべき構造的攻撃面の提示に留まる。
- `category` 以外の検出経路として `Config["tool"]` ∈ {eval, exec, code_interpreter, python_runner, shell} と name regex (`(?i)(eval|exec|code[_]?runner|python[_]?runner|shell|bash)`) もある。実装は `domain/rules/eval_missing.go:isEvalSink`。
