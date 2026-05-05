# dynamic_node_construction — 設計・実装ドキュメント

> **対象バージョン**: Phase 2 #3 (v0.6 系列)
> **実装ファイル**: `domain/rules/dynamic_node_construction.go`
> **テスト**: `domain/rules/dynamic_node_construction_test.go` (16 ケース、サブテスト込み)
> **層 (ADR-007)**: Local rule — `OnAny` listener + recursive Config scan

---

## 1. 背景・動機

LangGraph や ADK-Go のようなノードベース DSL では、 「ノードの中身」を **文字列として宣言** することがある:

```python
# LangGraph の典型的アンチパターン
graph.add_node("dispatcher", lambda payload: eval(payload['code']))
```

この `lambda x: eval(x)` のような body は、外部からみると単なる文字列だが runtime で実行されるため:

- **静的解析が中身に踏み込めない** — `eval` が何を実行するか graph レベルでは分からない
- **prompt injection / RAG poisoning と組み合わさると RCE** — `payload['code']` が攻撃者制御の文字列なら任意コード実行
- **secret_exposure_scanner / pii_leak_scanner などのルールも貫通** — 動的にしか分からない攻撃面が増える

本ルールは Node.Config 内の文字列値を再帰的に scan し、 `eval(`/`exec(`/`Function(`/`compile(`/`__import__(`/`getattr(`/`setattr(` の **存在自体** を Severity 別に Finding で表面化する。

> **位置付け**: 兄弟ルール `eval_missing` が「LLM ノード → コード実行 Tool への構造的到達可能性」を見るのに対し、本ルールは「Config 値内の文字列リテラル」を見る。両者は補完関係 — 構造的に sink がなくても Config に `eval(` が直書きされていれば本ルールが拾う、Config に何もなくても LLM が code_execution Tool に流れていれば eval_missing が拾う。

---

## 2. 検出対象

### 2.1 走査対象 Config キー

以下の curated set のみ走査する:

| キー | 用途例 |
|---|---|
| `body` | LangGraph `add_node(name, body)` の body 文字列 |
| `fn` | function spec (FastAPI / RPC) |
| `handler` | handler 関数定義 |
| `callback` | callback コード |
| `code` | code spec、code interpreter argument |
| `factory` | factory function string |
| `builder` | builder function string |

`description` / `model` / `prompt_template` のような自由テキスト・パラメータは **意図的に除外**。例えば description に "Wraps eval() calls safely" と書かれていても発火しない (ドキュメント文字列を誤検知しない設計判断)。

### 2.2 検出パターン

| パターン | 正規表現 | Severity | Confidence | ConfidenceReason |
|---|---|---|---|---|
| `eval(` | `\beval\s*\(` | **Critical** | 0.95 | `exact_static_match` |
| `exec(` | `\bexec\s*\(` | **Critical** | 0.95 | `exact_static_match` |
| `Function(` | `\bFunction\s*\(` | **Critical** | 0.95 | `exact_static_match` |
| `compile(` | `\bcompile\s*\(` | **Warning** | 0.85 | `exact_static_match` |
| `__import__(` | `__import__\s*\(` | **Warning** | 0.85 | `exact_static_match` |
| `getattr(` | `\bgetattr\s*\(` | **Info** | 0.6 | `heuristic_pattern` |
| `setattr(` | `\bsetattr\s*\(` | **Info** | 0.6 | `heuristic_pattern` |

`\s*` を関数名と `(` の間に挟んでホワイトスペース許容 (`eval ( payload )` も検出)。

### 2.3 Severity collapsing

1 つの Config 値に複数パターンがマッチする場合 (例: `getattr(obj, 'cmd')(eval(payload))`) は **最高 Severity 1 個に collapse**。`getattr` Info + `eval` Critical → Critical 1 件のみ発火。これにより 「複合的に危険」という事実は伝えつつ、 Finding の数で判断を曇らせない。

### 2.4 Placeholder strip-then-recheck

`secret_exposure_scanner.hasActualSecret` と同形のロジック:

- **placeholder のみ** (`${EVAL_FN}` / `{{handler}}` / `process.env.X` / `os.Getenv(...)`): skip (本物のコードではなく runtime 注入の参照値)
- **mixed** (`eval(${PAYLOAD})`): 発火 — placeholder を除いても `eval(` が残るので strip-then-recheck で生き残る

この方針は false positive を抑えつつ、 `eval(${PAYLOAD})` のような **placeholder で隠そうとしている** 攻撃面も拾える。

---

## 3. 検出アルゴリズム

```
Step 1: 各 Node に対して OnAny listener が発火 (1walk dispatcher)
Step 2: Node.Config を走査
        - キーが dynamicScanKeys に含まれない → スキップ
        - 含まれる → recursive scan (string / map / slice すべて深く辿る)
Step 3: leaf 文字列に対して collectStringHits
        - 文字列が空 → skip
        - placeholderPattern.MatchString && !hasActualDynamicPattern → skip
          (placeholder のみで実コードを含まない)
        - 各 dynamicPatterns 要素を順番に regex match → ヒットしたら hits に追加
Step 4: hits が空でなければ、最高 Severity の hit を選んで Finding を発火
        (1 つの key につき最大 1 件)
```

**計算量**: O(V × cfg) — V = ノード数、cfg = Config の値数 (再帰深さ含む)。 secret_exposure_scanner と同次元。

---

## 4. 実装の設計判断

### 4.1 走査キーを curated set に絞る理由

完全走査だと `description` / `system_prompt` / `instruction` 等の自由テキスト中の `eval()` 言及をすべて拾ってしまう。例えば「This tool wraps the python `eval()` function safely.」というドキュメント文字列は技術的に `eval(` regex にマッチするが、攻撃面ではない。**意図的に narrow** することで FP 抑制を優先した。

