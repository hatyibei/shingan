# prompt_injection_sink — 設計・実装ドキュメント

> **対象バージョン**: Phase 2 #1 (v0.6 系列)
> **実装ファイル**: `domain/rules/prompt_injection_sink.go`
> **テスト**: `domain/rules/prompt_injection_sink_test.go` (12 ケース、サブテスト込み)
> **層 (ADR-007)**: Path rule — Sources / Sinks / Propagate を実装

---

## 1. 背景・動機

LLM エージェント設計における最大の脆弱性カテゴリは **prompt injection** である:

- 攻撃者が制御可能な文字列が LLM の system prompt に直接連結されると、LLM はその「指示」をシステム命令として解釈してしまい、本来の policy を上書きされる。
- 結果として、**認証情報の漏洩**、**他テナント DB へのアクセス**、**禁止操作の実行 (tool abuse)**、**jailbreak** が発生する。

このルールは、ワークフローグラフの **静的構造** から「ユーザ入力ノード → LLM の system prompt フィールド」という到達可能性を検出し、設計時に **runtime sanitization に頼る前** に問題を顕在化させる。

> **静的解析の限界**: 文字列レベルの sanitization (escape / validate / classify) を runtime で行っているケースは検出できない。本ルールは「構造的な攻撃面」を可視化するに過ぎず、「実際に攻撃可能かどうか」までは断定しない。Confidence 0.9 を割り当てているのはそのため。

---

## 2. 検出対象

### 2.1 Source (ユーザ入力ノード)

以下のいずれかに該当するノードを source とみなす:

| 判定条件 | 例 |
|---|---|
| `Config["source"] == "user_input"` | 任意の NodeType。Config フラグが最も明示的 |
| Name / ID が正規表現 `(?i)^(user[_\-].*\|.*[_\-]input\|query\|request\|user_query\|user_request)$` に一致 | `user_query`, `chat_input`, `query`, `request` |

NodeType は問わない。実装上は Tool / LLM / Output いずれの type でもパターンが当たれば source 扱いになる。

### 2.2 Sink (LLM プロンプトテンプレートノード)

`NodeType.LLM` のノードのうち、以下の Config キーが空でないものを sink とみなす:

| キー | 区分 |
|---|---|
| `system_prompt` | system tier |
| `system` | system tier |
| `instruction` | system tier |
| `instructions` | system tier |
| `prompt_template` | user tier |
| `user_message_template` | user tier |
| `user_template` | user tier |
| `prompt` | user tier |

**substitution 検出**: 値の中に `{{var}}` / `${var}` / `{var}` のいずれかのテンプレート置換構文が含まれているかを正規表現で判定する。

### 2.3 Severity

Severity は **sink 分類時** に決定し、path 解析時の情報には依存しない:

| Sink 区分 | substitution | Severity | Confidence | ConfidenceReason |
|---|---|---|---|---|
| system tier | あり | **Critical** | 0.9 | `heuristic_pattern` |
| system tier | なし | **Warning** | 0.7 | `heuristic_pattern` |
| user tier | あり | **Info** | 0.5 | `heuristic_pattern` |
| user tier | なし | (sink でない) | — | — |

「user tier × substitution なし」を sink にしないのは、テンプレート無し prompt フィールドはほぼ確実に静的文字列であり、ユーザ入力の連結は起こり得ないため。

---

## 3. 検出アルゴリズム

PII leak rule (ADR-007 Path tier の reverse-BFS) と同型の構造:

```
Step 1: Sources(g) でユーザ入力ノードを抽出 (O(V))
Step 2: Sinks(g) で LLM template ノードを抽出 + 各 sink の severity を classifySink で決定 (O(V))
Step 3: 各 sink から逆方向 BFS。reverse adjacency は ctx.Reverse を共有 (PathRule の規約)。
        途中で source ノードに当たれば 1 finding を発火 (sink 分類が確定した severity を使う)。
Step 4: PII rule のような Human-gate 境界はない。任意の構造的到達可能性 = finding。
```

**計算量**: O(sinks × (V+E))。典型ワークフローでは sinks << V のため実質 O(V+E)。

**重複排除**: BFS は `visited` セットで再訪問を防ぐので、同じ (sink, source) ペアの finding は一度しか出ない。一方、複数の source から同じ sink に到達できる場合、それぞれ別 finding として個別に発火する (ユーザ目線で「どの入口がリスクか」を把握できるよう)。

---

## 4. 実装の設計判断

### 4.1 「Human gate を境界にしない」理由

PII leak は GDPR / CCPA の文脈で「人間が承認すれば外部送信して良い」という業務フローが成立する。一方 prompt injection は **承認しても危険性は変わらない** (むしろ攻撃者が承認画面を経由して刷り込むケースを想像する必要がある)。`NodeTypeHuman` を boundary にしないのが安全側のデフォルト。

将来的に `sanitizer` カテゴリのような明示ノードを境界扱いするオプションを追加する余地あり (YAGNI で defer)。

