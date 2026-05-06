> 🌐 Language: [English](./confidence-scoring.md) | **日本語**

# Confidence Scoring (v0.4)

Shingan の Confidence スコアは、各 Finding がどれだけ「真陽性」である可能性が高いかを 0.0〜1.0 で表す指標です。

## 設計思想: Severity と Confidence の2軸

```
              Severity (重大度)
              ↑
 Critical     │  loop_guard (1.0)   ← 確定的かつ重大
              │  error_handler (0.8)
 Warning      │  redundant (0.9)    ← 確信度高いが軽微
              │  pii_leak RAG (0.6)
 Info         │  cost (0.7)         ← ヒューリスティック
              └──────────────────→ Confidence (確信度)
                 0.0   0.5   1.0
```

- **Severity**: 問題が発生したときの影響度 (Info / Warning / Critical)
- **Confidence**: このアラートが実際に問題である確率

この2軸の分離により「重大だが不確かな警告」と「軽微だが確実な通知」を明確に区別できます。

## 各ルールの信頼度根拠

| Rule ID | Confidence | 根拠 |
|---------|-----------|------|
| `cycle_detection` | **1.0** | DFS back-edge検出は数学的に確定。サイクルが存在すれば必ず検出 |
| `loop_guard` | **1.0** | `Config["max_iterations"]`の有無チェックは確定的 |
| `unreachable_node` | **1.0** | BFS到達性は確定。エントリから辿れないノードは常に正しく検出 |
| `error_handler_checker` | **0.8** | 2ホップ先のConditionノードチェック + `reliable` フラグで改善済みだが、3ホップ以上のパターンは未検出 |
| `redundant_llm_call` | **0.9** | `prompt_template` の完全一致は強い根拠。nil/空文字列スキップで誤検知を排除 |
| `cost_estimation` | **0.7** | モデル価格階層 (High/Mid/Low) はロードマップ依存。モデル名の変更やプロバイダー値引きで変動 |
| `pii_leak_scanner` | **0.6** (RAG/has_pii) | `category=rag` または `has_pii=true` は明示的なフラグ。精度は高い |
| `pii_leak_scanner` | **0.3** (名前ヒント) | ノード名に "user"/"pii"/"personal" を含むだけでは弱い根拠。命名規則に依存 |
| `secret_exposure_scanner` | **0.95** (Critical/Warning) | AWS AKIA prefix, sk-ant-, OpenAI sk- などは非常に特定的なパターン |
| `secret_exposure_scanner` | **0.5** (Info) | `password=XXX` / JWT の汎用パターンは誤検知率が高い |

## CI統合例: Critical かつ Confidence >= 0.9 のみブロック

```yaml
# .github/workflows/shingan.yml
- name: Shingan Static Analysis
  run: |
    shingan analyze \
      --format json \
      --input workflow.json \
      --output sarif \
      --output-file results.sarif \
      --min-confidence 0.9
  # exit code 2 = Critical findings with confidence >= 0.9
  # exit code 1 = Warning only → allow merge
  # exit code 0 = clean

- name: Upload SARIF
  uses: github/codeql-action/upload-sarif@v3
  with:
    sarif_file: results.sarif
```

このワークフローでは:
- `cycle_detection` (1.0), `loop_guard` (1.0), `unreachable_node` (1.0), `redundant_llm_call` (0.9) → ブロック対象
- `error_handler_checker` (0.8), `cost_estimation` (0.7), `pii_leak_scanner` (0.3–0.6) → 通過 (レビュー推奨)

## JSON 出力サンプル

```json
{
  "findings": [
    {
      "rule": "cycle_detection",
      "severity": "critical",
      "node_id": "loop_ctrl",
      "confidence": 1.0,
      "message": "Loop node \"loop_ctrl\" has a cycle but max_iterations is not set"
    },
    {
      "rule": "pii_leak_scanner",
      "severity": "warning",
      "node_id": "api_sink",
      "confidence": 0.3,
      "message": "potential PII leak: path from RAG/PII node..."
    }
  ],
  "summary": {
    "total": 6,
    "critical": 3,
    "warning": 2,
    "info": 1,
    "high_confidence_count": 3
  }
}
```

## SARIF 出力 (GitHub Code Scanning)

- `result.properties.confidence`: 各検出の信頼度 (float)
- `rule.properties.precision`: ルール精度ラベル
  - `"high"`: Confidence >= 0.9
  - `"medium"`: 0.6 <= Confidence < 0.9
  - `"low"`: Confidence < 0.6

## v0.5 予定: 機械学習による動的 Confidence 調整

- 実際のコードベースからのフィードバック (true positive / false positive) を収集
- ルールごとの Precision-Recall を計測して Confidence を動的更新
- `cost_estimation` については LLM プロバイダーの価格 API と連携してリアルタイム更新
