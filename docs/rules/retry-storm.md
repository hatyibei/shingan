# retry_storm — 設計・実装ドキュメント

> **対象バージョン**: Phase 2 (v0.6 系列、reliability batch)
> **実装ファイル**: `domain/rules/retry_storm.go`
> **テスト**: `domain/rules/retry_storm_test.go` (14 ケース)
> **層 (ADR-007)**: Path rule — Sources / Sinks / Propagate を実装 (Sinks は nil)

---

## 1. 背景・動機

retry をナイーブに使うと、外部 API が一過性の故障を起こした瞬間に **指数的な負荷増幅** を生む:

- `retries=5` の Tool ノードが 1 リクエスト → 故障時は 5 回追加で叩く (リトライ係数 5)
- そのノードに **20 並列** で流入していたら、瞬間的に `100 リクエスト` の濃縮スパイクになる
- 上流の API レート制限を踏み越え、IP-block / token budget exhaust / 連鎖故障を引き起こす

このパターンを **静的グラフから事前検出** するのが retry_storm。「retry 係数」と「並列度」の積を **blast radius (爆発範囲)** として、閾値超過を Severity に紐付ける。

> **静的解析の限界**: backoff_factor / circuit-breaker / shared rate limiter 等の **mitigation 効果** は graph に現れにくい。Confidence は heuristic 領域に留めるのはそのため (Warning / Info ともに 0.7 / 0.5)。Critical (blast >= 100) は数値計算が確定値なので exact_static_match。

---

## 2. 検出対象

### 2.1 Source (retry-storm 候補ノード)

`NodeType.Tool` のうち、以下のいずれかの Config キーが **>= 3** であるノード:

| Config キー | 目的 |
|---|---|
| `retries` | 一般的な retry 回数 (LangChain / LlamaIndex) |
| `max_retries` | requests / boto3 系の慣用キー |
| `retry_count` | tenacity / Polly 系 |

3 未満は「通常の transient failure 対策」と見なし source 化しない (false positive 抑制)。

複数キーが同時に存在する場合は、**最初にマッチした (>=3) 値** を採用 (優先順は配列順)。

### 2.2 Parallelism 推定 (3 シグナルの max)

並列度は **次の 3 種を独立した上限値とみなして max を取る**:

1. **fan-in count** — incoming edges の本数 (同時に流入する upstream call 数)
2. **source の `max_concurrency`** — Tool 自身が宣言する並列上限
3. **upstream 1-hop 直近のシグナル** — incoming edge の `From` ノードが
   - `NodeTypeLoop` で `max_iterations` を持つ → その値
   - `max_concurrency` を持つ → その値

最低値は 1 (孤立 source でも `retries × 1` で評価される)。

> **設計判断**: 「3 つを max で取る」のは worst-case storm を見るため。実 runtime 並列度は max を下回ることが多いが、retry_storm は「**起こりうる最悪のシナリオ**」を可視化するルールで、実測値を当てるルールではない。

### 2.3 Severity / Confidence マトリクス

`blast = retries × parallelism` で計算し、以下の閾値で発火:

| blast radius | Severity | Confidence | ConfidenceReason | 補足 |
|---|---|---|---|---|
| >= 100 | **Critical** | 0.9 | `exact_static_match` | 数値積で確定 |
| >= 30 | **Warning** | 0.7 | `heuristic_pattern` | parallelism 推定が保守的 |
| >= 10 | **Info** | 0.5 | `heuristic_pattern` | 参考レベル、`--min-confidence 0.7` で抑制可 |
| < 10 | (発火しない) | — | — | retry × parallelism が小規模 |

---

## 3. 検出アルゴリズム

```
Step 1: Sources(g) — Tool ノードを走査し retryCount() >= 3 のものを集める (O(V))
Step 2: Sinks(g) は nil。retry_storm は per-source 評価で path 解析を要しない
Step 3: Propagate(ctx) — 各 source について estimateParallelism(graph, src) を計算:
        - fan-in count を数える
        - src 自身の max_concurrency を読む
        - 各 incoming edge の From ノードを 1 hop 見て Loop.max_iterations / max_concurrency を読む
        - 3 シグナルの最大値を採用 (最低 1)
Step 4: blast = retries × parallelism を計算し、閾値マトリクスで Severity を選択。
Step 5: Finding を emit。
```

**計算量**: O(sources × E)。典型ワークフローでは sources << V のため実質 O(V+E)。

---

## 4. 実装の設計判断

### 4.1 Path tier に置いた理由

Local rule (1 node + Config だけ) では incoming edges に触れない。 retry_storm は upstream 1-hop を見るので、Local では不可能で、ESLint の listener API では表現困難。Global で graph 全域 DFS を組むのも過剰。