### 4.2 source / sink 検出が両方ヒューリスティックである件

両者ともに **名前 / Config キーのパターンマッチ** に依存し、taint propagation 等の意味解析は行っていない。これを反映して `ConfidenceReason = ReasonHeuristicPattern`、Confidence は 0.5–0.9 の幅に収めている。

これは ADR-008 の「Reason を全 Finding に持たせる」原則に従ったもので、ユーザは `--min-confidence 0.8` で False Positive を抑制でき、Reason を見て調整方針 (parser 改善 / DSL 拡張 / type annotation) を決められる。

### 4.3 `system_prompt` / `system` / `instruction(s)` を「system tier」と一括した理由

LangChain / LlamaIndex / ADK-Go / Vercel AI SDK / Anthropic Messages API のそれぞれで使われるキー名のうち、 system role を意図する代表的な名称を網羅した。`prompt_template` 単独だと user メッセージ用テンプレートと曖昧になるため別 tier に分離。

### 4.4 substitution 構文の正規表現

```
(\{\{[^}]+\}\}|\$\{[^}]+\}|\{[A-Za-z_][A-Za-z0-9_\.]*\})
```

- `{{var}}`: Mustache / Handlebars / LangChain 系
- `${var}`: JS template literal / Vercel AI SDK 系
- `{var}`: Python `str.format` / f-string flag (識別子のみ、JSON/数式の `{...}` を巻き込まないよう conservative)

3 種の混在テンプレート (`hi {{a}}, ${b}, {c}`) も全て検出される。

---

## 5. 推奨される対策パターン

LLM SDK 側で **メッセージ配列 (`messages`) で role:user / role:system を分離する** API を使うと、system は静的文字列、user 部分は別フィールドという明確な構造になる。これによって以下のような Anti-Injection な設計が可能:

```python
# OK: user content と system instruction は別レイヤ
client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[
        {"role": "system", "content": "You are ACME's support assistant."},
        {"role": "user",   "content": user_input},  # ← 別フィールド
    ],
)

# NG: system に user input を str.format で混ぜている
client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[
        {"role": "system", "content": f"You are ACME's support assistant. User said: {user_input}"},
    ],
)
```

ライブラリ側のサポート例:

| ライブラリ | 推奨 API |
|---|---|
| Anthropic SDK | `messages=[{"role":"system",...},{"role":"user",...}]` (Messages API) |
| OpenAI SDK | 同上 (chat.completions, system/user メッセージ分離) |
| LangChain | `ChatPromptTemplate.from_messages([("system","..."),("user","{user_query}")])` で input variable を user 側だけに |
| Vercel AI SDK | `streamText({ messages, system })` で system 引数を専用化 |
| ADK-Go | `LlmAgent.Instructions` (system) と `Input` (user) を別フィールドで保持 |

加えて defensive layer として:

1. **Input length cap** — 異常に長い入力を最初に弾く (10K-50K chars 以上の prompt は injection の温床)
2. **Schema validation** — JSON schema や regex で type/shape を強制し、自由文以外は早期に reject
3. **Allow-list output classification** — 危険な出力 (tool 呼び出しの引数、SQL クエリ) を出力時にも分類器でフィルタ

---

## 6. 既知の False Positive / False Negative

### False Positive

- **`prompt_template` に substitution があるが、テンプレ側で role 分離された LLM 呼び出し**: 静的解析では LLM SDK 側の挙動まで見えないため、Info で出る。Confidence 0.5 の根拠もここにある。`--min-confidence 0.8` で抑制可能。
- **名前パターンの誤マッチ**: たとえば `customer_request` は意図的に `request` 末尾を含むので user-input と分類される。ビジネスロジック上「内部生成された RPC リクエスト」のケースもありうるが、命名を変更するか source キーを明示することで FP を回避できる。

### False Negative

- **動的なノード生成**: `conditional_edges` / runtime でノードを差し込む LangGraph 構文では、static graph 上に source ノードが現れず検出できない。
- **多段プロンプトテンプレート結合**: `prompt = base + user_query` のような string concatenation を直接 `system_prompt` に入れている場合、Config 上は静的文字列しか見えないため検出できない (この場合は `secret_exposure_scanner` などの別系統で間接的に拾う)。
- **非標準キー名**: `messages_template` / `instr` / 独自 schema キーは対象外。フレームワーク固有 parser (LangGraph / ADK-Go) で構造化メッセージ配列を直接読むのが王道。

---

## 7. 関連ルール / ADR

- **ADR-006**: ESLint visitor pattern — Local rule との対比。本ルールは Path tier。
- **ADR-007**: Local / Path / Global の3層分離 — 本ルールが Path tier に属する根拠。
- **ADR-008**: ConfidenceReason — `ReasonHeuristicPattern` を採用。
- `pii_leak_scanner` (`docs/pii-detection.md`) — 同じ Path rule の先行実装、reverse-BFS の鋳型。
- `secret_exposure_scanner` (`docs/secret-detection.md`) — Local rule、Config 値レベルの正規表現マッチ。
