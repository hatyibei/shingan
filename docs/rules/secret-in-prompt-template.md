# secret_in_prompt_template — 設計・実装ドキュメント

> **対象バージョン**: Phase 2 #5 (v0.6 系列)
> **実装ファイル**: `domain/rules/secret_in_prompt_template.go`
> **テスト**: `domain/rules/secret_in_prompt_template_test.go` (18 ケース)
> **層 (ADR-007)**: Local rule — `OnNode[NodeTypeLLM]` で 1 node 完結判定

---

## 1. 背景・動機

prompt engineering の試行錯誤中に **API key を system prompt に貼り付けたまま commit してしまう** ミスは典型的。漏洩経路は:

- **Git レポジトリ**: 履歴に残り続ける
- **ログ**: workflow 実行ログ・LLM provider の trace
- **Export**: `langsmith` / `langfuse` / `wandb` などの workflow export
- **Third-party 推論**: 外部 SDK が prompt を保存するケース

`secret_exposure_scanner` (broad な OnAny scan) と異なり、本ルールは **prompt-template 専用の Suggestion** を出すことで「env-var 置換 + rotation」という具体的な修正手順を提示する点に存在価値がある。

---

## 2. 検出対象

### 2.1 走査対象キー (LLM ノードに限定)

`NodeType.LLM` のみ対象。Config 内の以下の **4 キー** だけを走査:

| キー | フレームワーク例 |
|---|---|
| `system_prompt` | OpenAI Messages API, Anthropic Claude |
| `prompt_template` | LangChain `PromptTemplate`, ADK-Go |
| `user_message_template` | カスタム実装 |
| `instruction` | LangChain Agent, ADK-Go LlmAgent |

汎用 `prompt` キーは **走査しない** (`secret_exposure_scanner` の OnAny 再帰スキャンが既にカバーしているため重複を避ける)。

### 2.2 検出パターン

| Pattern | Severity | Confidence | Reason |
|---|---|---|---|
| AWS access key (`AKIA[0-9A-Z]{16}`) | Critical | 0.95 | exact_static_match |
| Private key PEM (`-----BEGIN ... PRIVATE KEY-----`) | Critical | 0.95 | exact_static_match |
| Anthropic API key (`sk-ant-[A-Za-z0-9_-]{20,}`) | Critical | 0.95 | exact_static_match |
| OpenAI API key (`sk-[A-Za-z0-9]{20,}`) | Critical | 0.95 | exact_static_match |
| GitHub token (`gh[pousr]_[A-Za-z0-9]{36,}`) | Critical | 0.95 | exact_static_match |
| JWT (`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`) | Warning | 0.7 | heuristic_pattern |

`sk-ant-` パターンは `sk-` の前に評価することで、より specific な分類を優先する。

### 2.3 Placeholder の除外

以下の placeholder は秘密検出前に **regex で stripped** されるので false positive にならない:

- `$VAR`, `${VAR}` (shell)
- `{{anything}}` (Mustache / Handlebars / LangChain)
- `process.env.VAR_NAME` (Node.js)
- `os.Getenv(` (Go)

例えば `Authorization: Bearer ${OPENAI_API_KEY}` は完全に exempt。**ただし mixed (`sk-abc${SUFFIX}`) は stripped した後 `sk-abc` がパターン未満となるため発火しない可能性あり** — 実装としては保守的側に倒している。

### 2.4 Dedup

(node, key) ごとに **最初の 1 finding** だけ emit。同一 system_prompt に AWS key と OpenAI key が混在していても 1 finding (より specific な AWS key 側)。これは多重 finding 抑制で `secret_exposure_scanner` と同じ方針。

### 2.5 Redaction

Message / Suggestion に直接埋め込む secret は `redactSecret()` で先頭 6 文字 + `***` に圧縮:

```
node "leaky" config["system_prompt"] contains a hardcoded openai_api_key (sk-abc***)
```

privacy / log-safety のため。reviewer が source 側で grep して場所を確認するには十分な情報。

---

## 3. `secret_exposure_scanner` との棲み分け

| ルール | 走査範囲 | finding 種類 |
|---|---|---|
| `secret_exposure_scanner` | 全 Config 値 (OnAny 再帰)、9 種の secret regex | Critical/Warning/Info 多種、generic Suggestion |
| `secret_in_prompt_template` | LLM ノードの 4 prompt-template キーのみ | Critical/Warning 主体、prompt-specific Suggestion (env-var + rotation) |

**設計上の重複** が発生する: 例えば LLM ノードの `system_prompt` 値に `sk-...` が入っていると **両方発火する**。これは仕様であり、bug ではない。両ルールの Suggestion テキストが異なるため、ユーザは「どこで何を直すべきか」を異なる視点から把握できる。

`--rules secret_exposure_scanner --rules secret_in_prompt_template` のように個別有効化したり、`--min-confidence 0.95` で重複ノイズを下げる運用が可能。

---

## 4. False Positive / False Negative

### False Positive

- **Test fixture / mock secret**: テスト用の "fake" API key が system_prompt に入っているケース。`--min-confidence 0.95` でも JWT 以外は素通りしてしまう。回避策: `${TEST_API_KEY}` 形式に書き換える、もしくは Markdown reporter の `# baseline:` directive で個別に suppress (将来導入予定の baseline 機能)。
- **Documentation literal**: prompt の中で「sk-... の形式の API key を使ってね」と説明する instruction text は誤検出される。 false positive の典型源だが、現状トレードオフ。

### False Negative

- **Generic `prompt` キー**: 本ルールは scope 外。`secret_exposure_scanner` がカバー。
- **動的に組み立てた prompt**: コード側で `system_prompt = build_prompt(secret)` のように runtime 構築している場合は Config に static 値が現れず検出できない。フレームワーク固有 parser (LangGraph / ADK-Go) で AST レベルの追跡が必要。
- **Placeholder の中に secret 隣接** (`sk-abc${SUFFIX}`): stripped した後 6 文字未満で regex が当たらない。安全側に倒した結果。

---

## 5. 関連 ADR

- **ADR-006**: ESLint visitor pattern — Local rule の OnNode dispatcher を使用。
- **ADR-007**: Local / Path / Global の3層分離 — 本ルールは Local tier。
- **ADR-008**: ConfidenceReason — exact_static_match (Critical 5パターン) と heuristic_pattern (JWT) を使い分け。
- **ADR-010**: Plugin SDK internal-only (v1.0 まで) — 本ルールも `init()` で `registerBuiltin()` 経由の登録。
