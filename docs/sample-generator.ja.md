> 🌐 Language: [English](./sample-generator.md) | **日本語**

# shingan-gen — Sample Workflow Generator

`shingan-gen` は開発者が Shingan の静的解析をすぐに試せるよう、ランダムまたは意図的なパターンの WorkflowGraph JSON を生成するCLIツールです。

## インストール / ビルド

```bash
go build -o shingan-gen ./cmd/shingan-gen
# または
make gen-cli
```

## 使い方

```bash
shingan-gen --pattern <name> --size <N> --seed <S> --output <path>
```

### フラグ

| フラグ | デフォルト | 説明 |
|--------|-----------|------|
| `--pattern` | `random` | 生成パターン（下記参照） |
| `--size` | `10` | ノード数（random/clean/unreachable/cycle に適用） |
| `--seed` | `42` | 乱数シード（再現性のため固定する） |
| `--output` | stdout | 出力ファイルパス（`-` または省略でstdout） |

## パターン一覧

### `random` — ランダムグラフ

意図的なバグを含む大規模グラフ。`GenerateRandomGraph` と互換性あり。

```bash
shingan-gen --pattern random --size 50 --seed 42 > random.json
shingan analyze --format json --input random.json --output markdown
```

**期待されるFindings**: 複数のCritical/Warning（cycles, loop_guard, unreachable, pii_leak など）

---

### `clean` — 問題なしグラフ

全7ルールをパスする構造的に正しいグラフ。
新ルール実装時の「falseポジティブがないこと」の確認に使用します。

```bash
shingan-gen --pattern clean --size 20 --seed 42 > clean.json
shingan analyze --format json --input clean.json --output markdown
# 期待: 0 findings
```

**期待されるFindings**: なし（0件）

---

### `buggy` — 全7ルール発火

全ての分析ルールが少なくとも1件のFindingを返すよう設計されたグラフ。

```bash
shingan-gen --pattern buggy --seed 42 > buggy.json
shingan analyze --format json --input buggy.json --output markdown
# 期待: 7種のルールすべて発火
```

**期待されるFindings**:

| ルール | Severity | 理由 |
|--------|----------|------|
| `cycle_detection` | Critical | max_iterations なしのLoopノードがサイクルを形成 |
| `loop_guard` | Critical | LoopAgentにmax_iterations未設定 |
| `unreachable_node` | Warning | dangling_node (LLM)がエントリから到達不能 |
| `error_handler_checker` | Warning | api_toolが無条件エッジのみ（エラーハンドリングなし） |
| `cost_estimation` | Warning | gpt-4oノードが無限ループ内に存在 |
| `redundant_llm_call` | Warning | 同一(model, prompt_template)のLLMノードが2つ |
| `pii_leak_scanner` | Warning | RAGツール → 外部APIへのパスにHuman gateなし |

---

### `infinite-loop` — LoopGuard発火パターン

`max_iterations` 未設定の LoopAgent がサイクルを形成するグラフ。

```bash
shingan-gen --pattern infinite-loop --seed 42 > infinite-loop.json
shingan analyze --format json --input infinite-loop.json --output markdown
```

**期待されるFindings**:
- `loop_guard`: Critical — `unbounded_loop` に max_iterations なし
- `cycle_detection`: Critical — Loop ノードがサイクルを形成しているが max_iterations 未設定

---

### `unreachable` — unreachable_node発火パターン

エントリノードから到達できない孤立ノードを含むグラフ。

```bash
shingan-gen --pattern unreachable --size 15 --seed 42 > unreachable.json
shingan analyze --format json --input unreachable.json --output markdown
```

**期待されるFindings**:
- `unreachable_node`: Warning — `dangling_llm` (LLM型) が到達不能
- `unreachable_node`: Warning — `dangling_tool` (Tool型) が到達不能

---

### `pii-leak` — PIILeakScanner発火パターン

RAGツールから外部API（Human gateなし）へのパスを持つグラフ。

```bash
shingan-gen --pattern pii-leak --seed 42 > pii-leak.json
shingan analyze --format json --input pii-leak.json --output markdown
```

**期待されるFindings**:
- `pii_leak_scanner`: Warning — `user_data_rag` → `external_api` にHuman gateなし
- `error_handler_checker`: Warning — Tool ノードの条件付きエッジ不足

---

### `cycle` — 純粋なサイクルグラフ

Loop/LoopAgent ノードで保護されていない生のサイクル（グラフ定義エラー）。

```bash
shingan-gen --pattern cycle --size 4 --seed 42 > cycle.json
shingan analyze --format json --input cycle.json --output markdown
```

**期待されるFindings**:
- `cycle_detection`: Critical — 非Loopノードが親 Loop ノードなしでサイクルを形成

---

## パイプで使う

```bash
# buggy グラフを直接 shingan analyze に渡す
shingan-gen --pattern buggy | shingan analyze --format json --input /dev/stdin --output markdown

# clean グラフを検証
shingan-gen --pattern clean --size 50 | shingan analyze --format json --input /dev/stdin --output json | jq '.findings | length'
# 期待出力: 0
```

## Makefile ターゲット

```bash
# shingan-gen をビルド
make gen-cli

# 任意のパターンを生成（stdout）
make sample-buggy
make sample-clean
make sample-pii-leak
# など
```

## 教育目的での活用

### 新ルール実装時のフロー

1. `shingan-gen --pattern clean` を使って新ルールがfalse positiveを出さないことを確認
2. 新ルール用のパターンを `domain/testutil/generate.go` に追加
3. `testdata/generated/` に期待サンプルを追加

### テストフィクスチャとして

各パターンファイルは `shingan analyze` の E2E テストで入力として使用できます:

```go
testdata := "../../testdata/generated/buggy-seed42.json"
findings := runAnalysis(t, testdata)
if !hasCriticalFinding(findings, "cycle_detection") {
    t.Error("expected cycle_detection Critical")
}
```

## JSON フォーマット

`shingan-gen` が出力する JSON は `shingan analyze --format json` と互換性があります。
ノードは配列として出力されます（ID順にソート済み）:

```json
{
  "nodes": [
    {"id": "entry", "name": "entry", "type": "llm", "config": {"model": "gpt-4o-mini"}}
  ],
  "edges": [
    {"from": "entry", "to": "output"}
  ],
  "entry_node_id": "entry"
}
```