→ 「source 周辺の隣接情報を見る」用途に最適な Path tier に置く。Sinks は使わないが、`PathRule` interface の Sinks は nil 許容なので問題なし (CostAnalyzer が同型のパターン)。

### 4.2 `retries=2` を source 化しない理由

実運用ワークフローで `retries=1` ~ `retries=2` は「transient failure を 1 回だけ吸収する」用途で多用される。これは安全な使い方であり、retry_storm の警告対象から外すのが false positive 削減に直結する。閾値 3 は LangChain / OpenAI Python SDK のデフォルト (3) を踏まえた現実的な下限。

### 4.3 parallelism シグナルの max を取る理由

3 種のシグナルは「独立した上限値」であり、実 runtime はそれぞれの **min** に近い値で動く。しかし静的解析が見たいのは「**最悪のケース**」 (worst-case blast) なので max を取る。これにより:
- false negative を抑える (どのシグナルも見落とさない)
- false positive は ConfidenceReason / Severity で説明 (`heuristic_pattern` の Warning / Info)

### 4.4 Critical だけ `exact_static_match`、Warning/Info は `heuristic_pattern` の理由

blast >= 100 は **どんな mitigation を入れても storm として顕在化する** 規模。一方 30 ~ 99 の領域は backoff / circuit breaker で実害を抑え込める可能性が高いため heuristic として扱う。 Confidence の段差 (0.9 / 0.7 / 0.5) は ADR-008 の推奨値に整合。

---

## 5. 推奨される対策パターン

```python
# NG: ナイーブな retry + 高並列
@retry(stop=stop_after_attempt(5))  # retries=5
def call_external_api(arg):
    ...
# 上流 ParallelAgent (max_concurrency=20) から呼ぶ → blast 100

# OK: exponential backoff
@retry(stop=stop_after_attempt(5), wait=wait_exponential(multiplier=2))
def call_external_api(arg):
    ...

# OK: circuit breaker
breaker = CircuitBreaker(fail_max=3, reset_timeout=60)
@breaker
@retry(stop=stop_after_attempt(5))
def call_external_api(arg):
    ...

# OK: shared rate limiter (semaphore)
sem = asyncio.Semaphore(5)  # 並列上限を絞り込む
async def call_external_api(arg):
    async with sem:
        ...
```

ライブラリ側のサポート例:

| ライブラリ | 推奨 API |
|---|---|
| Python `tenacity` | `wait_exponential` / `wait_random_exponential` |
| Python `httpx` | `AsyncHTTPTransport(retries=...)` + custom transport for backoff |
| Go `cenkalti/backoff` | `ExponentialBackOff` |
| Go `sony/gobreaker` | `CircuitBreaker` |
| LangChain | `ChatModel(... max_retries=N)` + manual `wait_exponential` |
| OpenAI Python SDK | `OpenAI(max_retries=N)` (内部で exponential backoff) |

---

## 6. 既知の False Positive / False Negative

### False Positive

- **fan-in は 20 だが実際の同時並列は ≤ 5**: 静的解析では runtime scheduler の挙動まで見えない。`--min-confidence 0.8` で Warning 以下を切ると軽減できる。
- **retry が冪等な操作で実害ゼロ**: GET だけのキャッシュ取得など、storm が発生しても upstream が傷つかないケース。Severity は Warning / Info なので CI ブロックには直結しない (Critical は exit code 2)。

### False Negative

- **retry が外側のミドルウェアに移譲されている**: framework 内部の retry middleware (LangGraph の `RetryPolicy`、Vercel AI SDK の `streamText({ maxRetries: ... })`) は graph の Config に現れない場合がある。フレームワーク固有 parser (Phase 2 後半で拡張予定) で検出範囲を広げる。
- **動的に retry 数を決めるコード**: `retries=int(os.getenv("RETRY"))` のような expression 形式は parser によっては数値が取れず、Config 不在として扱われる (false negative)。

---

## 7. 関連ルール / ADR

- **ADR-006**: ESLint visitor pattern — Local rule との対比。本ルールは Path tier。
- **ADR-007**: Local / Path / Global の3層分離 — 本ルールが Path tier に属する根拠。
- **ADR-008**: ConfidenceReason — Critical = `exact_static_match`、Warning/Info = `heuristic_pattern` を採用。
- `cost_estimation` (`docs/rules/cost-estimation.md` 相当) — Path rule + Sinks=nil の同型実装。
- `max_parallel_branches` (Global) — fan-out 単体検出。retry_storm と組み合わせると相補的。
- `error_handler_checker` (Path) — Tool ノードの error 経路欠落。retry_storm が指摘する Tool は同時に error_handler_checker の対象になることが多い。