走査対象が足りない場合は `dynamicScanKeys` map に追加するだけの 1 行 patch 拡張可能。

### 4.2 Severity collapsing で per-key 1 件にする理由

`getattr(obj, 'cmd')(eval(payload))(__import__('os'))` のような攻撃文字列に対し、3 件 (Critical + Warning + Info) を吐くと「複数アラート = 別問題」と誤解されやすい。 **1 つの値 = 1 件 (最高 Severity)** に丸めることで「ここにヤバい構造が 1 つある」という認知に揃える。

複数の異なる Config キーで eval が出ていれば `body` と `handler` で別々に発火する (Case 14 でテスト済み)。

### 4.3 placeholder の扱いを secret_exposure と共有する理由

`hasActualDynamicPattern` は `secret_exposure_scanner.placeholderPattern` を再利用している。これにより `${VAR}` / `{{var}}` / `process.env.X` / `os.Getenv(` の安全な参照は両ルールで一貫してスキップされる。一方を変更するともう一方にも影響するので、共有による偶然の整合性は **保守容易性 > 結合度懸念** と判断した。

### 4.4 NodeType を絞らない (`OnAny`) 理由

Tool ノードが本命だが、 LLM ノードの `prompt_template` 内に lambda body が紛れているケース (LangChain 旧 API)、Output ノードの後処理スクリプト、 Human ノードの review function など、実例ごとに型が違う。 **OnAny で全種を walk し、走査キーで絞る** ほうが移植性が高い。

---

## 5. 推奨される対策パターン

```python
# NG: lambda x: eval(x)  ← 本ルールが Critical で発火
def dispatch_via_eval(payload):
    return eval(payload['code'])

# OK: 明示的 dispatch table
HANDLERS = {
    "sum":  lambda args: args['a'] + args['b'],
    "diff": lambda args: args['a'] - args['b'],
}
def dispatch_via_table(payload):
    op = payload['op']
    if op not in HANDLERS:
        raise ValueError(f"unknown op {op}")
    return HANDLERS[op](payload['args'])

# OK 別解: schema-validated function calling (OpenAI / Anthropic Tool Use)
# LLM が tools array に列挙された関数だけを名前指定で呼び出す。eval は介在しない。

# OK 最後の選択肢: sandboxed evaluator
# - PyPy sandbox / WASI / Vercel Sandbox / Firecracker microVM
# - allow-list import + memory cap + CPU cap
# 静的解析側からは sink 自体は残るので Critical は出るが、運用側で "sandboxed" を
# 表す独自 category (例: "sandboxed_code") を導入して allow-list する。
```

ライブラリ別の対策:

| ライブラリ | 推奨 API |
|---|---|
| LangChain | `RunnableLambda` の引数に **import 済み Python callable** を直接渡す。文字列の eval は使わない。 |
| LangGraph | `add_node(name, callable)` で `callable` は静的に import された関数。`lambda x: eval(...)` は `add_node` 自体の引数として禁止する CI ルール推奨。 |
| OpenAI / Anthropic | Tool Use / function calling — schema-typed argument。`tools=[...]` を介して allow-list dispatch |
| ADK-Go | `agent.RunnerFn` を Go の関数型として直接渡す (文字列代入不要) |

---

## 6. 既知の False Positive / False Negative

### False Positive

- **コメントとして書かれた eval**: `description` は除外されるので発火しないが、`body` のような scan 対象キーに `// example: eval(x) is dangerous` のようなコメントが混ざると現在の regex は中身まで見るので発火する。Confidence 0.95 で出るので `--min-confidence 1.0` (将来 reserved for absolutes) では抑制不能。コメント剥がしを追加する余地あり (YAGNI で defer)。
- **正規表現リテラル**: Go の `regexp.MustCompile("eval\\(...\\)")` のような文字列が `body` 等に直書きされていると発火する。命名規約で `regex_pattern` のようなキーを別途用意する運用が望ましい。

### False Negative

- **間接的な eval 呼び出し**: `func = "eval"; getattr(builtins, func)(...)` のような **名前を文字列で渡すパターン** は `eval(` の literal に当たらないので検出不能。 `getattr` は Info で出るので人間の眼で連鎖を読むしかない。
- **Encode された eval**: `bytes.fromhex('6576616c28...')` のような hex / base64 経由で隠された eval は検出不能。今後 secret_exposure_scanner と同じく entropy heuristic を加える余地あり。
- **C 拡張経由**: Cython / native の eval-相当機能 (`ctypes.cdll.eval(...)`) は検出不能。
- **Scan 対象キー外**: `prompt_template` / `model` / `description` のような自由テキストキーに直書きされた eval は意図的に検出しない (Section 4.1 参照)。

---

## 7. 関連ルール / ADR

- **ADR-006**: ESLint visitor pattern — 本ルールは `OnAny` を使う Local tier の典型例。
- **ADR-007**: Local / Path / Global の3層分離 — Config 値レベルの判定なので Local tier。
- **ADR-008**: ConfidenceReason — Critical / Warning は `ReasonExactStaticMatch` (regex 完全一致)、Info は `ReasonHeuristicPattern` (動的属性アクセスは間接的シグナル)。
- `eval_missing` (`docs/rules/eval-missing.md`) — sibling Path rule、構造的攻撃面 (LLM → code_execution Tool reachability) を担当。本ルールが文字列レベル攻撃面、 eval_missing が構造的攻撃面という補完関係。
- `secret_exposure_scanner` (`docs/secret-detection.md`) — 同じ Local + recursive scan + placeholder strip-then-recheck 構造。`placeholderPattern` を共有。
