# Shingan ユースケース集

Shingan の 20 ルール + 7 エントリポイント (CLI / LSP / MCP / API / GitHub Action / Runner / Web middleware) が、現場で具体的にどう活用されるかのシナリオ集。

---

## 1. SaaS エージェントプラットフォーム — 保存時の品質ゲート

**シナリオ**: GUI ワークフローエディタ (n8n / Dify / 自社 SaaS) で、ユーザー (非エンジニア) がワークフローを保存しようとした時。

**統合**:
```
[ユーザー: 保存ボタンクリック]
  ↓
[エディタ backend] → POST https://shingan.internal/analyze { format: "json", content: {...} }
  ↓
[Shingan API] 20 ルール並行解析 (典型的なワークフロー: 30ノード → 0.2ms)
  ↓
[response] { findings: [...], summary: {...} }
  ↓
[エディタ] Critical あれば保存ブロック、UI に警告表示
```

**防げる事故**:
- `loop_guard`: MaxIter 未設定で無限ループ → Gemini 課金数万円
- `error_handler_checker`: ブラウザ操作失敗時のフォールバック無し → GUI自動化が途中で止まる
- `secret_exposure_scanner`: プロンプトにAPIキーハードコード → ログに漏洩

**コスト**: 1解析 < 0.5ms、1日1万回実行でもサーバーコスト < $0.01/月

---

## 2. CI/CD Pull Request ガード

**シナリオ**: エージェント定義を Go コード (ADK-Go) or JSON でリポジトリ管理している開発チーム。

**GitHub Actions**:
```yaml
- uses: hatyibei/shingan@v0.5.0
  with:
    format: adk-go
    input: ./agents/
    fail-on: critical
    output: sarif
    output-file: shingan.sarif

- uses: github/codeql-action/upload-sarif@v3
  with:
    sarif_file: shingan.sarif
```

**効果**:
- PR の Files changed タブに Shingan 警告がインライン表示
- Critical検出時は Branch Protection でマージ禁止
- 開発者は **ローカルで** `shingan analyze` 実行して事前修正可能

**現実の事例想定**:
- `deprecated_model`: チームが `gpt-4-0314` (shutdown済) で Agent 書いてたのを PR レビュー前に検出
- `cost_estimation`: ループ内で gpt-4o 使ってた設定を、PR 時点で「miniで十分では?」と指摘

---

## 3. エージェント実行ランタイムの pre-execution guard

**シナリオ**: Shingan-runner を Agent 実行 middleware として使う。

```bash
./shingan-runner --sample infinite_loop_unbounded --dry-run
# → Shingan が Critical 検出 → 実行拒否
# → Gemini 呼び出しゼロ、コスト発生ゼロ
```

**応用**: **ADK Web UI** 内の Run API middleware (`cmd/shingan-web`)
- ブラウザで「送信」ボタン押す → Run API → Shingan middleware → 問題なければ Gemini へ、問題ありなら 403

これは Shingan-Gemini 実行間の「最後の砦」。

---

## 4. セキュリティ監査バッチ

**シナリオ**: 企業内で数百のエージェントが稼働している。定期的 (月次) に全部を再解析して非推奨化・セキュリティ問題を洗い出す。

```bash
# cron で毎月1日 02:00
for dir in /agents/*/; do
  shingan analyze --format adk-go --input "$dir" --output sarif --output-file "reports/$(basename $dir).sarif"
done

# SARIF を集約して通知
cat reports/*.sarif | jq '.runs[0].results[] | select(.level=="error")' | slack-notify #security
```

**検出されるもの**:
- `deprecated_model`: shutdownされたモデルをまだ使ってる Agent のリスト
- `secret_exposure_scanner`: ハードコードシークレットの掘り出し
- `pii_leak_scanner`: GDPR違反になりうるRAGパス

---

## 5. 教育・トレーニング

**シナリオ**: 社内エンジニアに「AIエージェント設計のベストプラクティス」を教える時。

```bash
# 意図的にバグを仕込んだサンプル
shingan-gen --pattern buggy --seed 42 > exercise.json

# 学習者は exercise.json を手動レビューして誤りを探す
# その後 Shingan 実行で答え合わせ
shingan analyze --format json --input exercise.json --output markdown
```

**活用**: 新人研修、ワークショップ、ドキュメント化された悪い例として `testdata/generated/buggy-seed42.json` をそのまま使える。

---

## 6. 他システムへの組み込み

### LangGraph (Python) — GA
`infrastructure/parser/langgraph.go` + `scripts/export_langgraph_server.py` で `StateGraph` を抽出。`pip install langgraph` 必要。

### ADK-Go (Google) — GA
`go/parser` + `go/types` ネイティブ解析。`functiontool.New[TArgs, TResults]` のジェネリクスから Tool カテゴリ推定。

### Generic JSON workflow / 自社 GUI エディタ — GA
任意の workflow JSON を `domain.WorkflowGraph` 形式に正規化すれば 20 ルールがそのまま動く。`docs/rule-authoring.md` の IR section 参照。

### n8n / CrewAI / Mastra への対応 (v0.7+ 予定)
新 parser を `infrastructure/parser/<framework>.go` に追加するだけ。Onion Architecture により domain / application 層は不変。

---

## 7. データドリブンな品質改善

**シナリオ**: 検出された Finding の種類を時系列でトラック。

```bash
# Finding を DB に保存
shingan analyze --format json --input . --output json | jq '.findings[]' | psql -c "INSERT INTO findings ..."

# Grafana で可視化: 月ごとの Critical 数推移、よく出るルールTop5、etc.
```

**効果**: 開発チームの「ワークフロー品質の習慣化」を定量測定できる。

---

## 8. Shingan自体の開発 (self-dogfood)

Shingan 自身のパイプラインを WorkflowGraph で表現 (`testdata/meta/shingan_pipeline.json`) して、Shingan で解析する。v0.1 では 5件の誤検知が出て、v0.2 で NodeType 分離 + 2ホップ追跡で全件解消。

**学び**: ルール自体の誤検知率を継続的に下げるには、自己適用で「ドッグフード」し続けるのが効果的。

---

## ユースケース別の推奨フラグ

| ユースケース | 推奨コマンド |
|---|---|
| CI PR ガード | `--output sarif --output-file out.sarif` |
| 本番middleware | `shingan-web` 起動、middleware注入 |
| 実行前ガード | `shingan-runner --sample NAME` |
| 監査バッチ | `--output json` + jq 加工 |
| 人間レビュー | `--output markdown` (テーブル表示) |
| 高信頼のみ表示 | `--min-confidence 0.9` |
